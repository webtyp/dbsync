package dbsync

import (
	"webtyp.com/router"
	"webtyp.com/model"
)

type offlineCaller struct {
	c   *Client
}

func (c *Client) Caller() router.Caller {
	return &offlineCaller{
		c:   c,
	}
}

func (o *offlineCaller) Call(op string, args model.Encodable, into model.Decodable, done func(error)) {
	if done == nil {
		done = func(error) {}
	}

	go func() {
		o.c.Sync()
		done(nil)
	}()
}

func (o *offlineCaller) Dispatch(op string, args model.Encodable) {
	o.Call(op, args, nil, nil)
}
