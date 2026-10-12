package dbsync

import (
	"sync"

	webtypfmt "webtyp.com/fmt"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/outbox"
	"webtyp.com/router"
	"webtyp.com/rpc"
	"webtyp.com/storage"
	"webtyp.com/unixid"
)

type ClientConfig struct {
	Local    storage.Conn
	Remote   *rpc.Caller
	Origin   string
	Tables   []func() model.Model
	Modules  func(db *orm.DB, ids model.IDGenerator) ([]router.OperationModule, error)
	TenantID string
	UserID   string
	Now      func() int64
	Lock     TabLock
}

// LocalModels returns one instance of each app table plus dbsync's own local tables
func LocalModels(tables []func() model.Model) []model.Model {
	var ms []model.Model
	ms = append(ms, &State{}, &Touched{})
	for _, f := range tables {
		ms = append(ms, f())
	}
	return ms
}

type Status struct {
	Online   bool
	Pending  int
	Rejected int
}

type Client struct {
	cfg        ClientConfig
	raw        *orm.DB
	tracked    *orm.DB
	outbox     *outbox.Outbox
	executor   router.Caller
	wrappedIDs model.IDGenerator

	mu         sync.Mutex // guards write path and row application
	statusMu   sync.Mutex
	status     Status
	statusCB   []func(Status)

	state      *State

	syncSig    chan struct{}
	closeCh    chan struct{}
	timerStop  func()

	opMap      map[string]router.Route
}

type wrappedIDGen struct {
	gen        *unixid.UnixID
	c          *Client
	lastMinted int64
	mu         sync.Mutex
}

func (w *wrappedIDGen) NewID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	id := w.gen.NewID()
	w.lastMinted, _, _ = w.gen.Parse(id)
	return id
}

func Open(cfg ClientConfig) (*Client, error) {
	l := cfg.Lock
	if l == nil {
		l = defaultTabLock()
	}
	if l == nil {
		return nil, webtypfmt.Err(errTabLockRequired)
	}

	ok, err := l.TryLock("dbsync")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrAnotherTab
	}

	c := &Client{
		cfg:      cfg,
		syncSig:  make(chan struct{}, 1),
		closeCh:  make(chan struct{}),
	}

	err = c.initAndLoop()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Client) IDs() model.IDGenerator {
	return c.wrappedIDs
}

func (c *Client) Sync() {
	select {
	case c.syncSig <- struct{}{}:
	default:
	}
}

func (c *Client) Status() Status {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	return c.status
}

func (c *Client) OnStatus(fn func(Status)) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	c.statusCB = append(c.statusCB, fn)
}

func (c *Client) Rejected() ([]outbox.Mutation, error) {
	if c.outbox == nil {
		return nil, nil
	}
	return c.outbox.Rejected()
}

func (c *Client) Dismiss(id string) error {
	if c.outbox == nil {
		return nil
	}
	err := c.outbox.Dismiss(id)
	if err == nil {
		c.updateStatus(c.Status().Online)
	}
	return err
}

func (c *Client) Close() error {
	close(c.closeCh)
	if c.timerStop != nil {
		c.timerStop()
	}
	return nil
}

func (c *Client) updateStatus(online bool) {
	c.statusMu.Lock()
	old := c.status

	if c.outbox != nil {
		c.status.Pending, _ = c.outbox.Pending()
		rej, _ := c.outbox.Rejected()
		c.status.Rejected = len(rej)
	}
	c.status.Online = online

	changed := old != c.status
	c.statusMu.Unlock()

	if changed {
		for _, fn := range c.statusCB {
			fn(c.status)
		}
	}
}
