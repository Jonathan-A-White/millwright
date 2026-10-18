package ticketq_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/ticketq"
)

func aQueue(t *testing.T) *ticketq.Queue {
	return &ticketq.Queue{Dir: filepath.Join(t.TempDir(), "q"), Cap: time.Hour, Now: time.Now}
}

func TestTheOldestLiveTicketIsTheHead(t *testing.T) {
	q := aQueue(t)
	now := time.Now()
	first, err := q.Join("mw-a", now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := q.Join("mw-b", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if head, _ := q.Head(first.Name()); !head {
		t.Error("expected the first to be the head")
	}
	if head, by := q.Head(second.Name()); head || by != "mw-a" {
		t.Errorf("expected the second to wait behind mw-a, got %v and %q", head, by)
	}
	first.Drop()
	if head, _ := q.Head(second.Name()); !head {
		t.Error("expected the second to be the head once the first dropped its ticket")
	}
	second.Drop()
	if entries, _ := os.ReadDir(q.Dir); len(entries) != 0 {
		t.Errorf("expected no tickets left, got %d", len(entries))
	}
}

func TestATicketNobodyHoldsIsSkippedAndCleared(t *testing.T) {
	q := aQueue(t)
	// A waiter that died leaves its ticket file with nobody holding its flock.
	if err := os.MkdirAll(q.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	deadName := fmt.Sprintf("%020d-%010d-0", time.Now().Add(-time.Minute).UnixNano(), 1)
	if err := os.WriteFile(filepath.Join(q.Dir, deadName), []byte("mw-dead\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mine, err := q.Join("mw-b", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer mine.Drop()
	if head, _ := q.Head(mine.Name()); !head {
		t.Error("expected a dead waiter not to stand before me")
	}
	if _, err := os.Stat(filepath.Join(q.Dir, deadName)); !os.IsNotExist(err) {
		t.Errorf("expected the dead ticket cleared, stat gave %v", err)
	}
}

func TestATicketOlderThanTheCapIsSkippedWhileItsOwnerLives(t *testing.T) {
	q := aQueue(t)
	old, err := q.Join("mw-old", time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Drop()
	mine, err := q.Join("mw-b", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer mine.Drop()
	if head, _ := q.Head(mine.Name()); !head {
		t.Error("expected an over-age ticket not to stand before me")
	}
}
