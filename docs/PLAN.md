---
PLAN: "refactor!: offline operations are router.Operation; the queue stores Operation.String(); the Caller and the harvester take the typed operation"
EXECUTOR: jules
REVIEWER: none
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit after every stage, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.
> Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own work.
> **The last step is `git push` and `gh pr create`; your final message is the pull request link, never a question.** Commit after every stage so nothing is lost.

## Goal

`dbsync` runs an offline module's operations on the local database and queues the writes for the server. Everywhere it handles an operation as a string
(`"notes.note_c"`): the harvester, `Client.op`, the `Caller`, the queue row (`outbox.Entry.Op`) and the push (`rpc.Caller.CallKeyed(m.Operation, …)`). After this plan it holds a `router.Operation`;
only the queue row is text, written with `op.String()` and read back with `router.ParseOperation`. A queued row in the old format (`notes.note_c`) is not silently dropped: it goes to the conflict tray with an explanatory reason.

## Ecosystem search

Searched `Operation`, `Entry`, `ParseOperation`, `harvest` over `webtyp/*`: `outbox.Entry.Op string` is opaque to `outbox` (**reuse** unchanged: the queue stores text); `router.Operation`/`ParseOperation` (router plan) are the typed identity and its persistence form; `crud.Mount`/`crud.LoopbackActingAs` (crud plan). No new library.

## Design gate

1. **Prior art**: none new; this follows the `router`/`rpc` plans.
2. **Novice-name test**: the tray shows `notes.note.create` instead of `notes.note_c`.
3. **Ledger**: concepts 0; operation strings in logic −5; ways to name an operation 2 → 1.
4. **Where**: `dbsync` owns the offline queue and how it stores an operation; `router` owns the type.
5. **Deletes**: `harvester.module`, the `h.module + "." + name` concatenation, string comparison in `Client.op`.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
```

Dependencies (all published, no `replace`): `go get webtyp.com/model@v0.2.18 webtyp.com/router@v0.4.21 webtyp.com/crud@v0.0.10 webtyp.com/storage@v0.2.1 webtyp.com/rpc@v0.2.21 webtyp.com/fetch@v0.1.30 && go mod tidy`

## APIs used

| symbol | exact signature | file |
|---|---|---|
| `router.Operation` | `struct{ Module, Entity string; Action model.Action }`; `String() string` | `router/op.go` (router plan) |
| `router.ParseOperation` | `func ParseOperation(s string) (Operation, error)` (inverse of `String`; refuses anything else) | `router/op.go` (router plan) |
| `router.Caller`, `router.OperationRegistry` | `Call(op Operation, …)`, `Dispatch(op Operation, …)`; `Operation(op Operation, h HandlerFunc) Route` | router plan |
| `crud.Mount` | `func Mount(reg router.OperationRegistry, public []Public, pub events.Publisher, m Module) error` | crud plan |
| `crud.LoopbackActingAs` | `func LoopbackActingAs(pub events.Publisher, userID string, mods ...Module) router.Caller` | crud plan |
| `rpc.Caller.CallKeyed` | `func (c *Caller) CallKeyed(op router.Operation, key string, body []byte, into model.Decodable, done func(err error))` | rpc plan |
| `outbox.Entry`, `outbox.Mutation` | `Op string`, `Payload []byte`; verdicts `outbox.Delivered`, `outbox.Rejected`, `outbox.Retry`; `outbox.Result{Verdict, Reason}` | `outbox/outbox.go` |

If a signature differs from this table, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 0 — the tests stop using `httpd` (decision D44: a sync library depends on no server)

`dbsync/tests/go.mod` drops `webtyp.com/server`; `go get webtyp.com/router@v0.4.21` there (it adds `routertest.Handler`/`Serve`, and makes the mock `Context` decode a `GET` from its query). In `tests/env_test.go` (`func (s *server) handler() http.Handler`, line ~211) replace the `httpd` server by a mock router:

| Before | After |
|---|---|
| `srv := httpd.New(httpd.Config{TLS: httpd.TLSConfig{PlainHTTP: true}, Authn: …, Authorize: allow})`; `r := srv.Router()` | `r := &mock.Router{}`; `r.Configure(mock.Config{Authn: <the same function>, Authorize: allow})` (the TLS option disappears) |
| `h, err := srv.Handler()` | `h := routertest.Handler(r)` |

Everything else in `handler()` stays (`r.Use(s.idem.Middleware())`, `rpc.Mount`, `syncSrv.MountAPI(r)`, the wrapper that counts `hits` and injects the 503). Imports: `webtyp.com/router/mock` and `webtyp.com/router/routertest` instead of `webtyp.com/server/httpd`. The same assertions pass. Commit: `test: dbsync tests run on router/mock, not httpd`.

## Test fixtures against `crud` v0.0.10 (a module fixture that implements `crud.Module` or `crud.Registry`)

`crud` v0.0.10, v0.0.9 and v0.0.10 changed the shapes below; every fixture and fake in this repo's tests follows them. The reference is `crud`'s own `tests/stored_entity_test.go` (`testModule`, `declareRules`): it is in the module cache at `webtyp.com/crud@v0.0.10/tests/` (`go list -m -f '{{.Dir}}' webtyp.com/crud`).

| Old | New (`crud` v0.0.10) |
|---|---|
| `crud.Caller` (hook and read arguments) | `crud.CurrentUser` (same field `UserID`) |
| `reg.Read(&Rec{}, nil, func(c crud.Caller, args crud.ReadArgs, f model.Model) (model.ModelSlice, error) {…})` | `reg.AddReadOnlyEntity(&Rec{}, nil, func(c crud.CurrentUser, args crud.ReadArgs, f model.Model) (model.ModelSlice, error) {…})` (same parameters, new name) |
| `reg.Table(crud.Table{…})` | `reg.AddStoredEntity(crud.StoredEntity{…})`; its hooks return `(crud.RuleAnswer, error)`: `return crud.ApplyAndPublish, nil` (an error: `return 0, err`); `Rules: model.Rules{…}` is required (`HaveNoBusinessRule` where a hook is nil, `AddMyBusinessRule` where it is set) |
| a `crud.Module` has `ModelName()` and `MountOperations(reg crud.Registry)` | it also has `Definition() model.ModuleDefinition`, listing **exactly** the entities it mounts, or `crud.Mount` refuses it (`ErrGeneratedOutOfDate`) |
| `crud.Op(&Rec{}, model.Read)` (a string) | `router.Operation{Module: "<ModelName()>", Entity: "<entity ModelName()>", Action: model.Read}` |
| `crud.Mount(reg, public, m)` | `crud.Mount(reg, public, nil, m)` (the new third argument is the `events.Publisher`; `nil` = nobody listens) |
| `crud.Loopback(m...)`, `crud.LoopbackActingAs(id, m...)` | `crud.Loopback(nil, m...)`, `crud.LoopbackActingAs(nil, id, m...)` |

**The `Store` of a fixture is `orm.GlobalStore(db, ids)` (or `orm.TenantStore`) from `orm` v0.12.31: it already is a `crud.Store`. Never write a `Store` wrapper, and never ask whether to: the fixtures follow this section.**

`Definition()` for the two usual fixtures:

```go
// a read-only entity mounted with AddReadOnlyEntity (a Wire model, no table)
func (m myModule) Definition() model.ModuleDefinition {
	return model.ModuleDefinition{Name: m.ModelName(), Entities: []model.Definition{{
		Name: (&Rec{}).ModelName(), Wire: true, Allow: model.Read, Rules: model.Rules{Read: model.AddMyBusinessRule}}}}
}

// a stored entity mounted with AddStoredEntity: copy what the entity says about itself
func (m myModule) Definition() model.ModuleDefinition {
	return model.ModuleDefinition{Name: m.ModelName(), Entities: []model.Definition{{
		Name: m.entity.Model.ModelName(), Allow: m.entity.Model.(model.Allower).Allow(), Rules: m.entity.Rules}}}
}
```

There is no `model.BaseData` and no `crud.Table`/`crud.Op` any more: if a name is not in `crud` v0.0.10, `go doc webtyp.com/crud` lists what is.

## Stage 1 — tests first (red)

Fixture modules in `tests/` (`notes` in `env_test.go:114`, `badMod` in `harvest_wiring_test.go:18`) implement `Definition()`: `notes` returns
`model.ModuleDefinition{Name: "notes", Entities: []model.Definition{{Name: "note", Allow: model.AllActions, Rules: <HaveNoBusinessRule for the four actions>}}}` (and the same shape for the other entity it mounts);
`badMod` returns the definition of the entity it wrongly mounts so the wiring error still comes from `crud`. Every `Table` they build carries `Rules:` equal to its definition and hooks of the new `(crud.RuleAnswer, error)` shape (see the `crud` plan, stage 1.1).
The fake server (`env_test.go:35`) now routes by method and path:

| Old | New |
|---|---|
| `pathCreateNote = "/api/notes/note_c"` (any method) | route `POST /api/notes/note` creates, `PATCH /api/notes/note` updates, `DELETE /api/notes/note` deletes; compare `r.Method` too |

Edit the existing tests:

| Where | Old | New |
|---|---|---|
| `env_test.go:353` `func (d *device) call(op string, …)` | `op string` | `op router.Operation`; `d.t.Fatalf("%s: no answer", op.String())` |
| `env_test.go:371` | `d.call("notes.note_c", args)` | `d.call(router.Operation{Module: "notes", Entity: "note", Action: model.Create}, args)` |
| `dbsync_test.go:207`, `:243`, `ui_test.go:69` | `rej[0].Op != "notes.note_c"` / `"notes.note_u"`; tray text `"notes.note_c"` | `"notes.note.create"` / `"notes.note.update"` |
| `dbsync_test.go:233`, `:363` | `d.call("notes.note_u", …)`, `d.call("notes.note_r", …)` | the same with `router.Operation{Module:"notes", Entity:"note", Action: model.Update}` / `model.Read` |
| `dbsync_test.go:357` | `for _, op := range []string{"notes.nope", "notes.note_d"}` | `[]router.Operation{{Module:"notes", Entity:"nope", Action: model.Read}, {Module:"notes", Entity:"note", Action: model.Delete}}` — expectation unchanged (an operation the modules did not offer offline is refused) |

New tests:

| Test | setup → call → expected |
|---|---|
| `TestQueue_StoresTheOperationAsText` | offline `Create` of a note → the queued mutation's `Operation` is `"notes.note.create"` |
| `TestQueue_PushUsesTheStoredOperation` | after reconnecting, the fake server receives `POST /api/notes/note` with the stored payload and the mutation id as idempotency key |
| `TestQueue_OldFormatGoesToTheTray` | insert a mutation row with `Op: "notes.note_c"` directly through `orm`, run a sync → it ends `Rejected` with a reason containing `notes.note_c`, and nothing is sent to the server |
| `TestOffline_ReadNeverQueues` | `Call(router.Operation{…Read})` → answered from the local database, the queue stays empty |

Run `gotest`: it does not compile. Commit: `test: typed offline operations (red)`.

## Stage 2 — the code

`client.go` (`harvester`, `opInfo`, `Client.op`):

```go
// opInfo is what dbsync knows of an offline operation: its identity and declared action.
type opInfo struct {
	op       router.Operation
	action   model.Action
	declared bool // Requires was called
	// mfa: the route demands a second factor. An offline replay cannot prove one, so such an operation never runs offline.
	mfa bool
}

// harvester is a router.OperationRegistry that only records operations and actions.
type harvester struct {
	ops []opInfo
}

func harvest(mods []crud.Module) ([]opInfo, error) {
	h := &harvester{}
	for _, m := range mods {
		if m == nil {
			continue
		}
		if err := crud.Mount(h, nil, nil, m); err != nil {
			return nil, err
		}
	}
	return h.ops, nil
}

func (h *harvester) Operation(op router.Operation, _ router.HandlerFunc) router.Route {
	h.ops = append(h.ops, opInfo{op: op})
	return harvestRoute{h: h, i: len(h.ops) - 1}
}

func (c *Client) op(op router.Operation) (opInfo, bool) {
	for _, o := range c.ops {
		if o.op == op {
			return o, o.declared && !o.mfa
		}
	}
	return opInfo{}, false
}
```

(`harvestRoute` methods unchanged.) Line 176: `c.local = crud.LoopbackActingAs(nil, cfg.UserID, mods...)` — the offline modules announce nothing outside the device (`nil`: nobody listens locally).

`caller.go`: `offlineCaller.Call(op router.Operation, …)`, `Dispatch(op router.Operation, …)`, `Client.call/runLocal/write(op router.Operation, …)`; the queue row:

```go
	id, err := c.box.Enqueue(outbox.Entry{Op: op.String(), Payload: payload})
```

`errors.go:43`: `func errNotOfflineOp(op router.Operation) error { return syncError(fmt.Sprintf(msgNotOfflineOp, op.String())) }` (same for `errScopeOperator` if it takes an operation).

`sync.go:104` the sender parses the stored text and sends the typed operation:

```go
func (s sender) Send(m *outbox.Mutation) outbox.Result {
	op, err := router.ParseOperation(m.Operation)
	if err != nil {
		// A row queued before operations were typed: refuse it loudly, the person sees it in the tray.
		return outbox.Result{Verdict: outbox.Rejected, Reason: err.Error()}
	}
	ch := make(chan error, 1)
	s.c.cfg.Remote.CallKeyed(op, m.Id, m.Payload, nil, func(err error) { ch <- err })
	// …the rest of Send is unchanged…
```

`ui/tray.go:102` keeps `Span().Text(m.Operation)`: the stored text is already readable (`notes.note.create`). `gotest` passes. Commit: `refactor!: typed offline operations`.

## Stage 3 — documentation

- `README.md` (≤ 150 lines, four ordered sections): replace operation strings in the guide with `router.Operation{…}`; add one sentence: the queue stores `Operation.String()`.
- `docs/ARCHITECTURE.md` (four sections) and any `docs/diagrams/*.md`: operation names in diagrams become `notes.note.create`; state the old-format rule.
- `docs/TESTS.md`: one row per new test. The old `docs/PLAN.md` ("offline modules are crud.Module…") was executed (commit `2c51afc`); this file replaces it.
- Do not link `docs/PLAN.md` from permanent docs.

Commit: `docs: typed offline operations`.

## Delivery check

1. `gotest` in the module and in `tests/` (a PR that does not compile, or whose tests could not run, is a failed delivery).
2. `grep -rn "_c\"\|_r\"\|_u\"\|_d\"\|h\.module" --include=*.go --include=*.md .` → empty.
3. `git diff --stat main` lists only `client.go`, `caller.go`, `errors.go`, `sync.go`, `go.mod`, `go.sum`, `tests/*`, `README.md`, `docs/*`.

| Stage | Files | Commit |
|---|---|---|
| 1 | `tests/*` | `test: … (red)` |
| 2 | `client.go`, `caller.go`, `errors.go`, `sync.go` | `refactor!: …` |
| 3 | `README.md`, `docs/*` | `docs: …` |
