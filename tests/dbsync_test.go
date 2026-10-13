//go:build !wasm

package tests

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"webtyp.com/dbsync"
	"webtyp.com/storage"
	"webtyp.com/storage/mem"
)

// 1
func TestBootstrapCopiesOnlyTheUsersRows(t *testing.T) {
	s := newServer(t)
	s.put(&Note{Id: "n1", Owner: userID, Text: "mine"})
	s.put(&Note{Id: "n2", Owner: otherUser, Text: "theirs"})

	d := open(t, s)
	if n, ok := d.note("n1"); !ok || n.Text != "mine" {
		t.Fatalf("own note not copied: %+v %v", n, ok)
	}
	if _, ok := d.note("n2"); ok {
		t.Fatal("another user's note was copied")
	}
	if st := d.state(); st.Replica != 1 || st.Epoch == "" || st.Cursor != s.log.Head() {
		t.Fatalf("state after bootstrap = %+v, head %d", st, s.log.Head())
	}
}

// 2
func TestOnlineWriteIsLocalAtOnceThenOnTheServer(t *testing.T) {
	s := newServer(t)
	d := open(t, s)

	n := d.create("hello")
	if _, ok := d.note(n.Id); !ok {
		t.Fatal("the write is not in the local db right after done")
	}
	eventually(t, d, "note on the server", func() bool { _, ok := s.note(n.Id); return ok })
	eventually(t, d, "nothing pending", func() bool { return d.c.Status().Pending == 0 })
}

// 3
func TestOfflineWriteIsDeliveredWhenTheServerComesBack(t *testing.T) {
	s := newServer(t)
	d := open(t, s)
	s.stop()

	n := d.create("offline")
	if _, ok := d.note(n.Id); !ok {
		t.Fatal("offline write is not in the local db")
	}
	eventually(t, d, "offline status", func() bool {
		st := d.c.Status()
		return !st.Online && st.Pending == 1
	})

	s.start()
	eventually(t, d, "delivered after restart", func() bool { _, ok := s.note(n.Id); return ok })
	eventually(t, d, "online, nothing pending", func() bool {
		st := d.c.Status()
		return st.Online && st.Pending == 0
	})
	if got := s.calls.Load(); got != 1 {
		t.Fatalf("create_note ran %d times, want 1", got)
	}
}

// 4
func TestLostAnswerIsAnsweredOnce(t *testing.T) {
	s := newServer(t)
	d := open(t, s)
	n := d.create("once")
	eventually(t, d, "delivered", func() bool { _, ok := s.note(n.Id); return ok && d.c.Status().Pending == 0 })

	s.restart() // new epoch, same idempotency store: the client resends what it delivered recently
	eventually(t, d, "resent", func() bool { return s.count(pathCreateNote) >= 2 })
	eventually(t, d, "nothing pending", func() bool { return d.c.Status().Pending == 0 })
	if got := s.calls.Load(); got != 1 {
		t.Fatalf("create_note ran %d times, want 1 (idempotency)", got)
	}
}

// 5
func TestRejectedWriteIsUndoneLocallyAndListed(t *testing.T) {
	s := newServer(t)
	d := open(t, s)

	n := d.create("taken")
	if _, ok := d.note(n.Id); !ok {
		t.Fatal("the write is not in the local db right after done")
	}
	eventually(t, d, "rejected and undone", func() bool {
		_, local := d.note(n.Id)
		return d.c.Status().Rejected == 1 && !local
	})
	rej, err := d.c.Rejected()
	if err != nil || len(rej) != 1 || rej[0].Reason != "taken" || rej[0].Op != "notes.create_note" {
		t.Fatalf("Rejected() = %+v, %v", rej, err)
	}

	if err := d.c.Dismiss(rej[0].Id); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if rej, _ := d.c.Rejected(); len(rej) != 0 || d.c.Status().Rejected != 0 {
		t.Fatalf("after Dismiss: %+v, status %+v", rej, d.c.Status())
	}

	later := d.create("fine")
	eventually(t, d, "a later write goes through", func() bool { _, ok := s.note(later.Id); return ok })
}

// 6
func TestRejectedUpdateRestoresTheServersRow(t *testing.T) {
	s := newServer(t)
	s.put(&Note{Id: "n1", Owner: userID, Text: "a"})
	d := open(t, s)

	if err := d.call("notes.update_note", &Note{Id: "n1", Owner: userID, Text: "locked"}); err != nil {
		t.Fatalf("update_note: %v", err)
	}
	if n, _ := d.note("n1"); n == nil || n.Text != "locked" {
		t.Fatalf("local update not applied at once: %+v", n)
	}
	eventually(t, d, "server's text restored", func() bool {
		n, _ := d.note("n1")
		return n != nil && n.Text == "a" && d.c.Status().Rejected == 1
	})
	if rej, _ := d.c.Rejected(); len(rej) != 1 || rej[0].Reason != "locked" {
		t.Fatalf("Rejected() = %+v", rej)
	}
}

// 7
func TestPullAppliesCreatesDeletesAndScopeChanges(t *testing.T) {
	s := newServer(t)
	s.put(&Note{Id: "n1", Owner: userID, Text: "to delete"})
	s.put(&Note{Id: "n2", Owner: userID, Text: "to give away"})
	d := open(t, s)

	s.put(&Note{Id: "n3", Owner: userID, Text: "new"})
	if err := s.db.Delete(&Note{}, storage.Eq("id", "n1")); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Update(&Note{Id: "n2", Owner: otherUser, Text: "given"}, storage.Eq("id", "n2")); err != nil {
		t.Fatal(err)
	}

	eventually(t, d, "pulled", func() bool {
		_, n1 := d.note("n1")
		_, n2 := d.note("n2")
		_, n3 := d.note("n3")
		return !n1 && !n2 && n3
	})
}

// 8
func TestPullPagesPastFilteredChanges(t *testing.T) {
	s := newServer(t)
	d := open(t, s)

	for i := 0; i < 600; i++ {
		if err := s.db.Create(&Other{Id: fmt.Sprintf("o%04d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	s.put(&Note{Id: "last", Owner: userID, Text: "after 600 unsynced rows"})

	eventually(t, d, "note behind filtered pages", func() bool { _, ok := d.note("last"); return ok })
	eventually(t, d, "cursor at head", func() bool { return d.state().Cursor == s.log.Head() })
}

// 9
func TestPullWaitsForPush(t *testing.T) {
	s := newServer(t)
	d := open(t, s)
	s.fail503.Store(true)

	n := d.create("blocked")
	s.put(&Note{Id: "from-server", Owner: userID, Text: "x"})
	eventually(t, d, "push refused with 503", func() bool { return s.count(pathCreateNote) >= 1 })
	d.c.Sync()
	time.Sleep(200 * time.Millisecond)
	if _, ok := d.note("from-server"); ok {
		t.Fatal("pulled while a write was pending")
	}
	if s.count(pathPull) != 0 {
		t.Fatal("/pull was requested while a write was pending")
	}
	if st := d.c.Status(); st.Pending != 1 || !st.Online {
		t.Fatalf("status = %+v, want online with 1 pending", st)
	}

	s.fail503.Store(false)
	eventually(t, d, "both notes on both sides", func() bool {
		_, a := d.note("from-server")
		_, b := s.note(n.Id)
		return a && b
	})
}

// 10
func TestEpochChangeResendsToAStandbyThatMissedIt(t *testing.T) {
	s := newServer(t)
	d := open(t, s)
	n := d.create("survives failover")
	eventually(t, d, "delivered", func() bool { _, ok := s.note(n.Id); return ok && d.c.Status().Pending == 0 })

	// The standby missed the last seconds: neither the note nor its idempotency key.
	if err := s.db.Delete(&Note{}, storage.Eq("id", n.Id)); err != nil {
		t.Fatal(err)
	}
	s.freshIdempotency()
	s.restart()

	eventually(t, d, "note recreated on the standby", func() bool { _, ok := s.note(n.Id); return ok })
	eventually(t, d, "still local", func() bool { _, ok := d.note(n.Id); return ok })
}

// 11
func TestSecondTabIsRefused(t *testing.T) {
	s := newServer(t)
	_, err := dbsync.Open(config(s, mem.New(), fakeLock{free: false}))
	if err != dbsync.ErrAnotherTab {
		t.Fatalf("err = %v, want ErrAnotherTab", err)
	}
}

// 12
func TestFirstRunNeedsTheServer(t *testing.T) {
	s := newServer(t)
	s.stop()
	_, err := dbsync.Open(config(s, mem.New(), fakeLock{free: true}))
	if err != dbsync.ErrFirstRunNeedsServer {
		t.Fatalf("err = %v, want ErrFirstRunNeedsServer", err)
	}
}

// 13
func TestOnlyDeclaredOperationsRunOffline(t *testing.T) {
	s := newServer(t)
	d := open(t, s)
	for _, op := range []string{"notes.nope", "notes.ping", "notes.purge_notes"} {
		err := d.call(op, &Note{})
		if err == nil || !strings.Contains(err.Error(), "is not an offline operation") {
			t.Fatalf("%s: err = %v", op, err)
		}
	}
	if err := d.call("notes.get_note", &Note{Id: "missing"}); err == nil {
		t.Fatal("get_note of a missing note should fail locally")
	}
}

// 14
func TestUnsupportedScopeOperatorAnswers500(t *testing.T) {
	s := newServer(t)
	s.scope = func(string) ([]storage.Condition, error) {
		return []storage.Condition{storage.IsNotNull("owner")}, nil
	}
	s.restart()

	resp, err := http.Get(s.url + dbsync.DefaultPrefix + "/snapshot?table=note")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 500 || !strings.Contains(string(body), "scope operator") {
		t.Fatalf("got %d %q", resp.StatusCode, body)
	}
}

// 15
func TestFieldsSendOnlyTheChosenColumns(t *testing.T) {
	s := newServer(t)
	s.fields = func(string) ([]string, error) { return []string{"text"}, nil }
	s.restart()
	s.put(&Note{Id: "n1", Owner: userID, Text: "bootstrapped"})

	d := open(t, s)
	if n, ok := d.note("n1"); !ok || n.Text != "bootstrapped" || n.Owner != "" {
		t.Fatalf("snapshot row = %+v (%v), want id and text only", n, ok)
	}
	s.put(&Note{Id: "n2", Owner: userID, Text: "pulled"})
	eventually(t, d, "pulled row with id and text only", func() bool {
		n, ok := d.note("n2")
		return ok && n.Text == "pulled" && n.Owner == ""
	})
}

// 16
func TestUnknownFieldAnswers500(t *testing.T) {
	s := newServer(t)
	s.fields = func(string) ([]string, error) { return []string{"nope"}, nil }
	s.restart()

	resp, err := http.Get(s.url + dbsync.DefaultPrefix + "/snapshot?table=note")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 500 || !strings.Contains(string(body), "nope") {
		t.Fatalf("got %d %q", resp.StatusCode, body)
	}
}
