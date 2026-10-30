# `webtyp.com/dbsync`
<img src="docs/img/badges.svg">

Offline-first sync for webtyp apps: every browser runs the same domain modules over its own local
database, so reads and writes keep working while the server is unreachable. Writes are queued and
pushed to their normal endpoints with an `Idempotency-Key`; the server's change log flows back.

How it works, the protocol and its limits: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Wiring

```go
now := time.NowNano                              // webtyp.com/time: the instant of each change

// server
log, _ := changelog.New(raw, config.SyncTableFactories()...)
db := orm.New(log)
store, _ := idempotency.New(db, now)
srv.Router().Use(store.Middleware())                           // before mounting routes
_ = rpc.Mount(srv.Router(), rpc.DefaultPrefix, modules...)
syncSrv, _ := dbsync.NewServer(dbsync.ServerConfig{DB: db, Log: log, Rules: config.SyncRules(), Authorize: authz, Now: now})
syncSrv.MountAPI(srv.Router())

// browser (call Open from the composition root, never from a JavaScript event callback: it blocks)
local, _ := indexdb.New("app", dbsync.LocalModels(config.SyncTables())...)
client, err := dbsync.Open(dbsync.ClientConfig{Local: local, Remote: rpc.NewCaller(""), Tables: config.SyncTables(), Modules: config.OfflineModules, TenantID: t, UserID: u, Now: now, Records: records})
// views of offline modules get client.Caller(); forms get client.IDs(); the SSE reconnect calls client.Sync()
// the server announces the head over SSE at /sync/events; the timer is the fallback.
```

`config/sync.go` declares which tables each device keeps:

```go
func SyncRules() []dbsync.Rule {
	return []dbsync.Rule{
		{Model: func() model.Model { return &booking.Reservation{} }, Resource: "reservation"},
		{Model: func() model.Model { return &records.Visit{} }, Resource: "visit",
			Scope: func(userID string) ([]storage.Condition, error) {
				return []storage.Condition{storage.Eq("staff_id", userID)}, nil
			}},
		{Model: func() model.Model { return &patients.Patient{} }, Resource: "patient",
			Fields: func(userID string) ([]string, error) { // a box keeps a minimal directory
				if isBox(userID) {
					return []string{"name", "rut"}, nil
				}
				return nil, nil // reception: every column
			}},
	}
}
```

## API

| Symbol | Side | What |
|---|---|---|
| `Rule{Model, Resource, Scope, Fields}` | both | one table devices keep; `model.Read` on `Resource` and `Scope` (AND-joined conditions) decide which rows a user receives; `Fields` which columns (the primary key always travels) |
| `NewServer(ServerConfig)` / `MountAPI` | server | `/api/dbsync/register`, `/head`, `/snapshot`, `/pull`, `/row` |
| `migrate.Migrate` | server | creates `dbsync_replica` with `ddl.Sync` ([flow](https://github.com/webtyp/ddl/blob/main/docs/diagrams/SYNC_FLOW.md)) |
| `LocalModels(tables)` | browser | every table the local database must have |
| `Open(ClientConfig)` | browser | tab lock, replica number, first copy, background sync |
| `Client.Caller()` / `IDs()` | browser | what views and forms use |
| `Client.Sync()` / `Status()` / `OnStatus(fn)` | browser | sync now; the connection indicator |
| `Client.Rejected()` / `Dismiss(id)` | browser | the conflict tray |
| `ErrAnotherTab`, `ErrFirstRunNeedsServer` | browser | the two errors an app shows to the user |

## Showing it to the person

`webtyp.com/dbsync/ui` (browser only; the engine never imports it) has the two things a person
sees. The app puts the indicator in the header slot of `platformd.Platform`:

```go
import "webtyp.com/dbsync/ui"

Status: func() Component { return ui.Indicator(client) },
```

The indicator says "Online" or "Offline" with two counts: writes waiting to be sent and writes the
server refused. Clicking it opens the conflict tray (`ui.Tray(client)`), which lists each refused
write with the server's reason and a Dismiss button. Build each one once per page: each subscribes
to `OnStatus`. Their styles come from `ui.RenderCSS()`.
