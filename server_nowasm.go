//go:build !wasm

package dbsync

import (
	"webtyp.com/sse"
	"webtyp.com/model"
	"webtyp.com/fmt"
	"webtyp.com/router"
)

type serverEvents struct {
	events *sse.SSEServer
}

type headChannel struct{}

func (headChannel) ResolveChannels(ctx router.Context) ([]string, error) {
	return []string{channelHead}, nil
}

func (s *Server) setupEvents() {
	s.events = sse.New(&sse.Config{}).Server(&sse.ServerConfig{
		Path:            DefaultPrefix + routeEvents,
		Access:          model.AccessAuthenticated,
		ChannelProvider: headChannel{},
	})
	s.cfg.Log.OnChange(func(head int64) {
		s.events.Publish([]byte(fmt.Convert(head).String()), channelHead)
	})
}

func (s *Server) mountEvents(r router.Router) {
	s.events.MountAPI(r)
}
