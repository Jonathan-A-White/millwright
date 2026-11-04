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

// A ticket its owner kept (Keep) outlives the owner: nobody holds its flock, and
// it still stands in its place until its keep time has passed (mw-gq6.939).
func TestAKeptTicketWhoseOwnerIsGoneStillStandsInItsPlaceUntilItsKeepTimePasses(t *testing.T) {
	now := time.Now()
	clock := &now
	q := aQueue(t)
	q.Now = func() time.Time { return *clock }
	kept, err := q.JoinAs("check mw-a", "laptop checking mw-a", now)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := q.Join("mw-b", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer mine.Drop()

	kept.Keep(now.Add(10 * time.Minute))
	if head, by := q.Head(mine.Name()); head || by != "laptop checking mw-a" {
		t.Errorf("expected the kept ticket to stand before mine, got %v and %q", head, by)
	}
	if place, of := q.Place(mine.Name()); place != 2 || of != 2 {
		t.Errorf("expected mine to be 2 of 2 behind the kept ticket, got %d of %d", place, of)
	}
	later := now.Add(11 * time.Minute)
	clock = &later
	if head, _ := q.Head(mine.Name()); !head {
		t.Error("expected a kept ticket past its keep time not to stand before mine")
	}
	if entries, _ := os.ReadDir(q.Dir); len(entries) != 1 {
		t.Errorf("expected the lapsed ticket cleared, %d tickets left", len(entries))
	}
}

// The same key takes the kept ticket up again: the same name, so the same place,
// not the back of the queue; another key does not take it.
func TestTheSameKeyTakesAKeptTicketUpAgainAndAnotherKeyDoesNot(t *testing.T) {
	now := time.Now()
	q := aQueue(t)
	q.Now = func() time.Time { return now }
	kept, err := q.JoinAs("check mw-a", "laptop checking mw-a", now)
	if err != nil {
		t.Fatal(err)
	}
	name := kept.Name()
	kept.Keep(now.Add(10 * time.Minute))
	between, err := q.Join("mw-b", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer between.Drop()

	other, err := q.JoinAs("check mw-c", "laptop checking mw-c", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Drop()
	if other.Name() == name {
		t.Fatal("expected another key to take a ticket of its own")
	}

	again, err := q.JoinAs("check mw-a", "laptop checking mw-a", now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if again.Name() != name {
		t.Errorf("expected the kept ticket %s to be taken up again, got %s", name, again.Name())
	}
	if head, _ := q.Head(again.Name()); !head {
		t.Error("expected the ticket taken up again to be the head, ahead of the one taken in between")
	}
	// Taken up, it is held again and no longer kept: a lapse of its old keep time
	// changes nothing while its owner lives.
	now = now.Add(30 * time.Minute)
	if head, _ := q.Head(between.Name()); head {
		t.Error("expected the ticket taken up to stand before mine while its owner lives")
	}
	again.Drop()
	if head, _ := q.Head(between.Name()); !head {
		t.Error("expected the ticket dropped to stand before none")
	}
}

// A kept ticket past its keep time is not taken up: the place was given up.
func TestAKeptTicketPastItsKeepTimeIsNotTakenUpAgain(t *testing.T) {
	now := time.Now()
	q := aQueue(t)
	q.Now = func() time.Time { return now }
	kept, err := q.JoinAs("check mw-a", "x", now)
	if err != nil {
		t.Fatal(err)
	}
	name := kept.Name()
	kept.Keep(now.Add(time.Minute))
	now = now.Add(2 * time.Minute)
	again, err := q.JoinAs("check mw-a", "x", now)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Drop()
	if again.Name() == name {
		t.Error("expected a lapsed place not to be taken up again")
	}
}

// A waiter that won its turn and dropped its ticket can put the place back under
// the ticket's own name, so that it stands where it stood and the same key takes
// it up again (mw-gq6.945).
func TestALeftTicketStandsInThePlaceOfTheOneItReplacesAndIsTakenUpByItsKey(t *testing.T) {
	q := aQueue(t)
	now := time.Now()
	mine, err := q.JoinAs("check x", "mw-a", now)
	if err != nil {
		t.Fatal(err)
	}
	behind, err := q.Join("mw-b", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	name := mine.Name()
	mine.Drop()

	place, of, err := q.Leave(name, "check x", "mw-a", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if place != 1 || of != 2 {
		t.Errorf("expected place 1 of 2, got %d of %d", place, of)
	}
	if head, by := q.Head(behind.Name()); head || by != "mw-a" {
		t.Errorf("expected the waiter behind to stand behind mw-a, got %v and %q", head, by)
	}
	again, err := q.JoinAs("check x", "mw-a", now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if again.Name() != name {
		t.Errorf("expected the key to take the left ticket %s up, got %s", name, again.Name())
	}
}
