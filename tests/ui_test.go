//go:build !wasm

package tests

import (
	"strings"
	"testing"

	"webtyp.com/dbsync/ui"
	"webtyp.com/dom"
	"webtyp.com/widget"
)

// openState is how the indicator marks "offline" in its markup.
var openState = widget.Open.Key() + "='" + widget.Open.Value() + "'"

func render(t *testing.T, c dom.Component) string {
	t.Helper()
	v, ok := c.(dom.ViewRenderer)
	if !ok {
		t.Fatalf("%T does not render", c)
	}
	return v.Render().String()
}

func mustContain(t *testing.T, html string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(html, w) {
			t.Errorf("missing %q in %s", w, html)
		}
	}
}

func TestIndicatorShowsOnline(t *testing.T) {
	d := open(t, newServer(t))

	html := render(t, ui.Indicator(d.c))
	mustContain(t, html, "dbsyncindicator", "Online")
	if strings.Contains(html, openState) {
		t.Errorf("online indicator is marked offline: %s", html)
	}
}

// The same indicator follows the connection: it is built online and renders
// "Offline" once the server is gone.
func TestIndicatorFollowsTheConnection(t *testing.T) {
	s := newServer(t)
	d := open(t, s)
	indicator := ui.Indicator(d.c)

	s.stop()
	d.create("offline")
	eventually(t, d, "offline status", func() bool { return !d.c.Status().Online })

	html := render(t, indicator)
	mustContain(t, html, "Offline", openState)
}

// The tray is built before the server refuses the write: it fills itself from
// the status, and empties itself after the Dismiss.
func TestTrayListsRefusedWritesUntilDismissed(t *testing.T) {
	d := open(t, newServer(t))
	tray := ui.Tray(d.c)
	mustContain(t, render(t, tray), "No conflicts")

	d.create("taken")
	eventually(t, d, "write refused", func() bool { return d.c.Status().Rejected == 1 })
	mustContain(t, render(t, tray), "taken", "notes.create_note", "Dismiss")

	refused, err := d.c.Rejected()
	if err != nil || len(refused) != 1 {
		t.Fatalf("Rejected() = %d writes, %v; want 1", len(refused), err)
	}
	if err := d.c.Dismiss(refused[0].Id); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	html := render(t, tray)
	mustContain(t, html, "No conflicts")
	if strings.Contains(html, "taken") {
		t.Errorf("dismissed write still listed: %s", html)
	}
	mustContain(t, render(t, ui.Tray(d.c)), "No conflicts")
}

func TestRenderCSSStylesBothWidgets(t *testing.T) {
	sheet := ui.RenderCSS()
	if sheet == nil {
		t.Fatal("RenderCSS() = nil")
	}
	mustContain(t, sheet.String(), ".dbsyncindicator", ".dbsynctray")
}
