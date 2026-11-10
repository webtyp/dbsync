//go:build !wasm

package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"webtyp.com/dbsync"
	"webtyp.com/router"
	"webtyp.com/server/httpd"
)

// sseURL serves the dbsync events stream of s on a real httpd server and returns its base URL.
// routertest.Handler buffers one response per request, so it cannot serve a Stream route: these two tests
// are the only ones that need httpd, which is why it lives in this tests/ module (its own go.mod) and not in dbsync's.
func sseURL(t *testing.T, s *server) string {
	t.Helper()
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
	syncSrv, err := dbsync.NewServer(dbsync.ServerConfig{
		DB:        s.db,
		Log:       s.log,
		Rules:     []dbsync.Rule{{Model: noteFactory, Resource: "note", Scope: s.scope, Fields: s.fields}},
		Authorize: allow,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("dbsync.NewServer: %v", err)
	}
	syncSrv.MountAPI(srv.Router())
	h, err := srv.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	ts := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	t.Cleanup(ts.Close)
	return ts.URL
}
