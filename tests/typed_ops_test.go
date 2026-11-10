//go:build !wasm

package tests

import (
	"strings"
	"testing"

	"webtyp.com/crud"
	"webtyp.com/model"
	"webtyp.com/outbox"
)

func queued(t *testing.T, d *device) outbox.MutationList {
	t.Helper()
	list, err := outbox.ReadAllMutation(d.db.Query(&outbox.Mutation{}))
	if err != nil {
		t.Fatalf("read the queue: %v", err)
	}
	return list
}

func TestQueue_StoresTheOperationAsText(t *testing.T) {
	s := newServer(t)
	d := open(t, s)
	s.stop()
	d.create("offline")
	list := queued(t, d)
	if len(list) != 1 || list[0].Op != "notes.note.create" {
		t.Fatalf("the queued mutation must store Operation.String() = notes.note.create, got %+v", list)
	}
}

func TestQueue_PushUsesTheStoredOperation(t *testing.T) {
	s := newServer(t)
	d := open(t, s)
	s.stop()
	n := d.create("later")
	s.start()
	eventually(t, d, "delivered by the stored operation", func() bool { _, ok := s.note(n.Id); return ok })
	if s.count(pathCreateNote) < 1 {
		t.Fatalf("the server must receive POST %s, hits: %v", pathCreateNote, s.hits)
	}
}

func TestQueue_OldFormatGoesToTheTray(t *testing.T) {
	s := newServer(t)
	d := open(t, s)
	s.stop()
	old := &outbox.Mutation{Id: "old-1", Seq: 99, Op: "notes.note_c", Payload: []byte("{}"), State: "pending", CreatedAt: now()}
	if err := d.db.Create(old); err != nil {
		t.Fatalf("insert the old-format row: %v", err)
	}
	s.start()
	before := s.count(pathCreateNote)
	eventually(t, d, "the old row is rejected", func() bool { return d.c.Status().Rejected >= 1 })
	rej, err := d.c.Rejected()
	if err != nil || len(rej) == 0 || !strings.Contains(rej[0].Reason, "notes.note_c") {
		t.Fatalf("the tray must say which operation it could not read, got %+v, %v", rej, err)
	}
	if s.count(pathCreateNote) != before {
		t.Fatal("nothing may be sent to the server for an unreadable operation")
	}
}

func TestOffline_ReadNeverQueues(t *testing.T) {
	s := newServer(t)
	d := open(t, s)
	n := d.create("read me")
	eventually(t, d, "nothing pending", func() bool { return d.c.Status().Pending == 0 })
	before := len(queued(t, d))
	if err := d.call(opNote(model.Read), &crud.ReadArgs{ID: n.Id}); err != nil {
		t.Fatalf("local read: %v", err)
	}
	if after := len(queued(t, d)); after != before {
		t.Fatalf("a read must not queue anything: %d rows before, %d after", before, after)
	}
}
