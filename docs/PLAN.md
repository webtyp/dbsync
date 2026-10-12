---
PLAN: "feat(ui): dbsync/ui — Indicator (online, pending, conflicts) and Tray (the conflict tray with Dismiss)"
EXECUTOR: jules
REVIEWER: none
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.
>
> **Prerequisites (published tags):** `webtyp.com/dbsync` v0.2.0 (this repo, `main`),
> `webtyp.com/components` **v0.8.26**, `webtyp.com/lang`, `webtyp.com/dom`, `webtyp.com/html`,
> `webtyp.com/time` v0.5.7. If any is missing, stop and report it.

# Plan — `dbsync/ui`: what a person sees of the sync engine

## 0. Context (read first)

`webtyp.com/dbsync` (this repo) is the offline-first sync engine: every browser runs the app's
modules over a local database, queues its writes and pushes them to the server when it can
(`docs/ARCHITECTURE.md`). Two things must be visible to the person using the app, and every app
that uses `dbsync` needs the same two:

1. **Where things stand:** online or offline, how many writes are waiting to be sent, how many the
   server refused.
2. **The conflict tray:** each write the server refused (for example a booking on an hour another
   desk took first), with the server's reason, and a way to dismiss it once the person dealt with it.

The engine already exposes all of it (`client.go`):

```go
func (c *Client) Status() Status                // Status{Online bool; Pending, Rejected int}
func (c *Client) OnStatus(fn func(Status))      // called from the sync goroutine on every change
func (c *Client) Rejected() ([]outbox.Mutation, error) // outbox.Mutation{Id, Op, Reason string; CreatedAt int64 (unix ns); ...}
func (c *Client) Dismiss(id string) error
```

The app places the indicator in the header slot `platformd.Platform.Status func() Component`
(`webtyp.com/layout` v0.3.44). This package only builds the component; it never imports `layout`.

House pattern: a library's screens live in its own `ui/` sub-package (as `veltylabs/*/ui`), so the
engine (`dbsync`) stays free of UI code and the app imports `webtyp.com/dbsync/ui` only in the
browser.

## Design gate

### 1. Prior art
- **Google Docs / Notion offline badge:** a small header chip ("Offline", "Saving…") that opens
  details on click. Our `Indicator`.
- **Outlook / Thunderbird Outbox + "Send/Receive errors" dialog:** queued items and the refused ones
  with the server's reason. Our `Tray`.
- **Replicache / Linear:** the app shows pending mutations and lets the user discard a rejected one;
  the engine never discards silently. Our `Dismiss`.

### 2. Novice-name test
`ui.Indicator(client)` → "the sync indicator". `ui.Tray(client)` → "the conflict tray".

### 3. Complexity ledger
```
Concepts the developer must learn   +2 (Indicator, Tray)
Files they must touch to do X        +1 line in the app's header slot
Lines at the call site               1
Ways to do the same thing            0 (nothing displayed sync state before)
```

### 4. Where it belongs
`webtyp.com/dbsync/ui`: the UI of this library, next to it (house pattern). It composes
`webtyp.com/components` (`modaldialog`, `actionbutton`, `countbadge`) and never re-creates them.

### 5. What this deletes
Nothing (new capability).

## 1. API (`ui/`, package `ui`)

```go
// Indicator shows this device's sync state: "Online" or "Offline", the writes waiting to be sent,
// and the writes the server refused. Clicking it opens the Tray in a modal dialog.
func Indicator(c *dbsync.Client) Component

// Tray lists the writes the server refused, oldest first: when, the operation, and the server's
// reason, each with a Dismiss button (c.Dismiss). Empty → "No conflicts".
func Tray(c *dbsync.Client) Component
```

`Component` is `webtyp.com/dom`'s. Nothing else is exported.

### 1.1 Indicator (`ui/indicator.go`)
- One struct (unexported) embedding `dom.Element` by value, with signals: `online *SignalBool`,
  `pending *SignalString`, `rejected *SignalString`, `hasPending *SignalBool`,
  `hasRejected *SignalBool` (`webtyp.com/dom` signals).
- Initial values from `c.Status()`; `c.OnStatus(func(s dbsync.Status) { … })` updates the signals.
- Render: a `<button>` (root class) containing a text span bound to the online signal ("Online" /
  "Offline"), a `countbadge.CountBadge{Count: pending, Visible: hasPending}` and a second
  `countbadge.CountBadge{Count: rejected, Visible: hasRejected}`. Clicking opens a
  `modaldialog.ModalDialog{Title: "Conflicts", Content: Tray(c)}` (call its `Open()`).
- `widget.Name("dbsyncindicator")`, `WidgetKind() widget.Kind` = `widget.Region`. Classes only
  through `Name.Root()` / `Name.Class(part)` (never a class literal). Parts: `PartLabel`
  (`"label"`).
- States: bind `widget.Open` (or the state the style recipes use for "on") to `online` so the
  stylesheet can tint the label: follow skill **widget-styling**; if no recipe expresses
  "online/offline", stop and report it (do not hand-compose CSS).

### 1.2 Tray (`ui/tray.go`)
- Holds a `*SignalNodes` of rows; `refresh()` rebuilds them from `c.Rejected()`. Called on render,
  after each Dismiss, and from `c.OnStatus`.
- Each row: `time.FormatDateTime(m.CreatedAt)` (`webtyp.com/time`), `m.Op`, `m.Reason`, and an
  `actionbutton.ActionButton{Text: "Dismiss", Variant: "secondary", OnClick: …}` that calls
  `c.Dismiss(m.Id)` and then `refresh()`. A Dismiss error shows its text in the row.
- `c.Rejected()` error → one row with the error text.
- No rejected writes → a single line "No conflicts".
- `widget.Name("dbsynctray")`, Kind `widget.Region`, parts `PartRow` (`"row"`), `PartReason`
  (`"reason"`), `PartEmpty` (`"empty"`).

### 1.3 Texts and translations
Every visible text is a `lang.Text` written in **English**: `"Online"`, `"Offline"`,
`"Conflicts"`, `"Dismiss"`, `"No conflicts"`. Add them with their Spanish translation to a
`lang.json` at the module root, same format as
<https://github.com/webtyp/components/blob/main/lang.json>:
`"Online": ["En línea"]`, `"Offline": ["Sin conexión"]`, `"Conflicts": ["Conflictos"]`,
`"Dismiss": ["Descartar"]`, `"No conflicts": ["Sin conflictos"]` (skill **translations**).

### 1.4 Stylesheets
`ui/css.go` (`//go:build !wasm`) declares `RenderCSS() *css.Stylesheet` for each of the two
widgets with `webtyp.com/widget/style` recipes only (skill **widget-styling**). The label tint for
offline uses a status token (warning), never a literal colour.

## 2. Stages

| Stage | Files | Content |
|---|---|---|
| 1 | `go.mod`, `go.sum` | `go get` the deps of the header |
| 2 | `ui/indicator.go`, `ui/tray.go` | §1.1, §1.2 |
| 3 | `ui/css.go`, `lang.json` | §1.3, §1.4 |
| 4 | `tests/ui_test.go` (existing nested `tests/` module) | §3 |
| 5 | `README.md`, `docs/ARCHITECTURE.md` | "Showing it to the person": the one-line app wiring `Status: func() Component { return ui.Indicator(client) }` and a sentence on the tray |

## 3. Tests (`tests/ui_test.go`, `//go:build !wasm`, reuse `env_test.go`'s `newServer`/`open`)

1. `ui.Indicator(d.c).Render().String()` contains the root class `dbsyncindicator` and `Online`
   right after `open` (server up).
2. After a rejected write (create a note with text `"taken"`, wait for `Status().Rejected == 1`
   with the existing `eventually`): `ui.Tray(d.c).Render().String()` contains `taken`,
   `notes.create_note` and `Dismiss`; after `d.c.Dismiss(id)` a fresh `ui.Tray(d.c)` renders
   `No conflicts`.
3. Each widget's `RenderCSS()` is non-nil and does not panic (it validates the composition).
4. Every existing test keeps passing.

## 4. Code rules (non-negotiable)
- `ui/` compiles to TinyGo WASM: `webtyp.com/fmt`, `webtyp.com/time`; no `errors`, `strconv`,
  `strings`, stdlib `time`, `map`. `css.go` is `//go:build !wasm`.
- Dot-import `webtyp.com/dom` and `webtyp.com/html` (house style). Classes only through
  `widget.Name`/`widget.Part`. Never compose an element id (skill **dom-elements**).
- The engine package (`dbsync`, the module root) must **not** import `ui` or any UI package.
- No exported symbol beyond §1. Tests in `tests/`; never export for a test.

## 5. Acceptance criteria
- `gotest ./...` green (stdlib and wasm).
- `go list -deps . | grep -E "webtyp.com/(dom|html|components)"` (run at the module root) → empty:
  the engine stays free of UI.
- `grep -rn "Class(\"" --include=*.go ui/` → empty.
- **Before opening the PR:** `git diff --stat main` lists every file of the stages table, and no file
  is deleted that this plan does not order deleted.
