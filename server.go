package dbsync

import (
	"sync"

	"webtyp.com/changelog"
	"webtyp.com/fmt"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/router"
)

const (
	moduleName        = "dbsync"
	defaultPageLimit  = 500
	maxPageLimit      = 1000
	statusBadRequest  = 400
	statusNotFound    = 404
	statusServerError = 500
)

// ServerConfig is what the server side of dbsync needs.
type ServerConfig struct {
	DB        *orm.DB          // reads the app's rows; dbsync's own table (replica registry) lives here
	Log       *changelog.Log   // the change log wrapping the app's storage
	Rules     []Rule           // the tables devices keep (the app's config/sync.go)
	Authorize model.Authorizer // required: model.Read on Rule.Resource decides who receives a table
	Now       func() int64     // unix ns; required
}

// Server answers devices: replica numbers, the first copy, and the change feed.
type Server struct {
	cfg   ServerConfig
	epoch string
	mu    sync.Mutex // serialises /register
}

// NewServer validates cfg. Its epoch is minted once here (decimal unix ns of Now() at start).
func NewServer(cfg ServerConfig) (*Server, error) {
	switch {
	case cfg.DB == nil:
		return nil, errDBRequired
	case cfg.Log == nil:
		return nil, errLogRequired
	case cfg.Authorize == nil:
		return nil, errAuthzRequired
	case cfg.Now == nil:
		return nil, errNowRequired
	case len(cfg.Rules) == 0:
		return nil, errNoRules
	}
	for i, r := range cfg.Rules {
		if r.Model == nil {
			return nil, errRuleModel
		}
		if r.Resource == "" {
			return nil, errRuleResource
		}
		name := r.Model().ModelName()
		for _, prev := range cfg.Rules[:i] {
			if prev.Model().ModelName() == name {
				return nil, errDuplicateRule(name)
			}
		}
		if _, err := pkName(r.Model()); err != nil {
			return nil, err
		}
	}
	return &Server{cfg: cfg, epoch: fmt.Sprintf("%d", cfg.Now())}, nil
}

// ModelName makes Server a router.APIModule.
func (s *Server) ModelName() string { return moduleName }

// MountAPI serves the routes under DefaultPrefix. Every route needs a signed-in user.
func (s *Server) MountAPI(r router.Router) {
	r.Mount(DefaultPrefix, func(r router.Router) {
		r.Post(routeRegister, s.register).Authenticated().Describe("Assigns this device its replica number")
		r.Get(routeHead, s.head).Authenticated().Describe("Server epoch and change-log head")
		r.Get(routeSnapshot, s.snapshot).Authenticated().Describe("One page of a table, for a device's first copy")
		r.Get(routePull, s.pull).Authenticated().Describe("Changes after a cursor, filtered by the sync rules")
		r.Get(routeRow, s.row).Authenticated().Describe("One row by id, to undo a rejected write on a device")
	})
}

func (s *Server) register(ctx router.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	last := &Replica{}
	err := s.cfg.DB.Query(last).OrderBy(Replica_.Number).Desc().Limit(1).ReadOne()
	if err != nil && !orm.IsNotFound(err) {
		fail(ctx, statusServerError, err)
		return
	}
	n := last.Number + 1
	rep := &Replica{Id: fmt.Sprintf("%d", n), Number: n, UserId: ctx.UserID(), CreatedAt: s.cfg.Now()}
	if err := s.cfg.DB.Create(rep); err != nil {
		fail(ctx, statusServerError, err)
		return
	}
	ctx.Encode(&replicaWire{replica: n})
}

func (s *Server) head(ctx router.Context) {
	ctx.Encode(&headWire{epoch: s.epoch, head: s.cfg.Log.Head()})
}

func (s *Server) snapshot(ctx router.Context) {
	rule, ok := s.readable(ctx, query(ctx, queryTable))
	if !ok {
		ctx.WriteStatus(statusNotFound)
		return
	}
	limit, err := pageLimit(ctx)
	if err != nil {
		fail(ctx, statusBadRequest, err)
		return
	}
	proto := rule.Model()
	pk, _ := pkName(proto)
	qb, err := s.scoped(rule, ctx.UserID(), proto)
	if err != nil {
		fail(ctx, statusServerError, err)
		return
	}
	if after := query(ctx, queryAfter); after != "" {
		qb = qb.Where(pk).Gt(after)
	}
	out := &snapshotOut{}
	err = qb.OrderBy(pk).Asc().Limit(limit).ReadAll(rule.Model, func(m model.Model) {
		out.rows = append(out.rows, m)
	})
	if err != nil {
		fail(ctx, statusServerError, err)
		return
	}
	if len(out.rows) == limit {
		out.next, _ = pkValue(out.rows[len(out.rows)-1])
	}
	ctx.Encode(out)
}

func (s *Server) pull(ctx router.Context) {
	cursor, err := intParam(ctx, queryCursor, 0)
	if err != nil {
		fail(ctx, statusBadRequest, err)
		return
	}
	limit, err := pageLimit(ctx)
	if err != nil {
		fail(ctx, statusBadRequest, err)
		return
	}
	head := s.cfg.Log.Head()
	changes, err := s.cfg.Log.Since(cursor, limit)
	if err != nil {
		fail(ctx, statusServerError, err)
		return
	}
	out := &pullWire{epoch: s.epoch, head: head, cursor: cursor}
	if len(changes) > 0 {
		// Advance past every change read, kept or not: a page whose changes were all
		// filtered out must still move the device forward.
		out.cursor = changes[len(changes)-1].Version
	}
	for _, c := range changes {
		rule, ok := s.readable(ctx, c.TableName)
		if !ok {
			continue
		}
		row, found, err := s.load(rule, ctx.UserID(), c.RowId)
		if err != nil {
			fail(ctx, statusServerError, err)
			return
		}
		w := &changeWire{table: c.TableName, rowID: c.RowId, op: opDelete}
		if found {
			w.op, w.row = opUpsert, row
		}
		out.changes = append(out.changes, w)
	}
	if out.head < out.cursor {
		out.head = out.cursor // a write landed between Head() and Since()
	}
	ctx.Encode(out)
}

func (s *Server) row(ctx router.Context) {
	rule, ok := s.readable(ctx, query(ctx, queryTable))
	if !ok {
		ctx.WriteStatus(statusNotFound)
		return
	}
	row, found, err := s.load(rule, ctx.UserID(), query(ctx, queryID))
	if err != nil {
		fail(ctx, statusServerError, err)
		return
	}
	if !found {
		ctx.WriteStatus(statusNotFound)
		return
	}
	ctx.Encode(&rowWire{row: row})
}

// readable returns the rule of table when the user may read it.
func (s *Server) readable(ctx router.Context, table string) (Rule, bool) {
	for _, r := range s.cfg.Rules {
		if r.Model().ModelName() == table {
			return r, s.cfg.Authorize(ctx.UserID(), r.Resource, model.Read)
		}
	}
	return Rule{}, false
}

// scoped starts a query on proto restricted to the rows userID receives.
func (s *Server) scoped(rule Rule, userID string, proto model.Model) (*orm.QB, error) {
	qb := s.cfg.DB.Query(proto)
	if rule.Scope == nil {
		return qb, nil
	}
	conds, err := rule.Scope(userID)
	if err != nil {
		return nil, err
	}
	return where(qb, conds)
}

// load reads one row by primary key inside the user's scope.
func (s *Server) load(rule Rule, userID, id string) (model.Model, bool, error) {
	m := rule.Model()
	pk, _ := pkName(m)
	qb, err := s.scoped(rule, userID, m)
	if err != nil {
		return nil, false, err
	}
	err = qb.Where(pk).Eq(id).ReadOne()
	if orm.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return m, true, nil
}

func query(ctx router.Context, key string) string {
	v, _ := router.QueryParam(ctx.Path(), key)
	return v
}

func intParam(ctx router.Context, key string, def int64) (int64, error) {
	v := query(ctx, key)
	if v == "" {
		return def, nil
	}
	return fmt.Convert(v).Int64()
}

func pageLimit(ctx router.Context) (int, error) {
	n, err := intParam(ctx, queryLimit, defaultPageLimit)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		n = defaultPageLimit
	}
	if n > maxPageLimit {
		n = maxPageLimit
	}
	return int(n), nil
}

func fail(ctx router.Context, status int, err error) {
	ctx.WriteStatus(status)
	ctx.Write([]byte(err.Error()))
}
