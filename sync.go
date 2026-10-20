package dbsync

import (
	"webtyp.com/fetch"
	"webtyp.com/fmt"
	"webtyp.com/idempotency"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/outbox"
	"webtyp.com/rpc"
	wtime "webtyp.com/time"
)

const (
	statusConflict        = 409
	statusUnauthorized    = 401
	statusRequestTimeout  = 408
	statusTooManyRequests = 429
	statusServerErrorMin  = 500
)

// loop runs one sync per signal until Close.
func (c *Client) loop() {
	for {
		select {
		case <-c.closed:
			return
		case <-c.signal:
			c.sync(c.explicit.Swap(false))
			c.arm()
		}
	}
}

// arm schedules the next periodic sync.
func (c *Client) arm() {
	c.timerMu.Lock()
	defer c.timerMu.Unlock()
	select {
	case <-c.closed:
		return
	default:
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = wtime.AfterFunc(syncEveryMs, c.wake)
}

// sync: epoch check, push, undo rejected writes, pull (only when nothing is pending), prune.
// Errors end the run early; the next run tries again.
func (c *Client) sync(explicit bool) {
	defer c.refreshStatus()
	if explicit {
		// A server that just came back is used at once instead of waiting for the back-off.
		c.box.RetryNow()
	}
	var h headWire
	if status, _ := c.request(fetch.Get, routeHead, &h); status != statusOK {
		return
	}
	if err := c.checkEpoch(h.epoch); err != nil {
		return
	}
	if _, err := c.box.Deliver(sender{c: c}); err != nil {
		return
	}
	if err := c.revertRejected(); err != nil {
		return
	}
	if pending, err := c.box.Pending(); err != nil || pending > 0 {
		return
	}
	if err := c.pull(); err != nil {
		return
	}
	c.prune()
}

// checkEpoch: a new server process (restart, or failover to a standby that may have missed the
// last seconds) → resend what was delivered recently. Safe: every send is idempotent by its key.
func (c *Client) checkEpoch(epoch string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if epoch == c.state.Epoch {
		return nil
	}
	if c.state.Epoch != "" {
		if _, err := c.box.Redeliver(int64(c.cfg.Now() - redeliverWindow)); err != nil {
			return err
		}
	}
	c.state.Epoch = epoch
	return c.saveState()
}

// sender pushes one queued write to its normal endpoint, keyed by the mutation id.
type sender struct{ c *Client }

func (s sender) Send(m *outbox.Mutation) outbox.Result {
	ch := make(chan error, 1)
	s.c.cfg.Remote.CallKeyed(m.Op, m.Id, m.Payload, nil, func(err error) { ch <- err })
	err := <-ch
	if err == nil {
		s.c.online.Store(true)
		s.c.raw.Delete(&Touched{}, orm.Eq(Touched_.MutationId, m.Id))
		return outbox.Result{Verdict: outbox.Delivered}
	}
	e, ok := err.(*rpc.Error)
	if !ok {
		return outbox.Result{Verdict: outbox.Retry, Reason: err.Error()}
	}
	s.c.online.Store(e.Status != 0)
	return outbox.Result{Verdict: verdict(e), Reason: e.Body}
}

// verdict: no answer, still running, auth, throttling and server errors are retried; any other
// refusal goes to the conflict tray.
func verdict(e *rpc.Error) outbox.Verdict {
	switch {
	case e.Status == 0,
		e.Status == statusConflict && e.Body == idempotency.MsgInProgress,
		e.Status == statusUnauthorized,
		e.Status == statusRequestTimeout,
		e.Status == statusTooManyRequests,
		e.Status >= statusServerErrorMin:
		return outbox.Retry
	}
	return outbox.Rejected
}

// revertRejected reloads from the server every row a rejected write touched, or deletes it
// locally when the server does not have it (in the user's scope).
func (c *Client) revertRejected() error {
	rejected, err := c.box.Rejected()
	if err != nil {
		return err
	}
	for _, m := range rejected {
		touched, err := ReadAllTouched(c.raw.Query(&Touched{}).Where(Touched_.MutationId).Eq(m.Id))
		if err != nil {
			return err
		}
		for _, t := range touched {
			if err := c.revertRow(t); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) revertRow(t *Touched) error {
	f := factoryOf(c.cfg.Tables, t.TableName)
	if f != nil {
		w := &rowWire{row: f()}
		route := fmt.Sprintf("%s?%s=%s&%s=%s", routeRow, queryTable, t.TableName, queryID, t.RowId)
		status, err := c.request(fetch.Get, route, w)
		if status == 0 || (err != nil && status != statusNotFound) {
			return err
		}
		c.mu.Lock()
		if status == statusNotFound {
			err = c.deleteLocal(f, t.RowId)
		} else {
			err = c.upsert(f, w.row)
		}
		c.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return c.raw.Delete(&Touched{}, orm.Eq(Touched_.Id, t.Id))
}

// pull applies the server's changes after the stored cursor, page by page, until it reaches head.
func (c *Client) pull() error {
	for {
		w := &pullWire{tables: c.cfg.Tables}
		route := fmt.Sprintf("%s?%s=%d&%s=%d", routePull, queryCursor, c.cursor(), queryLimit, defaultPageLimit)
		if _, err := c.request(fetch.Get, route, w); err != nil {
			return err
		}
		if w.epoch != c.epoch() {
			// The server changed under us: resend first (next run), pull later.
			err := c.checkEpoch(w.epoch)
			c.wake()
			return err
		}
		if err := c.applyPage(w); err != nil {
			return err
		}
		if w.cursor >= w.head {
			return nil
		}
	}
}

func (c *Client) applyPage(w *pullWire) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range w.changes {
		f := factoryOf(c.cfg.Tables, ch.table)
		if f == nil {
			continue
		}
		var err error
		if ch.op == opUpsert && ch.row != nil {
			err = c.upsert(f, ch.row)
		} else if ch.op == opDelete {
			err = c.deleteLocal(f, ch.rowID)
		}
		if err != nil {
			return err
		}
	}
	c.state.Cursor = w.cursor
	return c.saveState()
}

// bootstrap copies every table the user may read, then starts the feed at the head read before
// copying (changes made meanwhile are pulled again: applying them twice is harmless).
func (c *Client) bootstrap() error {
	var h headWire
	status, err := c.request(fetch.Get, routeHead, &h)
	if status == 0 {
		return ErrFirstRunNeedsServer
	}
	if err != nil {
		return err
	}
	for _, f := range c.cfg.Tables {
		table := f().ModelName()
		after := ""
		for {
			w := &snapshotIn{factory: f}
			route := fmt.Sprintf("%s?%s=%s&%s=%d", routeSnapshot, queryTable, table, queryLimit, defaultPageLimit)
			if after != "" {
				route += fmt.Sprintf("&%s=%s", queryAfter, after)
			}
			status, err := c.request(fetch.Get, route, w)
			if status == statusNotFound {
				break // the user may not read this table
			}
			if status == 0 {
				return ErrFirstRunNeedsServer
			}
			if err != nil {
				return err
			}
			c.mu.Lock()
			for _, row := range w.rows {
				if err = c.upsert(f, row); err != nil {
					break
				}
			}
			c.mu.Unlock()
			if err != nil {
				return err
			}
			if w.next == "" {
				break
			}
			after = w.next
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Cursor, c.state.Epoch = h.head, h.epoch
	return c.saveState()
}

// prune forgets delivered writes older than the redelivery window, and the touched-row records of
// writes that no longer exist (dismissed or pruned).
func (c *Client) prune() {
	cutoff := c.cfg.Now() - redeliverWindow
	if _, err := c.box.Prune(int64(cutoff)); err != nil {
		return
	}
	old, err := ReadAllTouched(c.raw.Query(&Touched{}).Where(Touched_.CreatedAt).Lt(cutoff))
	if err != nil {
		return
	}
	for _, t := range old {
		m := &outbox.Mutation{}
		err := c.raw.Query(m).Where(outbox.Mutation_.Id).Eq(t.MutationId).ReadOne()
		if orm.IsNotFound(err) {
			c.raw.Delete(&Touched{}, orm.Eq(Touched_.Id, t.Id))
		}
	}
}

// upsert writes a row received from the server into the local database (never recorded, never
// queued). Callers hold c.mu.
func (c *Client) upsert(f func() model.Model, row model.Model) error {
	pk, err := pkName(row)
	if err != nil {
		return err
	}
	id, err := pkValue(row)
	if err != nil {
		return err
	}
	err = c.raw.Query(f()).Where(pk).Eq(id).ReadOne()
	if orm.IsNotFound(err) {
		return c.raw.Create(row)
	}
	if err != nil {
		return err
	}
	return c.raw.Update(row, orm.Eq(pk, id))
}

// deleteLocal removes a row the server no longer has (or that left the user's scope). Callers
// hold c.mu.
func (c *Client) deleteLocal(f func() model.Model, id string) error {
	m := f()
	pk, err := pkName(m)
	if err != nil {
		return err
	}
	return c.raw.Delete(m, orm.Eq(pk, id))
}

func (c *Client) cursor() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.Cursor
}

func (c *Client) epoch() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.Epoch
}
