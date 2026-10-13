package dbsync

import (
	"sync"
	"sync/atomic"

	"webtyp.com/changelog"
	"webtyp.com/fetch"
	"webtyp.com/json"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/outbox"
	"webtyp.com/router"
	"webtyp.com/router/loopback"
	"webtyp.com/rpc"
	"webtyp.com/storage"
	wtime "webtyp.com/time"
	"webtyp.com/unixid"
)

const (
	lockName         = "dbsync"
	stateID          = "state"
	requestTimeoutMs = 10000
	syncEveryMs      = 30000
	redeliverWindow  = int64(10 * 60 * 1000000000) // 10 min in ns
	maxTouched       = 1000
	statusOK         = 200
)

// ClientConfig is what the browser side of dbsync needs.
type ClientConfig struct {
	Local  storage.Conn         // the browser database, opened with indexdb.New(name, dbsync.LocalModels(tables)...); must support BeginTx
	Remote *rpc.Caller          // pushes queued writes (CallKeyed)
	Origin string               // "" = same origin, for dbsync's own endpoints
	Tables []func() model.Model // the same factories as the server Rules (decode pulled rows)
	// Modules builds the offline modules over the local database and this device's ids.
	Modules  func(db *orm.DB, ids model.IDGenerator) ([]router.OperationModule, error)
	TenantID string
	UserID   string       // the signed-in user (offline: the cached session)
	Now      func() int64 // unix ns
	Lock     TabLock      // nil = browser Web Locks (wasm); tests pass a fake
}

// TabLock lets one tab of a browser profile own the local database.
type TabLock interface {
	TryLock(name string) (bool, error)
}

// Status is what a connection indicator shows.
type Status struct {
	Online   bool // the last request to the server got an HTTP answer (any status)
	Pending  int  // queued writes not delivered yet
	Rejected int  // writes the server refused (the conflict tray)
}

// LocalModels returns one instance of each app table plus dbsync's own local tables
// (dbsync_state, dbsync_touched, outbox_mutation, change_log): pass them to the local database
// constructor.
func LocalModels(tables []func() model.Model) []model.Model {
	out := make([]model.Model, 0, len(tables)+4)
	for _, f := range tables {
		out = append(out, f())
	}
	return append(out, &State{}, &Touched{}, &outbox.Mutation{}, &changelog.Change{})
}

// Client is the sync engine of one browser profile.
type Client struct {
	cfg      ClientConfig
	raw      *orm.DB        // dbsync's tables, the outbox, and every row applied from the server
	localLog *changelog.Log // records the rows the offline modules write
	ids      *deviceIDs
	ops      []opInfo
	local    router.Caller // loopback over the offline modules
	box      *outbox.Outbox

	mu    sync.Mutex // one local write, pull page or revert at a time
	state State
	saved bool // the state row exists

	online   atomic.Bool
	explicit atomic.Bool
	signal   chan struct{}
	closed   chan struct{}
	once     sync.Once
	timerMu  sync.Mutex
	timer    wtime.Timer

	statusMu  sync.Mutex
	status    Status
	listeners []func(Status)
}

// Open takes the tab lock, registers the device on its first run, copies the data on its first
// run, and starts syncing in the background. It blocks: call it from the composition root, never
// from a JavaScript event callback.
func Open(cfg ClientConfig) (*Client, error) {
	switch {
	case cfg.Local == nil:
		return nil, errLocalRequired
	case cfg.Remote == nil:
		return nil, errRemoteRequired
	case cfg.Modules == nil:
		return nil, errModulesRequired
	case cfg.Now == nil:
		return nil, errNowRequired
	case len(cfg.Tables) == 0:
		return nil, errTablesRequired
	case cfg.UserID == "":
		return nil, errUserRequired
	}
	txc, ok := cfg.Local.(changelog.TxConn)
	if !ok {
		return nil, errLocalNeedsTx
	}
	lock := cfg.Lock
	if lock == nil {
		lock = defaultTabLock()
	}
	if lock == nil {
		return nil, errTabLockRequired
	}
	got, err := lock.TryLock(lockName)
	if err != nil {
		return nil, err
	}
	if !got {
		return nil, ErrAnotherTab
	}

	c := &Client{cfg: cfg, raw: orm.New(cfg.Local), signal: make(chan struct{}, 1), closed: make(chan struct{})}
	tracked := make([]changelog.NewModel, len(cfg.Tables))
	for i, f := range cfg.Tables {
		tracked[i] = f
	}
	if c.localLog, err = changelog.New(txc, tracked...); err != nil {
		return nil, err
	}
	if err := c.loadState(); err != nil {
		return nil, err
	}
	if c.state.Replica == 0 {
		var w replicaWire
		status, err := c.request(fetch.Post, routeRegister, &w)
		if status == 0 {
			return nil, ErrFirstRunNeedsServer
		}
		if err != nil {
			return nil, err
		}
		c.state.Replica = w.replica
		if err := c.saveState(); err != nil {
			return nil, err
		}
	}
	gen, err := unixid.NewForReplica(unixid.Replica(c.state.Replica), c.state.LastMinted)
	if err != nil {
		return nil, err
	}
	c.ids = &deviceIDs{gen: gen, last: c.state.LastMinted}

	mods, err := cfg.Modules(orm.New(c.localLog), c.ids)
	if err != nil {
		return nil, err
	}
	c.ops = harvest(mods)
	c.local = loopback.ActingAs(cfg.TenantID, cfg.UserID, mods...)
	if c.box, err = outbox.New(c.raw, c.ids, cfg.Now); err != nil {
		return nil, err
	}

	if c.state.Epoch == "" {
		if err := c.bootstrap(); err != nil {
			return nil, err
		}
	}
	c.refreshStatus()
	go c.loop()
	c.arm()
	return c, nil
}

// IDs mints ids for records created on this device ("<nanos>.<replica>").
func (c *Client) IDs() model.IDGenerator { return c.ids }

// Sync asks for a sync now (for example when the SSE channel reconnects). It never blocks.
func (c *Client) Sync() {
	c.explicit.Store(true)
	c.wake()
}

// Status returns the last known status.
func (c *Client) Status() Status {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	return c.status
}

// OnStatus registers fn, called (from the sync goroutine) every time the status changes.
func (c *Client) OnStatus(fn func(Status)) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	c.listeners = append(c.listeners, fn)
}

// Rejected lists the writes the server refused: the conflict tray.
func (c *Client) Rejected() ([]outbox.Mutation, error) { return c.box.Rejected() }

// Dismiss removes a refused write from the conflict tray once the user resolved it.
func (c *Client) Dismiss(id string) error {
	if err := c.box.Dismiss(id); err != nil {
		return err
	}
	c.refreshStatus()
	return nil
}

// Close stops the background sync. The tab keeps the lock until the page closes.
func (c *Client) Close() error {
	c.once.Do(func() {
		close(c.closed)
		c.timerMu.Lock()
		if c.timer != nil {
			c.timer.Stop()
		}
		c.timerMu.Unlock()
	})
	return nil
}

func (c *Client) wake() {
	select {
	case c.signal <- struct{}{}:
	default:
	}
}

func (c *Client) loadState() error {
	err := c.raw.Query(&c.state).Where(State_.Id).Eq(stateID).ReadOne()
	if orm.IsNotFound(err) {
		c.state = State{Id: stateID}
		return nil
	}
	if err != nil {
		return err
	}
	c.saved = true
	return nil
}

// saveState persists c.state; callers hold c.mu or run before the sync goroutine starts.
func (c *Client) saveState() error {
	if c.ids != nil {
		c.state.LastMinted = c.ids.lastMinted()
	}
	if c.saved {
		return c.raw.Update(&c.state, orm.Eq(State_.Id, stateID))
	}
	if err := c.raw.Create(&c.state); err != nil {
		return err
	}
	c.saved = true
	return nil
}

func (c *Client) refreshStatus() {
	next := Status{Online: c.online.Load()}
	next.Pending, _ = c.box.Pending()
	if rej, err := c.box.Rejected(); err == nil {
		next.Rejected = len(rej)
	}
	c.statusMu.Lock()
	changed := next != c.status
	c.status = next
	listeners := c.listeners
	c.statusMu.Unlock()
	if changed {
		for _, fn := range listeners {
			fn(next)
		}
	}
}

// request sends one dbsync request and decodes a 2xx answer into into. status 0 = no answer.
func (c *Client) request(method func(endpoint any) *fetch.Request, route string, into model.Decodable) (int, error) {
	type result struct {
		status int
		body   []byte
		err    error
	}
	ch := make(chan result, 1)
	method(c.cfg.Origin + DefaultPrefix + route).Timeout(requestTimeoutMs).Send(func(resp *fetch.Response, err error) {
		if err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{status: resp.Status, body: resp.Body()}
	})
	r := <-ch
	c.online.Store(r.status != 0)
	if r.status == 0 {
		return 0, r.err
	}
	if r.status < statusOK || r.status > 299 {
		return r.status, errServerAnswered(r.status, route, string(r.body))
	}
	if into != nil && len(r.body) > 0 {
		return r.status, json.Decode(r.body, into)
	}
	return r.status, nil
}

// deviceIDs mints this device's ids and remembers the newest one's timestamp.
type deviceIDs struct {
	mu   sync.Mutex
	gen  *unixid.UnixID
	last int64
}

func (d *deviceIDs) NewID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	id := d.gen.NewID()
	if ts, _, err := d.gen.Parse(id); err == nil {
		d.last = ts
	}
	return id
}

func (d *deviceIDs) lastMinted() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.last
}

// opInfo is what dbsync knows of an offline operation: its qualified name and declared action.
type opInfo struct {
	name     string
	action   model.Action
	declared bool // Requires was called
	// mfa: the route demands a second factor. An offline replay cannot prove
	// one, so such an operation never runs offline.
	mfa bool
}

// harvester is a router.OperationRegistry that only records names and actions.
type harvester struct {
	module string
	ops    []opInfo
}

func harvest(mods []router.OperationModule) []opInfo {
	h := &harvester{}
	for _, m := range mods {
		if m == nil {
			continue
		}
		h.module = m.ModelName()
		m.MountOperations(h)
	}
	return h.ops
}

func (h *harvester) Operation(name string, _ router.HandlerFunc) router.Route {
	h.ops = append(h.ops, opInfo{name: h.module + "." + name})
	return harvestRoute{h: h, i: len(h.ops) - 1}
}

type harvestRoute struct {
	h *harvester
	i int
}

func (r harvestRoute) Requires(_ model.Resource, a model.Action) router.Route {
	r.h.ops[r.i].action, r.h.ops[r.i].declared = a, true
	return r
}
func (r harvestRoute) Authenticated() router.Route { return r }
func (r harvestRoute) Public() router.Route        { return r }
func (r harvestRoute) RequiresMFA() router.Route {
	r.h.ops[r.i].mfa = true
	return r
}
func (r harvestRoute) Accepts(_ model.Fielder) router.Route { return r }
func (r harvestRoute) Describe(_ string) router.Route       { return r }

func (c *Client) op(name string) (opInfo, bool) {
	for _, o := range c.ops {
		if o.name == name {
			return o, o.declared && !o.mfa
		}
	}
	return opInfo{}, false
}
