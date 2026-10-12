# `webtyp.com/dbsync`
<img src="docs/img/badges.svg">

Offline-first sync for webtyp apps: every browser runs the same domain modules over its own local
database, so reads and writes keep working while the server is unreachable. Writes are queued and
pushed to their normal endpoints with an `Idempotency-Key`; the server's change log flows back.

How it works, the protocol and its limits: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Wiring

```go
now := func() int64 { return time.Now().UnixNano() }

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
client, err := dbsync.Open(dbsync.ClientConfig{Local: local, Remote: rpc.NewCaller(""), Tables: config.SyncTables(), Modules: config.OfflineModules, TenantID: t, UserID: u, Now: now})
// views of offline modules get client.Caller(); forms get client.IDs(); the SSE reconnect calls client.Sync()
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
	}
}
```

## API

| Symbol | Side | What |
|---|---|---|
| `Rule{Model, Resource, Scope}` | both | one table devices keep; `model.Read` on `Resource` and `Scope` (AND-joined conditions) decide which rows a user receives |
| `NewServer(ServerConfig)` / `MountAPI` | server | `/api/dbsync/register`, `/head`, `/snapshot`, `/pull`, `/row` |
| `migrate.Migrate` | server | creates `dbsync_replica` |
| `LocalModels(tables)` | browser | every table the local database must have |
| `Open(ClientConfig)` | browser | tab lock, replica number, first copy, background sync |
| `Client.Caller()` / `IDs()` | browser | what views and forms use |
| `Client.Sync()` / `Status()` / `OnStatus(fn)` | browser | sync now; the connection indicator |
| `Client.Rejected()` / `Dismiss(id)` | browser | the conflict tray |
| `ErrAnotherTab`, `ErrFirstRunNeedsServer` | browser | the two errors an app shows to the user |
