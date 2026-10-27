---
PLAN: "feat: changes reach every open screen — SSE «head moved» from the change log, pulled rows go to view.Records"
TAG: v0.3.11
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 11127215870619801806
---

> Read AGENTS.md at the repo root before any change.

<!-- Dispatch only after webtyp.com/view v0.6.47 is published (view.Records). -->

# Plan — `webtyp.com/dbsync`: what another person saves shows on my screen

## Why

When reception renames a service, the doctor with the services list open must see it without reloading. The
server already writes every change to the change log (`changelog.Log`), and `/pull` already sends each user only
the rows they may read (`Rule.Resource` + `Rule.Scope`). Two pieces are missing:

1. **A nudge.** Today a device pulls on a timer (`sync.go:36-47`). The server will announce over SSE, on one
   channel, only «the head moved to N» (no data: what each user may see is decided by `/pull`, never by the
   nudge). The client pulls as soon as it hears it.
2. **The screens.** Pulled rows go to the local database only. From now on they also go to the page's shared
   records (`view.Records`, `webtyp.com/view` v0.6.47), so every list and select showing them changes at once.

## Tools

```
go install webtyp.com/devflow/cmd/gotest@latest
go install webtyp.com/lang/cmd/langc@latest
go get webtyp.com/view@v0.6.47
go get webtyp.com/sse@v0.2.30
```

## Ecosystem search

- `changelog.Log.OnChange(fn func(head int64))` (`webtyp.com/changelog`, `log.go:47`) → **reuse**: the server's hook.
- `webtyp.com/sse` v0.2.30: `sse.New(cfg).Server(&sse.ServerConfig{Path, Access, Resource, ChannelProvider, …})`,
  `(*SSEServer).Publish(data []byte, channel string)`, `MountAPI(r)`; client `sse.New(cfg).Client(&sse.ClientConfig{
  Endpoint, RetryInterval, MaxRetryDelay, MaxReconnectAttempts})`, `OnMessage(func(*sse.SSEMessage))`, `Connect()`
  (`sse/docs/USAGE.md`) → **reuse**.
- `view.Records.Put(rows ...model.Model)`, `Remove(modelName string, ids ...string)` (view v0.6.47) → **reuse**.
- Precedent of «poke + pull»: Replicache.

## Design gate

1. **Prior art.** Replicache (server «poke» without data, client pulls), ElectricSQL (change-log stream), Firebase
   (listeners on the local cache). All separate «something changed» from «what you may read».
2. **Novice-name test.** `ClientConfig.Records` — «the records of the page».
3. **Complexity ledger.** Concepts +1 (`ClientConfig.Records`); the timer stays only as a fallback; ways a change
   reaches a screen: 1.
4. **Where it belongs.** Moving changes between server and device is `dbsync`'s job; the page copy is `view`'s.
5. **What it deletes.** Nothing; the periodic pull becomes the fallback when the SSE channel is down.

## APIs used

| Symbol | Exact signature | Tag | File |
|---|---|---|---|
| `(*changelog.Log).OnChange` | `func (l *Log) OnChange(fn func(head int64))` | changelog (version in `go.mod`) | `log.go` |
| `sse.ServerConfig` | fields `Path string`, `Access model.Access`, `Resource model.Resource` (only with `AccessGuarded`), `ChannelProvider` | sse v0.2.30 | `server_config.go` |
| `(*sse.SSEServer).Publish` | `func (s *SSEServer) Publish(data []byte, channel string)` | sse v0.2.30 | `server.go` |
| `sse.ChannelProvider` | `ResolveChannels(ctx router.Context) ([]string, error)` | sse v0.2.30 | `docs/USAGE.md` |
| `(*view.Records).Put` / `Remove` | `Put(rows ...model.Model)`; `Remove(modelName string, ids ...string)` | view v0.6.47 | `records.go` |

Read each signature in the module cache before using it. If one differs from this table, stop and name it in the
PR; never adapt it by guessing.

## Stage 1 — server: announce the head (`server.go`, `wire.go`)

1. `wire.go`: `routeEvents = "/events"` and `channelHead = "head"`.
2. `Server` gets a field `events *sse.SSEServer`. In `NewServer`, after validation:

```go
	s.events = sse.New(&sse.Config{}).Server(&sse.ServerConfig{
		Path:            DefaultPrefix + routeEvents,
		Access:          model.AccessAuthenticated,
		ChannelProvider: headChannel{},
	})
	cfg.Log.OnChange(func(head int64) {
		s.events.Publish([]byte(fmt.Convert(head).String()), channelHead)
	})
```

   with `type headChannel struct{}` whose `ResolveChannels` returns `[]string{channelHead}, nil`. If
   `model.AccessAuthenticated` has another name in the `model` version of `go.mod`, use the constant that means
   «any signed-in user» and name it in the PR.
3. `MountAPI`: after the `r.Mount(DefaultPrefix, …)` block, `s.events.MountAPI(r)`.

The payload is the head number only: what each user may see is still decided by `/pull`.

## Stage 2 — client: pull when nudged; pulled rows to the page (`client.go`, `sync.go`)

1. `ClientConfig` gains:

```go
	// Records is the page's shared copy (view.NewRecords()); every pulled row is put
	// there too, so the screens showing it change at once. Required.
	Records *view.Records
```

   `Open`: `case cfg.Records == nil: return nil, errRecordsRequired` with
   `errRecordsRequired = fmt.Err("dbsync: ClientConfig.Records is required")` in `errors.go`.
2. In `Open`, after the client starts its loop, connect the nudge:

```go
	events := sse.New(&sse.Config{}).Client(&sse.ClientConfig{
		Endpoint:             cfg.Origin + DefaultPrefix + routeEvents,
		RetryInterval:        1000,
		MaxRetryDelay:        30000,
		MaxReconnectAttempts: 0, // keep trying; check the sse docs for the value that means «forever» and use it
	})
	events.OnMessage(func(*sse.SSEMessage) { c.wake() })
	events.Connect()
```

3. `applyPage` (`sync.go:199`): collect what changed and, **after** `c.saveState()` succeeds and the lock is
   released, tell the page:

```go
	var put []model.Model
	var removed []removedRow // type removedRow struct{ table, id string }
	// inside the loop: on opUpsert append ch.row to put; on opDelete append {ch.table, ch.rowID}
	…
	c.cfg.Records.Put(put...)
	for _, r := range removed {
		c.cfg.Records.Remove(r.table, r.id)
	}
```

   (`ch.table` is the model name the server sends; check it equals `ModelName()` of the factory's model and say so
   in the PR if not.)

## Stage 3 — tests

| Test | Setup | Expected |
|---|---|---|
| `TestServer_PublishesHeadOnChange` | server over a `changelog.Log` with one tracked table; subscribe a fake SSE client (or read the `SSEServer` history) | after a write through the log, a message `"<head>"` on channel `head` |
| `TestServer_EventsNeedSession` | GET `/sync/events` without a user | 401/403 (whatever `AccessAuthenticated` answers) |
| `TestClient_OpenRequiresRecords` | `Open` with `Records: nil` | `errRecordsRequired` |
| `TestClient_PullPutsRowsInRecords` | existing pull test harness; `Records: view.NewRecords()` | after a pull with one upsert and one delete, `Records.Get(table, id)` has the upserted row and not the deleted one |

## Stage 4 — docs and translations

- `README.md` «Wiring»: `Records: records` in `ClientConfig`; the server announces the head over SSE at
  `/sync/events`; the timer is the fallback.
- `docs/ARCHITECTURE.md`: a «Nudge» paragraph: payload = head only, `/pull` keeps deciding who sees what.
- `langc sync .`; fill every empty value; zero untranslated keys, zero reuse warnings.

## Delivery

- `gotest` green. A PR that does not compile, or whose tests could not run, is a failed delivery.
  Environment: `GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`.
- Never pause to ask permission or report progress: when the check passes, commit, push and open the PR. Stuck 30
  minutes → open the PR listing what fails. Commit after every stage; never `git reset`, `git restore`,
  `git checkout -- <file>` or `git stash` on your own work.

| Stage | File | Action |
|---|---|---|
| 1 | `server.go`, `wire.go`, `go.mod`, `go.sum` | SSE «head moved» |
| 2 | `client.go`, `sync.go`, `errors.go` | `Records` required; nudge → pull; pulled rows to the page |
| 3 | `tests/*` | behaviour |
| 4 | `README.md`, `docs/ARCHITECTURE.md`, `lang.json` | document, translate |
