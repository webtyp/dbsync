//go:build !wasm

package tests

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wtime "webtyp.com/time"

	"webtyp.com/changelog"
	"webtyp.com/view"
	"webtyp.com/dbsync"
	"webtyp.com/idempotency"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/router"
	"webtyp.com/rpc"
	"webtyp.com/server/httpd"
	"webtyp.com/storage"
	"webtyp.com/storage/mem"
)

const (
	userID         = "u1"
	otherUser      = "u2"
	pathCreateNote = "/api/notes/create_note"
	pathPull       = "/api/dbsync/pull"
	waitFor        = 5 * time.Second
)

// Note is the synced table (owner scopes who receives it).
type Note struct {
	Id    string
	Owner string
	Text  string
}

var noteSchema = []model.Field{
	{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}},
	{Name: "owner", Type: model.Text()},
	{Name: "text", Type: model.Text()},
}

func (n *Note) ModelName() string     { return "note" }
func (n *Note) Schema() []model.Field { return noteSchema }
func (n *Note) Pointers() []any       { return []any{&n.Id, &n.Owner, &n.Text} }
func (n *Note) IsNil() bool           { return n == nil }
func (n *Note) EncodeFields(w model.FieldWriter) {
	w.String("id", n.Id)
	w.String("owner", n.Owner)
	w.String("text", n.Text)
}
func (n *Note) DecodeFields(r model.FieldReader) {
	if v, ok := r.String("id"); ok {
		n.Id = v
	}
	if v, ok := r.String("owner"); ok {
		n.Owner = v
	}
	if v, ok := r.String("text"); ok {
		n.Text = v
	}
}
func (n *Note) Item() model.Item {
	return model.Item{
		ID:    n.Id,
		Label: n.Text,
	}
}

// Other is tracked by the server's change log but has no sync rule.
type Other struct{ Id string }

var otherSchema = []model.Field{{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}}}

func (o *Other) ModelName() string                { return "other" }
func (o *Other) Schema() []model.Field            { return otherSchema }
func (o *Other) Pointers() []any                  { return []any{&o.Id} }
func (o *Other) IsNil() bool                      { return o == nil }
func (o *Other) EncodeFields(w model.FieldWriter) { w.String("id", o.Id) }
func (o *Other) DecodeFields(r model.FieldReader) {
	if v, ok := r.String("id"); ok {
		o.Id = v
	}
}
func (o *Other) Item() model.Item {
	return model.Item{
		ID: o.Id,
	}
}

func noteFactory() model.Model  { return &Note{} }
func otherFactory() model.Model { return &Other{} }

var tables = []func() model.Model{noteFactory}

// notes is the offline module. On the server (server == true) it refuses the texts "taken"
// (create) and "locked" (update), as a real server refuses what a device could not know.
type notes struct {
	db     *orm.DB
	server bool
	calls  *atomic.Int32 // create_note runs
}

func (m *notes) ModelName() string { return "notes" }

func (m *notes) MountOperations(reg router.OperationRegistry) {
	reg.Operation("create_note", func(ctx router.Context) {
		var n Note
		if err := ctx.Decode(&n); err != nil {
			ctx.WriteStatus(400)
			return
		}
		if m.calls != nil {
			m.calls.Add(1)
		}
		if m.server && n.Text == "taken" {
			ctx.WriteStatus(409)
			ctx.Write([]byte("taken"))
			return
		}
		if err := m.db.Create(&n); err != nil {
			ctx.WriteStatus(500)
			ctx.Write([]byte(err.Error()))
			return
		}
		ctx.Encode(&n)
	}).Requires("note", model.Create)

	reg.Operation("update_note", func(ctx router.Context) {
		var n Note
		if err := ctx.Decode(&n); err != nil {
			ctx.WriteStatus(400)
			return
		}
		if m.server && n.Text == "locked" {
			ctx.WriteStatus(409)
			ctx.Write([]byte("locked"))
			return
		}
		if err := m.db.Update(&n, orm.Eq("id", n.Id)); err != nil {
			ctx.WriteStatus(500)
			ctx.Write([]byte(err.Error()))
			return
		}
		ctx.Encode(&n)
	}).Requires("note", model.Update)

	reg.Operation("get_note", func(ctx router.Context) {
		var n Note
		if err := ctx.Decode(&n); err != nil {
			ctx.WriteStatus(400)
			return
		}
		if err := m.db.Query(&n).Where("id").Eq(n.Id).ReadOne(); err != nil {
			ctx.WriteStatus(404)
			return
		}
		ctx.Encode(&n)
	}).Requires("note", model.Read)

	reg.Operation("ping", func(ctx router.Context) { ctx.WriteStatus(204) }).Public()

	// purge_notes demands a second factor: an offline replay cannot prove one,
	// so dbsync must refuse to run it offline even though it declares Requires.
	reg.Operation("purge_notes", func(ctx router.Context) { ctx.WriteStatus(204) }).Requires("note", model.Delete).RequiresMFA()
}

func now() wtime.Nano { return wtime.NowNano() }

func allow(string, model.Resource, model.Action) bool { return true }

// server is the app's server: rpc + idempotency + dbsync over a change-logged mem database.
type server struct {
	t      *testing.T
	log    *changelog.Log
	db     *orm.DB
	idem   *idempotency.Store
	calls  atomic.Int32
	scope  func(string) ([]storage.Condition, error)
	fields func(string) ([]string, error)

	fail503 atomic.Bool // answer 503 to create_note
	noAuth  bool        // do not set UserID
	mu      sync.Mutex
	hits    []string // request paths seen
	addr    string
	ts      *httptest.Server
	url     string
}

func ownerScope(u string) ([]storage.Condition, error) {
	return []storage.Condition{storage.Eq("owner", u)}, nil
}

func newServer(t *testing.T) *server {
	t.Helper()
	raw := mem.New()
	log, err := changelog.New(raw.(changelog.TxConn), noteFactory, otherFactory)
	if err != nil {
		t.Fatalf("changelog.New: %v", err)
	}
	s := &server{t: t, log: log, db: orm.New(log), scope: ownerScope}
	s.freshIdempotency()
	s.start()
	t.Cleanup(s.stop)
	return s
}

// freshIdempotency gives the server a store that never saw any key (a standby server).
func (s *server) freshIdempotency() {
	idem, err := idempotency.New(orm.New(mem.New()), now)
	if err != nil {
		s.t.Fatalf("idempotency.New: %v", err)
	}
	s.idem = idem
}

func (s *server) handler() http.Handler {
	srv := httpd.New(httpd.Config{
		TLS: httpd.TLSConfig{PlainHTTP: true},
		Authn: func(next router.HandlerFunc) router.HandlerFunc {
			return func(ctx router.Context) {
				if !s.noAuth {
					ctx.SetUserID(userID)
				}
				next(ctx)
			}
		},
		Authorize: allow,
	})
	r := srv.Router()
	r.Use(s.idem.Middleware()) // before mounting the routes
	if err := rpc.Mount(r, rpc.DefaultPrefix, &notes{db: s.db, server: true, calls: &s.calls}); err != nil {
		s.t.Fatalf("rpc.Mount: %v", err)
	}
	syncSrv, err := dbsync.NewServer(dbsync.ServerConfig{
		DB:        s.db,
		Log:       s.log,
		Rules:     []dbsync.Rule{{Model: noteFactory, Resource: "note", Scope: s.scope, Fields: s.fields}},
		Authorize: allow,
		Now:       now,
	})
	if err != nil {
		s.t.Fatalf("dbsync.NewServer: %v", err)
	}
	syncSrv.MountAPI(r)
	h, err := srv.Handler()
	if err != nil {
		s.t.Fatalf("Handler: %v", err)
	}
	// Outside the router, so it sees every request, even one the idempotency store answers.
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		s.mu.Lock()
		s.hits = append(s.hits, req.URL.Path)
		s.mu.Unlock()
		if s.fail503.Load() && req.URL.Path == pathCreateNote {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		h.ServeHTTP(w, req)
	})
}

// start serves on the same address every time, with a new dbsync server (new epoch).
func (s *server) start() {
	ts := httptest.NewUnstartedServer(s.handler())
	if s.addr != "" {
		ts.Listener.Close()
		ln, err := net.Listen("tcp", s.addr)
		if err != nil {
			s.t.Fatalf("listen %s: %v", s.addr, err)
		}
		ts.Listener = ln
	}
	ts.Start()
	s.ts, s.addr, s.url = ts, ts.Listener.Addr().String(), ts.URL
}

func (s *server) stop() {
	if s.ts != nil {
		s.ts.Close()
		s.ts = nil
	}
}

func (s *server) restart() {
	s.stop()
	s.start()
}

func (s *server) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, p := range s.hits {
		if strings.HasPrefix(p, path) {
			n++
		}
	}
	return n
}

func (s *server) note(id string) (*Note, bool) {
	n := &Note{}
	if err := s.db.Query(n).Where("id").Eq(id).ReadOne(); err != nil {
		return nil, false
	}
	return n, true
}

func (s *server) put(n *Note) {
	s.t.Helper()
	if err := s.db.Create(n); err != nil {
		s.t.Fatalf("server create: %v", err)
	}
}

type fakeLock struct{ free bool }

func (l fakeLock) TryLock(string) (bool, error) { return l.free, nil }

// device is one browser profile: its local database and its sync client.
type device struct {
	t       *testing.T
	local   storage.Conn
	db      *orm.DB
	c       *dbsync.Client
	records *view.Records
}

func config(s *server, local storage.Conn, lock dbsync.TabLock) dbsync.ClientConfig {
	return dbsync.ClientConfig{
		Local:  local,
		Remote: rpc.NewCaller(s.url),
		Origin: s.url,
		Tables: tables,
		Modules: func(db *orm.DB, _ model.IDGenerator) ([]router.OperationModule, error) {
			return []router.OperationModule{&notes{db: db}}, nil
		},
		TenantID: "t1",
		UserID:   userID,
		Now:      now,
		Lock:     lock,
		Records:  view.NewRecords(),
	}
}

func open(t *testing.T, s *server) *device {
	t.Helper()
	local := mem.New()
	cfg := config(s, local, fakeLock{free: true})
	c, err := dbsync.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return &device{t: t, local: local, db: orm.New(local), c: c, records: cfg.Records}
}

// call runs op through the device's Caller and waits for done.
func (d *device) call(op string, n *Note) error {
	d.t.Helper()
	ch := make(chan error, 1)
	d.c.Caller().Call(op, n, nil, func(err error) { ch <- err })
	select {
	case err := <-ch:
		return err
	case <-time.After(waitFor):
		d.t.Fatalf("%s: no answer", op)
		return nil
	}
}

func (d *device) create(text string) *Note {
	d.t.Helper()
	n := &Note{Id: d.c.IDs().NewID(), Owner: userID, Text: text}
	if err := d.call("notes.create_note", n); err != nil {
		d.t.Fatalf("create_note: %v", err)
	}
	return n
}

func (d *device) note(id string) (*Note, bool) {
	n := &Note{}
	if err := d.db.Query(n).Where("id").Eq(id).ReadOne(); err != nil {
		return nil, false
	}
	return n, true
}

func (d *device) state() dbsync.State {
	st := dbsync.State{}
	d.db.Query(&st).Where("id").Eq("state").ReadOne()
	return st
}

// eventually waits until cond holds, asking for a sync on every try.
func eventually(t *testing.T, d *device, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		if d != nil {
			d.c.Sync()
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}
