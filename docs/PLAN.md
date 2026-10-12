---
PLAN: "feat: dbsync — offline-first sync: run operations locally, queue writes, push them with an Idempotency-Key, pull the server's change log"
TAG: v0.1.0
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 6093513914445294968
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.
>
> **Prerequisites (published tags):** `webtyp.com/rpc` **v0.2.1** (`*rpc.Caller` with
> `CallKeyed(op, key string, body []byte, into, done)`), `webtyp.com/idempotency` **v0.1.0**
> (`idempotency.MsgInProgress`), `webtyp.com/changelog` v0.1.0, `webtyp.com/outbox` v0.1.0,
> `webtyp.com/unixid` v0.3.0, `webtyp.com/router` v0.4.1, `webtyp.com/storage` **v0.1.5**,
> `webtyp.com/orm` v0.12.9, `webtyp.com/model` v0.2.2, `webtyp.com/fetch` v0.1.29,
> `webtyp.com/time` v0.5.7. If any is missing, stop and report it.

# Plan — `dbsync`: the offline-first sync engine

## 0. Context (read first)

This repository is new (created with `gonew`; module `webtyp.com/dbsync`). Its only Go file is
gonew's placeholder `dbsync.go` (`type Dbsync`, `func New`), which this plan **deletes**. Master
plan (Spanish): <https://github.com/veltylabs/mjosefa-cms/blob/main/docs/OFFLINE_FIRST_MASTER_PLAN.md>.

A clinic app must keep booking appointments and writing records while its server is unreachable.
Every browser keeps a local copy of the data (IndexedDB through `webtyp/indexdb`, injected by the
app) and runs the **same domain modules** the server runs, over that local copy. This library glues
the published pieces together:

| Piece | Already published | Role here |
|---|---|---|
| `router.OperationModule`, `router.Caller` | `webtyp/router` | modules declare operations once; views call them |
| `router/loopback.ActingAs(tenant, user, mods...)` | `webtyp/router` | runs an operation in process, with an identity |
| `outbox` | `webtyp/outbox` | ordered, durable queue of local writes; retry / reject / redeliver / prune |
| `rpc.Caller.CallKeyed` | `webtyp/rpc` | sends one queued write to `POST /api/<module>/<op>` with `Idempotency-Key` |
| `idempotency` | `webtyp/idempotency` | server middleware: a repeated key is answered once (`409 MsgInProgress` while running) |
| `changelog.Log` | `webtyp/changelog` | **server:** records every write with a version; `Since(cursor, limit)`. **Client:** records which rows a local write touched, so a write the server rejects can be undone locally |
| `unixid.NewForReplica(r, last)` | `webtyp/unixid` | ids that never collide between devices |
| `time.AfterFunc(ms, f)` | `webtyp/time` | the periodic sync timer (WASM-safe; the stdlib `time` package is forbidden here) |

Decisions this plan implements (owner, 2026-10-09):
- **One tab per browser profile** (Web Locks). Each browser / profile is its own replica (own
  IndexedDB), with its own replica number assigned by the server on first run.
- **Which rows reach each device** is declared by the app as `[]dbsync.Rule` (in its
  `config/sync.go`); the server filters with them.
- **Server identity (epoch):** every server process has an epoch; when a client sees a different
  one, it resends its recently delivered writes (safe: idempotent by key) — this covers a failover
  to a standby server that missed the last seconds.
- Writes go through the app's normal operation endpoints (no second write path): RBAC on the
  server is identical online and offline.
- **A write the server rejects is undone locally**: the rows it touched are reloaded from the
  server (or deleted locally when the server does not have them), and the write stays in the
  conflict tray (`Rejected()`) until the user dismisses it.

### APIs of the published pieces (exact; do not invent others)

```go
// webtyp.com/outbox
func New(db *orm.DB, ids model.IDGenerator, now func() int64) (*Outbox, error)
func (o *Outbox) Enqueue(e Entry) (id string, err error)        // Entry{Op string; Payload []byte; Coalesce string}
func (o *Outbox) Deliver(s Sender) (Report, error)              // blocks; Sender.Send(m *Mutation) Result
func (o *Outbox) Pending() (int, error)
func (o *Outbox) Rejected() ([]Mutation, error)
func (o *Outbox) Dismiss(id string) error
func (o *Outbox) Redeliver(since int64) (int, error)            // delivered at >= since → pending again
func (o *Outbox) Prune(before int64) (int, error)               // deletes delivered at < before
func (o *Outbox) RetryNow() error                               // clears the back-off of pending ones
// Result{Verdict Verdict; Reason string}; Verdict: outbox.Delivered | outbox.Rejected | outbox.Retry
// Mutation{Id, Op string; Payload []byte; Reason string; ...}; outbox.MutationModel / ReadOneMutation

// webtyp.com/changelog
type NewModel func() model.Model
type TxConn interface { storage.Conn; BeginTx() (storage.TxBoundExecutor, error) }
func New(conn TxConn, tracked ...NewModel) (*Log, error)       // *Log is a storage.Conn
func (l *Log) Head() int64
func (l *Log) Since(cursor int64, limit int) ([]Change, error)   // ordered by Version asc, one entry per row
// Change{Version int64; TableName, RowId, Op string}; changelog.OpUpsert = "upsert", OpDelete = "delete"

// webtyp.com/unixid
type Replica uint32
func NewForReplica(replica Replica, last int64) (*UnixID, error) // last = timestamp part of the newest id minted
func (u *UnixID) NewID() string
func (u *UnixID) Parse(id string) (timestamp int64, replica Replica, err error)

// webtyp.com/orm  (QB has NO method that takes a storage.Condition — see §1.1)
func New(conn storage.Conn) *DB
func (d *DB) Create(m model.Model) error
func (d *DB) Update(m model.Model, cond storage.Condition, rest ...storage.Condition) error
func (d *DB) Delete(m model.Model, cond storage.Condition, rest ...storage.Condition) error
func (d *DB) Query(m model.Model) *QB   // qb.Where(col).Eq(v) / Neq / Gt / Gte / Lt / Lte / Like / In; qb.Or();
                                        // qb.OrderBy(col).Asc(); qb.Limit(n); qb.ReadOne(); qb.ReadAll(new, onRow)
func IsNotFound(err error) bool
var Eq, Gt, Lte ... = storage.Eq, storage.Gt, storage.Lte ...

// webtyp.com/router
func QueryParam(path, key string) (value string, ok bool)       // path = ctx.Path()
type APIModule interface { ModelName() string; MountAPI(r Router) }
// model.Field.IsPK() tells which schema field is the primary key.
```

## Design gate

### 1. Prior art
- **Replicache / Zero (Rocicorp):** client runs named mutations locally (optimistic), pushes them
  in order with a client mutation id, the server re-executes them authoritatively, the client pulls
  a versioned diff; a mutation the server refuses is dropped from the local view. Our model.
- **PowerSync / ElectricSQL:** sync rules ("buckets") declared on the server decide which rows each
  user receives; initial snapshot then incremental changes from a checkpoint. Our `Rule` + snapshot
  + cursor.
- **CouchDB/PouchDB `_changes` + checkpoint:** cursor-based pull, deletions as tombstones. Our pull.
- **Linear's sync engine / WhatsApp Web:** one active tab owns the local database ("use here").
  Our single-tab lock.

### 2. Novice-name test
`dbsync.Open(cfg)` → "open the sync engine". `client.Caller()` → "the caller my views use".
`client.IDs()` → "the id generator of this device". `client.Sync()` → "sync now".
`client.Rejected()` / `client.Dismiss(id)` → "the conflicts" / "I resolved this one".
`dbsync.NewServer(cfg)` + `MountAPI` → "the server side". `dbsync.Rule` → "one table this device keeps".
`dbsync.LocalModels(tables)` → "every table the local database must have".

### 3. Complexity ledger
```
Concepts the developer must learn   +5 (Open/Client, NewServer/Server, Rule, Status, LocalModels)
Files they must touch to do X        +2 in the app (config/sync.go, composition root)
Lines at the call site               ~+15 client, ~+5 server
Ways to do the same thing            0 (offline modules go through Client.Caller; everything else keeps rpc)
```

### 4. Where it belongs
The sync protocol, both ends, in one repository (like `mcp`, `sse`, `rpc`). It composes the pieces
above and re-implements none: queue = `outbox`, transport = `rpc`, change source and touched-row
capture = `changelog`, local execution = `loopback`, dedupe = `idempotency` (mounted by the app).

### 5. What this deletes
gonew's placeholder `dbsync.go` (`type Dbsync`, `func New`). Otherwise nothing (new capability).

## 1. Shared API (`rule.go`)

```go
// DefaultPrefix is where the server side is mounted.
const DefaultPrefix = "/api/dbsync"

// Rule declares one table that devices keep locally, and which of its rows a user receives.
type Rule struct {
    // Model returns a fresh instance of the table's model (the same factory given to changelog.New).
    Model func() model.Model
    // Resource: the user must hold model.Read on it (via ServerConfig.Authorize) or the table is
    // skipped for that user. Required (closed by default).
    Resource model.Resource
    // Scope returns the conditions selecting the rows userID receives. nil = all rows of the table.
    Scope func(userID string) ([]storage.Condition, error)
}
```

### 1.1 Applying a scope (unexported helper `where(qb *orm.QB, conds []storage.Condition) (*orm.QB, error)`)
`orm.QB` cannot take a `storage.Condition` directly. For each condition, in order: if
`c.Logic() == "OR"` call `qb = qb.Or()` first; then by `c.Operator()`: `"="` → `qb.Where(c.Field()).Eq(c.Value())`,
`"!="` → `Neq`, `">"` → `Gt`, `">="` → `Gte`, `"<"` → `Lt`, `"<="` → `Lte`, `"LIKE"` → `Like`,
`"IN"` → `In`. Any other operator → error `dbsync: scope operator <op> is not supported` (typed
constant + `fmt.Sprintf`). The operator and logic strings are unexported constants. Used by every
server route that reads rows (snapshot, pull, row).

## 2. Server side (`server.go`; no build tag)

```go
type ServerConfig struct {
    DB        *orm.DB            // reads the app's rows; dbsync's own table (replica registry) lives here
    Log       *changelog.Log     // the change log wrapping the app's storage
    Rules     []Rule
    Authorize model.Authorizer   // required
    Now       func() int64       // unix ns; required
}

// NewServer validates cfg. Its epoch is minted once here (decimal unix ns of Now() at start).
func NewServer(cfg ServerConfig) (*Server, error)

// Server is a router.APIModule (ModelName "dbsync").
func (s *Server) ModelName() string
func (s *Server) MountAPI(r router.Router)   // r.Mount(DefaultPrefix, func(r router.Router) { ... })
```

Errors (unexported typed constants): nil `DB`/`Log`/`Authorize`/`Now`, no rules, a rule without
`Model` or `Resource`, two rules for the same table (`Model().ModelName()`).

Routes (all `.Authenticated()`; the user is `ctx.UserID()`; mounted under `DefaultPrefix`; JSON
through `ctx.Encode` with unexported `model.Encodable` response types). "A table the user may read"
= it has a rule and `Authorize(userID, rule.Resource, model.Read)` is true. Reading a row of a table
always applies the rule's `Scope(userID)` through §1.1 (a `Scope` error → 500 with its text).

| Route | Answer |
|---|---|
| `POST /register` | allocates the next replica number (max `number` + 1, starting at 1, under a mutex) in table `dbsync_replica` `{id (text PK = the number in decimal), number (int), user_id, created_at}`; answers `{"replica": n}` |
| `GET /head` | `{"epoch": e, "head": log.Head()}` |
| `GET /snapshot?table=<name>&after=<pk>&limit=<n>` | rows of that table for the user, ordered by PK ascending, `PK > after` (omitted `after` = from the start), at most `limit` (default 500, max 1000): `{"rows":[<row objects>], "next": "<last pk>"}`; `next == ""` when fewer than `limit` rows came back. Unknown table or a table the user may not read → 404 |
| `GET /pull?cursor=<c>&limit=<n>` | `changes := log.Since(cursor, limit)` (limit default 500, max 1000). Answer `{"epoch": e, "head": h, "cursor": k, "changes": [{"table","row_id","op","row"}]}` where **`k` = the `Version` of the last element of `changes` before filtering, or the request's cursor when `changes` is empty**. For each change of a table the user may read, load the row by PK **with the scope**: found → `op:"upsert"` + `row`; not found (deleted, or out of the user's scope) → `op:"delete"`, no `row`. Changes of other tables are dropped from the answer (but still advance `k`) |
| `GET /row?table=<name>&id=<pk>` | the row by PK with the scope: `{"row": <row object>}`; not found, unknown table or a table the user may not read → 404 |

Read query parameters with `router.QueryParam(ctx.Path(), key)`; numbers parsed with
`webtyp.com/fmt`. A row object is the model itself (`model.Model` is `model.Encodable`); write a
list of rows / changes with `FieldWriter.Array` and `ArrayWriter` objects.

`dbsync/migrate`: `Migrate(conn ddl.Execer, compiler ddl.Compiler) error` creates `dbsync_replica`
(same convention as `webtyp.com/changelog/migrate`).

## 3. Client side (`client.go`, `caller.go`, `sync.go`, `lock.go`, `lock_wasm.go`)

```go
type ClientConfig struct {
    Local    storage.Conn                // the browser database, opened by the app with
                                         // indexdb.New(name, dbsync.LocalModels(tables)...); must support BeginTx
    Remote   *rpc.Caller                 // pushes queued writes (CallKeyed)
    Origin   string                      // "" = same origin, for dbsync's own endpoints
    Tables   []func() model.Model        // the same factories as the server Rules (decode pulled rows)
    // Modules builds the offline modules over the local database and this device's ids.
    Modules  func(db *orm.DB, ids model.IDGenerator) ([]router.OperationModule, error)
    TenantID string
    UserID   string                      // the signed-in user (offline: the cached session)
    Now      func() int64                // unix ns
    Lock     TabLock                     // nil = browser Web Locks (wasm); tests pass a fake
}

// TabLock lets one tab of a browser profile own the local database.
type TabLock interface {
    TryLock(name string) (bool, error)
}

// LocalModels returns one instance of each app table plus dbsync's own local tables
// (dbsync_state, dbsync_touched, outbox_mutation, change_log): pass them to the local database
// constructor.
func LocalModels(tables []func() model.Model) []model.Model

func Open(cfg ClientConfig) (*Client, error)
func (c *Client) Caller() router.Caller
func (c *Client) IDs() model.IDGenerator
func (c *Client) Sync()
func (c *Client) Status() Status
func (c *Client) OnStatus(fn func(Status))
func (c *Client) Rejected() ([]outbox.Mutation, error)
func (c *Client) Dismiss(id string) error
func (c *Client) Close() error

type Status struct {
    Online   bool // the last request to the server got an HTTP answer (any status)
    Pending  int  // queued writes not delivered yet
    Rejected int  // writes the server refused (the conflict tray)
}
```

Local models (stage 1, generated with `ormc`):
- `State` (`dbsync_state`): `{id (text PK, always "state"), replica (int), cursor (int), epoch (text), last_minted (int)}`.
  `last_minted` = timestamp part of the newest id this device minted.
- `Touched` (`dbsync_touched`): `{id (text PK, minted by the device ids), mutation_id (text), table_name (text), row_id (text), created_at (int)}`.

Every request dbsync itself sends (`/register`, `/head`, `/snapshot`, `/pull`, `/row`) uses
`webtyp.com/fetch` with `.Timeout(10000)` (unexported constant) and `Origin + DefaultPrefix + route`.

### 3.1 Open (blocks; call it from the composition root, never from a JS event callback)
1. `Lock.TryLock("dbsync")` → false → `ErrAnotherTab` (exported typed error constant, text
   `dbsync: the app is already open in another tab of this browser`). `Lock == nil` in a non-wasm
   build → `dbsync: TabLock is required outside the browser`.
2. Validate: `Local`, `Remote`, `Modules`, `Now` non-nil, `Tables` non-empty, `UserID` non-empty
   (unexported typed errors). `Local` must implement `changelog.TxConn`, else
   `dbsync: the local database must support transactions (BeginTx)`.
3. Two views of the local database:
   - `raw = orm.New(cfg.Local)` — dbsync's own tables, the outbox, and **every row applied from the
     server** (snapshot, pull, revert). Writes through `raw` are never recorded.
   - `localLog = changelog.New(cfg.Local, tables...)` (the `Tables` factories converted to
     `changelog.NewModel`) and `tracked = orm.New(localLog)` — **only** the offline modules write
     through `tracked`, so `localLog` records exactly the rows a local write touched.
4. Read the state row through `raw` (not found = fresh device).
5. No replica yet → `POST /register`; no HTTP answer → `ErrFirstRunNeedsServer` (exported, text
   `dbsync: this browser must reach the server once before working offline`). Save the replica.
6. `gen = unixid.NewForReplica(unixid.Replica(replica), state.last_minted)`, wrapped in an
   unexported `model.IDGenerator` whose `NewID()` calls `gen.NewID()`, takes the timestamp with
   `gen.Parse(id)` and keeps it as the in-memory `lastMinted` (persisted into the state row whenever
   a write is enqueued, §3.2).
7. `mods, err := cfg.Modules(tracked, wrappedIDs)`. Harvest their operation metadata with an
   unexported `router.OperationRegistry` (its `Operation` returns an unexported `router.Route`
   whose `Requires` records the action; `Authenticated`, `Public`, `Accepts`, `Describe` return the
   route unchanged) that records, per qualified name `"<ModelName>.<op>"`, the action declared by
   `Requires`. No handler is kept. An operation is a **read** when its action is exactly
   `model.Read`, a **write** for any other declared action, and **not offline** when it declares no
   `Requires` at all.
8. Local executor = `loopback.ActingAs(cfg.TenantID, cfg.UserID, mods...)`.
9. Outbox = `outbox.New(raw, wrappedIDs, cfg.Now)`.
10. Cursor 0 and the server answers → **bootstrap**: `GET /head` → remember `epoch`, `head`; for each
    of `Tables` page `GET /snapshot` until `next == ""` (a 404 skips that table), upserting every row
    through `raw` (§3.3 step 3 "apply"); then `cursor = head`; save state. Cursor 0 and no answer →
    `ErrFirstRunNeedsServer`.
11. Start one background goroutine that runs `sync` (§3.3) when signalled. Signals: `Sync()` (non-
    blocking send on a channel of capacity 1) and a timer re-armed after every run with
    `time.AfterFunc(30000, signal)` (`webtyp.com/time`; 30 s is an unexported constant). `Close()`
    stops the timer and the goroutine.

### 3.2 Caller (what views call)
`Call` never blocks its caller: it does its work in a new goroutine and calls `done` from there
(`done` may be nil). `Dispatch(op, args)` = `Call(op, args, nil, nil)`.
- Not an offline operation (unknown, or no `Requires`) → `done(err)` with
  `dbsync: operation <op> is not an offline operation; call it through rpc`.
- Read → the local executor's `Call(op, args, into, done)` as is.
- Write → under the client's mutex `mu` (also held while a pull or a revert applies rows, §3.3):
  1. `before := localLog.Head()`; local executor `Call(op, args, into, …)`. Local error →
     `done(err)`, nothing queued.
  2. `touched := localLog.Since(before, 1000)`.
  3. `payload = json.Encode(args)` (`webtyp.com/json`; nil args → `{}`);
     `id := outbox.Enqueue(outbox.Entry{Op: op, Payload: payload})`.
  4. For each change in `touched`: insert a `Touched{mutation_id: id, table_name, row_id, created_at: Now()}` through `raw`.
  5. Delete the local change-log entries just read: `raw.Delete(&changelog.Change{}, orm.Gt("version", before))` (keeps `change_log` empty at rest).
  6. Persist `lastMinted` into the state row; signal `Sync`; `done(nil)`.
- Local execution ignores RBAC (loopback does); the server re-checks every write when it is pushed.

### 3.3 sync (one at a time; only the background goroutine runs it)
1. **Explicit sync:** when the run was triggered by `Sync()` (not the timer), call
   `outbox.RetryNow()` first, so a server that just came back is used at once instead of waiting
   for the back-off.
2. **Push:** `outbox.Deliver(sender)`. The unexported sender calls
   `Remote.CallKeyed(m.Op, m.Id, m.Payload, nil, done)` and waits for `done`. Verdict:
   `nil` → `Delivered`; `*rpc.Error` with `Status == 0` → `Retry`; `Status == 409` and
   `Body == idempotency.MsgInProgress` → `Retry`; `Status` 401, 408, 429 or ≥ 500 → `Retry`;
   any other status → `Rejected` with `Body` as `Reason`. On `Delivered` it deletes the `Touched`
   rows of `m.Id`.
3. **Revert rejected writes:** for each mutation of `outbox.Rejected()` that still has `Touched`
   rows: for each one, `GET /row?table=&id=` → 200: apply as upsert; 404: delete the local row.
   Then delete that `Touched` row. A request without an HTTP answer stops this step (it is retried
   on the next run). **Apply** (used here, by pull and by bootstrap), under `mu`, through `raw`:
   pick the factory of `Tables` whose `ModelName()` equals the table (none → skip the change);
   upsert → decode the row into a fresh model, read by PK; exists → `Update(row, Eq(pk, id))`,
   else `Create(row)`; delete → `Delete(fresh, Eq(pk, id))`. The PK column is the schema field with
   `IsPK()`.
4. **Pull only when nothing is pending** (`outbox.Pending() == 0`): loop
   `GET /pull?cursor=<c>&limit=500`; apply each change; after each page save
   `cursor = response.cursor`; stop when `response.cursor >= response.head`. (Never set the cursor
   to `head`: a page can end before `head`, and a page whose changes were all filtered out comes back
   with an empty `changes` but a larger `cursor`.)
5. **Epoch:** if a response's `epoch` differs from the stored one and the stored one is not empty →
   `outbox.Redeliver(now − 10 min)` (unexported constant) and push again; store the new epoch.
6. **Prune:** `outbox.Prune(now − 10 min)` (the same window as step 5), then delete the `Touched`
   rows older than that window whose mutation no longer exists in the outbox
   (`outbox.ReadOneMutation` by id → not found).
7. Update `Status` (`Online` = the last request got any HTTP answer; `Pending`, `Rejected` from the
   outbox) and call the `OnStatus` callbacks when it changes.

`Dismiss(id)` = `outbox.Dismiss(id)`, then refresh `Status`.

### 3.4 TabLock in the browser (`lock_wasm.go`, `//go:build wasm`)
`navigator.locks.request(name, {ifAvailable: true}, cb)`: when `cb` receives a lock, return a
promise that never resolves (the lock is held for the page's life) and report `true`; when it
receives `null`, report `false`. Use `syscall/js` only here. `lock.go` (`//go:build !wasm`) holds
the non-browser default (`nil` → the "TabLock is required" error).

### 3.5 Known limits (do NOT implement; documented in `docs/ARCHITECTURE.md`)
- The local write (§3.2 step 1) and its enqueue (step 3) are not one transaction: a page closed
  between them keeps the local row without queueing it.
- `rpc.Caller` has no request timeout; a push to a server that never answers waits for the browser's
  own network timeout.

## 4. Stages

| Stage | Files | Content |
|---|---|---|
| 1 | `go.mod`, `model.go`, `model_orm.go`, delete `dbsync.go` | deps of the header; models `State` (`dbsync_state`), `Touched` (`dbsync_touched`) and `Replica` (`dbsync_replica`); run `ormc` |
| 2 | `rule.go`, `errors.go` | §1 (with §1.1), error constants |
| 3 | `server.go`, `migrate/migrate.go` | §2 |
| 4 | `client.go`, `caller.go`, `sync.go`, `lock.go`, `lock_wasm.go` | §3 |
| 5 | `tests/` (nested module `tests/go.mod` with `replace webtyp.com/dbsync => ../`, like `webtyp/rpc`) | §5 |
| 6 | `README.md`, `docs/ARCHITECTURE.md` | the table of §0, a sequence diagram (mermaid) of write → queue → push → pull and of reject → revert, the app wiring of §6, the known limits of §3.5 |

## 5. Tests (consumer-shaped, end to end; files under `tests/` carry `//go:build !wasm`)

Server: a real `webtyp.com/server/httpd` router on `httptest` (as `webtyp/rpc` and
`webtyp/idempotency` tests do) with an `Authn` that sets a fixed user and an `Authorize` that allows
everything. Storage: `raw := mem.New()`; `log := changelog.New(raw.(changelog.TxConn), noteFactory, otherFactory)`;
`db := orm.New(log)`. A test module `notes` (`create_note` Requires `note`/Create and answers
**409 `taken`** when `Text == "taken"`; `update_note` Requires `note`/Update and answers **409 `locked`** when `Text == "locked"`; `list_notes` Requires
`note`/Read); model `Note{Id, Owner, Text}` and a second model `Other{Id}` (tracked by the log, **no
rule**), hand-written in the test package with `ModelName/Schema/Pointers/EncodeFields/DecodeFields/IsNil`.
`idempotency.New(orm.New(idemConn), now)` middleware with `Use` **before** `rpc.Mount`;
`dbsync.NewServer` with one rule for `notes` (`Resource "note"`) whose `Scope` returns
`[]storage.Condition{storage.Eq("owner", userID)}`.
Client: `dbsync.Open` with `mem.New()` as `Local`, `rpc.NewCaller(ts.URL)`, `Origin: ts.URL`, a fake
`TabLock`, the same `notes` module built over the `db` the client passes to `Modules`.

**Stopping and restarting the server:** keep the address of the first `httptest.Server`; stop it
with `Close()` (requests then fail with no HTTP answer); restart with `httptest.NewUnstartedServer`
whose `Listener` is `net.Listen("tcp", addr)` on the same address, then `Start()`. A restart builds a
new router with the same modules, the same `db` and `log`, a **new** `dbsync.NewServer` (new epoch)
and — unless the test says otherwise — the **same** idempotency store.

1. Bootstrap: rows created on the server before `Open` are in the local db after `Open`, only the
   user's own (`owner` scope).
2. Online write: `Caller().Call("notes.create_note", …)` → visible locally at once; after `Sync`,
   present on the server; `Status().Pending == 0`.
3. Offline write: stop the server, create a note → local row exists, `Pending == 1`,
   `Online == false`; restart, `Sync` → delivered, present on the server once.
4. Lost answer: deliver a note, then restart the server (new epoch, **same** idempotency store) →
   `Sync` redelivers it and the `create_note` handler ran **once** in total (count in the handler).
5. Rejected and reverted: create a note with `Text: "taken"` → after `Sync` it is **gone from the
   local db**, `Rejected()` lists it with reason `taken`, `Status().Rejected == 1`; `Dismiss`
   removes it; a later write still goes through.
6. Rejected update restores the server's row: a note with `Text: "a"` exists on both sides;
   `Caller().Call("notes.update_note", …)` with `Text: "locked"` → locally `"locked"` at once; after
   `Sync` the local row has `"a"` again and `Rejected()` lists the update with reason `locked`.
7. Pull: a note created on the server by another path appears locally after `Sync`; a note deleted
   on the server disappears locally; a note moved out of the user's scope (owner changed on the
   server) disappears locally.
8. Pull pages past filtered changes: create 600 `Other` rows on the server and then one note →
   after `Sync` the note is local and the stored cursor equals the server's head.
9. Pull waits for push: a test middleware answers 503 to `POST /api/notes/create_note` while a
   flag is set. Set it; create a note through `Caller()`; create another note on the server by
   another path; `Sync` → the server's note is **not** local (nothing was pulled) and `Pending == 1`.
   Clear the flag; `Sync` → both notes on both sides.
10. Epoch change (failover to a standby that missed the last seconds): deliver a note; delete it on
    the server; restart with a new epoch **and a new idempotency store over a fresh `mem.New()`**
    (the standby never saw that key) → after `Sync` the client redelivered it and it exists again.
11. Second tab: a fake lock that answers false → `Open` returns `ErrAnotherTab`.
12. First run offline → `ErrFirstRunNeedsServer`.
13. Unknown operation, and an operation without `Requires`, through `Caller()` → the "not an
    offline operation" error.
14. Unsupported scope operator (`storage.IsNotNull`) → `/snapshot` answers 500 with the
    "scope operator" message.

## 6. App wiring (README, verbatim intent)

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

// browser
local, _ := indexdb.New("app", dbsync.LocalModels(config.SyncTables())...)
client, err := dbsync.Open(dbsync.ClientConfig{Local: local, Remote: rpc.NewCaller(""), Tables: config.SyncTables(), Modules: config.OfflineModules, TenantID: t, UserID: u, Now: now})
// views of offline modules get client.Caller(); forms get client.IDs(); the SSE reconnect calls client.Sync()
```

## 7. Code rules (non-negotiable)
- Client code compiles to TinyGo WASM: `webtyp.com/fmt`, `webtyp.com/json`, `webtyp.com/fetch`,
  `webtyp.com/time`; no `errors`, `strconv`, `strings`, `encoding/json`, `net/http`, stdlib `time` in
  library code; no `map`. `syscall/js` only in `lock_wasm.go`. `sync` (mutex) is allowed.
- Repeated strings (routes, query keys, JSON keys, table names, operators, messages) are constants.
- No exported symbol beyond §1–§3 and the generated models. Tests in `tests/`; never export for a test.

## 8. Acceptance criteria
- `gotest ./...` green; `GOOS=js GOARCH=wasm go build ./...` succeeds from the module root (the
  client compiles for the browser).
- `grep -rn "map\[" --include=*.go . | grep -v _test.go` → empty.
- `grep -rn "\"time\"" --include=*.go . | grep -v tests/` → empty.
- `test ! -e dbsync.go && ! grep -rn "type Dbsync" --include=*.go .` → succeeds.
- **Before opening the PR:** `git diff --stat main` lists every file of the stages table, and no file
  is deleted that this plan does not order deleted.
