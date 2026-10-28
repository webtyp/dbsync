//go:build wasm

package dbsync

import (
	"webtyp.com/router"
)

type serverEvents struct{}

func (s *Server) setupEvents() {}

func (s *Server) mountEvents(r router.Router) {}
