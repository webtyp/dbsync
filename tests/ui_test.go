//go:build !wasm

package tests

import (
	"strings"
	"testing"

	"webtyp.com/dbsync/ui"
	"webtyp.com/dom"
)

func TestIndicatorRenders(t *testing.T) {
	srv := newServer(t)
	d := open(t, srv)

	comp := ui.Indicator(d.c)
	vr, ok := comp.(dom.ViewRenderer)
	if !ok {
		t.Fatal("Indicator must implement dom.ViewRenderer")
	}

	str := vr.Render().String()
	if !strings.Contains(str, "dbsyncindicator") {
		t.Errorf("indicator rendering missing 'dbsyncindicator' class: %s", str)
	}
	if !strings.Contains(str, "Online") {
		t.Errorf("indicator rendering missing 'Online' text: %s", str)
	}
}

func TestTrayConflicts(t *testing.T) {
	srv := newServer(t)
	d := open(t, srv)

	// "taken" results in HTTP 409 from newServer
	d.create("taken")

	eventually(t, d, "conflict rejected", func() bool {
		return d.c.Status().Rejected == 1
	})

	comp := ui.Tray(d.c)
	vr, ok := comp.(dom.ViewRenderer)
	if !ok {
		t.Fatal("Tray must implement dom.ViewRenderer")
	}

	str := vr.Render().String()
	if !strings.Contains(str, "taken") {
		t.Errorf("tray rendering missing 'taken': %s", str)
	}
	if !strings.Contains(str, "notes.create_note") {
		t.Errorf("tray rendering missing 'notes.create_note': %s", str)
	}
	if !strings.Contains(str, "Dismiss") {
		t.Errorf("tray rendering missing 'Dismiss': %s", str)
	}

	muts, _ := d.c.Rejected()
	if len(muts) > 0 {
		err := d.c.Dismiss(muts[0].Id)
		if err != nil {
			t.Fatalf("dismiss failed: %v", err)
		}
	}

	freshComp := ui.Tray(d.c)
	freshVr, ok := freshComp.(dom.ViewRenderer)
	if !ok {
		t.Fatal("Tray must implement dom.ViewRenderer")
	}
	freshTray := freshVr.Render().String()
	if !strings.Contains(freshTray, "No conflicts") {
		t.Errorf("expected fresh tray to say 'No conflicts', got: %s", freshTray)
	}
}

func TestRenderCSS(t *testing.T) {
	sheet := ui.RenderCSS()
	if sheet == nil {
		t.Fatal("RenderCSS() returned nil")
	}

	str := sheet.String()
	if len(str) == 0 {
		t.Error("RenderCSS() returned empty string representation")
	}
}
