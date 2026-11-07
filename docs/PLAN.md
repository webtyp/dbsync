---
PLAN: "refactor!: offline modules are crud.Module; harvest through crud.Mount; crud.LoopbackActingAs"
EXECUTOR: antigravity
REVIEWER: none
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit after every stage and stop. Never ask whether to
> proceed. This plan runs **locally**: do not tag, push or publish anything; the owner reviews first.
> Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own work.

# Plan: `dbsync` over `crud.Module`

Context: master `webtyp/docs/ARCHITECTURE_MASTER_PLAN.md` D7, D14. Runs after the router and crud plans.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
```
`go.mod`: `replace` for `webtyp.com/model`, `webtyp.com/router`, `webtyp.com/crud`, `webtyp.com/rpc` to `../model`, `../router`, `../crud`, `../rpc`; `go get webtyp.com/crud`; `go mod tidy`.

## APIs used

| Symbol | Signature |
|---|---|
| `crud.Module` | `interface{ model.ModuleNaming; MountOperations(reg crud.Registry) }` |
| `crud.Mount` | `func Mount(reg router.OperationRegistry, public []model.ModuleNaming, m crud.Module) error` |
| `crud.LoopbackActingAs` | `func LoopbackActingAs(userID string, mods ...crud.Module) router.Caller` |

## Stage 1 — `client.go`

- `ClientConfig.Modules`: `func(db *orm.DB, ids model.IDGenerator) ([]crud.Module, error)`.
- `harvest(mods []crud.Module) ([]opInfo, error)`: replace `m.MountOperations(h)` with
  `if err := crud.Mount(h, nil, m); err != nil { return nil, err }`; the caller at line 173 becomes
  `if c.ops, err = harvest(mods); err != nil { return nil, err }`.
- Line 174: `c.local = crud.LoopbackActingAs(cfg.UserID, mods...)` (the tenant is the modules' own configuration now;
  `ClientConfig.TenantID` stays for dbsync's own requests — check every other use with `grep -n "TenantID" *.go` and keep them).
- Delete the `webtyp.com/router/loopback` import.

## Stage 2 — tests

`tests/env_test.go` and every fake module in `tests/` become `crud.Module`; operation names in expectations follow
`crud.Op` (`"<module>.<entity>_c"`…). New: `TestHarvest_CrudWiringErrorStopsClient` — a module with `reg.Update(nil, nil)` →
the client constructor returns `crud.ErrNilFunction`. Rows in `docs/TESTS.md`.

## Stage 3 — documentation

`README.md` / `docs/*`: `ClientConfig.Modules` returns `[]crud.Module`; remove `OperationModule` and `router/loopback`.

## Delivery check

`gotest` passes; `grep -rn "OperationModule\|router/loopback" --include=*.go .` → empty.

| Stage | Files |
|---|---|
| 1 | `client.go` |
| 2 | `tests/*.go`, `docs/TESTS.md` |
| 3 | `README.md`, `docs/*` |
