package dbsync

import (
	"webtyp.com/changelog"
	"webtyp.com/json"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/outbox"
	"webtyp.com/router"
)

// Caller is what the views of offline modules call: reads and writes run on the local database
// at once; writes are also queued and pushed to the server.
func (c *Client) Caller() router.Caller { return offlineCaller{c: c} }

type offlineCaller struct{ c *Client }

// Call never blocks its caller: the work runs in its own goroutine and done is called from there.
func (o offlineCaller) Call(op string, args model.Encodable, into model.Decodable, done func(err error)) {
	go func() {
		err := o.c.call(op, args, into)
		if done != nil {
			done(err)
		}
	}()
}

func (o offlineCaller) Dispatch(op string, args model.Encodable) { o.Call(op, args, nil, nil) }

func (c *Client) call(op string, args model.Encodable, into model.Decodable) error {
	info, ok := c.op(op)
	if !ok {
		return errNotOfflineOp(op)
	}
	if info.action == model.Read {
		return c.runLocal(op, args, into)
	}
	if err := c.write(op, args, into); err != nil {
		return err
	}
	c.wake()
	return nil
}

// runLocal runs op on the local modules (loopback answers synchronously).
func (c *Client) runLocal(op string, args model.Encodable, into model.Decodable) error {
	var out error
	c.local.Call(op, args, into, func(err error) { out = err })
	return out
}

// write runs op locally, queues it, and records which rows it touched (to undo it if the server
// rejects it).
func (c *Client) write(op string, args model.Encodable, into model.Decodable) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	before := c.localLog.Head()
	if err := c.runLocal(op, args, into); err != nil {
		return err
	}
	touched, err := c.localLog.Since(before, maxTouched)
	if err != nil {
		return err
	}
	payload := []byte(emptyObject)
	if args != nil && !model.IsNil(args) {
		payload = nil
		if err := json.Encode(args, &payload); err != nil {
			return err
		}
	}
	id, err := c.box.Enqueue(outbox.Entry{Op: op, Payload: payload})
	if err != nil {
		return err
	}
	now := c.cfg.Now()
	for _, t := range touched {
		rec := &Touched{Id: c.ids.NewID(), MutationId: id, TableName: t.TableName, RowId: t.RowId, CreatedAt: now}
		if err := c.raw.Create(rec); err != nil {
			return err
		}
	}
	// Keep the local change log empty at rest: it only exists to tell which rows a write touched.
	if len(touched) > 0 {
		if err := c.raw.Delete(&changelog.Change{}, orm.Gt(changelog.Change_.Version, before)); err != nil {
			return err
		}
	}
	return c.saveState()
}

const emptyObject = "{}"
