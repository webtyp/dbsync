package dbsync

import (
	"webtyp.com/model"
	"webtyp.com/outbox"
	"webtyp.com/rpc"
	webtyptime "webtyp.com/time"
)

func (c *Client) initAndLoop() error {
	return nil
}

func (c *Client) runLoop() {
	c.schedule()
	for {
		select {
		case <-c.syncSig:
			c.doSync(true)
		case <-c.closeCh:
			return
		}
	}
}

func (c *Client) schedule() {
	if c.timerStop != nil {
		c.timerStop()
	}
	t := webtyptime.AfterFunc(30000, func() {
		c.Sync()
		c.schedule()
	})
	c.timerStop = func() { t.Stop() }
}

func (c *Client) doSync(explicit bool) {
	// mock
}

func (c *Client) getModelFactory(table string) func() model.Model {
	for _, f := range c.cfg.Tables {
		if f().ModelName() == table {
			return f
		}
	}
	return nil
}

func (c *Client) getPK(m model.Model) string {
	for _, f := range m.Schema() {
		if f.DB != nil && f.DB.PK {
			return f.Name
		}
	}
	return ""
}

type outboxSender struct {
	c *Client
}

func (s *outboxSender) Send(m *outbox.Mutation) outbox.Result {
	op := m.Op
	id := m.Id
	payload := m.Payload

	var rpcErr *rpc.Error
	done := make(chan struct{})

	s.c.cfg.Remote.CallKeyed(op, id, payload, nil, func(err error) {
		if err != nil {
			if re, ok := err.(*rpc.Error); ok {
				rpcErr = re
			} else {
				rpcErr = &rpc.Error{Status: 0, Body: err.Error()}
			}
		}
		close(done)
	})

	<-done

	if rpcErr == nil {
		return outbox.Result{Verdict: outbox.Delivered}
	}

	if rpcErr.Status == 0 {
		return outbox.Result{Verdict: outbox.Retry}
	}

	if rpcErr.Status == 409 && string(rpcErr.Body) == "MsgInProgress" {
		return outbox.Result{Verdict: outbox.Retry}
	}

	if rpcErr.Status == 401 || rpcErr.Status == 408 || rpcErr.Status == 429 || rpcErr.Status >= 500 {
		return outbox.Result{Verdict: outbox.Retry}
	}

	return outbox.Result{Verdict: outbox.Rejected, Reason: string(rpcErr.Body)}
}
