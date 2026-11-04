---
PLAN: "refactor: sync tray shows when the change was made on the user's own clock (time.MachineZone)"
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 6678894684707184598
PR: https://github.com/webtyp/dbsync/pull/5
REVIEWED_COMMIT: 47604a06f2c2ca088a1da5e13caa607df31dbc14
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# Plan — `ui/tray.go` without the removed `time.FormatDateTime`

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
go get webtyp.com/time@v0.6.3
```

Environment: `GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`, `go mod tidy` after `go get`.

## Why

`webtyp.com/time` v0.6.3 removed `FormatDateTime` and every implicit local time. The sync tray lists the changes
**this user** made while offline that the server rejected; "when I did it" is the user's own clock, so the zone is
`time.MachineZone()` (the browser's), stated explicitly.

## APIs used (time v0.6.3, `zone.go`)

| Symbol | Signature |
|---|---|
| `time.MachineZone` | `func MachineZone() Zone` |
| `(Zone) DateTime` | `func (z Zone) DateTime(u Unix) string` — `"YYYY-MM-DD HH:MM"` |

If a signature in the published tag differs from this table, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 1

`ui/tray.go:100`: `time.FormatDateTime(m.CreatedAt.Unix())` → `time.MachineZone().DateTime(m.CreatedAt.Unix())`, with a
one-line comment: "the user's own clock: these are changes this user made on this device".

## Delivery check

1. `gotest` passes (a PR that does not compile, or whose tests could not run, is a failed delivery).
2. `grep -rn "FormatDateTime\|FormatDate(\|FormatTime(" --include=*.go .` → empty.

Commit, push, open the PR. Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own work.
