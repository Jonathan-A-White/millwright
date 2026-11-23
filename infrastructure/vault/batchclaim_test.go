package vault_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// sharedNotes is one beads server's key-value store, as two hosts see it.
type sharedNotes struct {
	mu   sync.Mutex
	m    map[string]string
	down bool
}

func (s *sharedNotes) Note(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.down {
		return "", errors.New("the beads server is not reachable")
	}
	return s.m[key], nil
}

func (s *sharedNotes) SetNote(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.down {
		return errors.New("the beads server is not reachable")
	}
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[key] = value
	return nil
}

func (s *sharedNotes) ClearNote(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func numbersOf(notes application.SlotNotes, host string) application.BatchNumbers {
	return application.BatchNumbers{Notes: notes, Host: host, Settle: 30 * time.Millisecond}
}

// Two hosts that number a batch of one rig in the same moment, neither having
// committed its folder yet, get two numbers (mw-gq6.1665: both called it 637).
func TestTwoHostsNumberingAtOnceGetTwoNumbers(t *testing.T) {
	for round := 0; round < 5; round++ {
		here, there := twoHosts(t)
		write(t, here, "runs/batch/lampas/636/outcome.txt", "landed aaa on main, 1 stories: x\n")
		pushFrom(t, here, "batch 636")
		run(t, there, "git", "pull", "-q")

		notes := &sharedNotes{}
		a := vault.New(here, vault.WithBatchNumbers(numbersOf(notes, "desktop")))
		b := vault.New(there, vault.WithBatchNumbers(numbersOf(notes, "laptop")))

		var got [2]int
		var errs [2]error
		var wg sync.WaitGroup
		for i, v := range []*vault.Vault{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got[i], errs[i] = v.NewBatch(context.Background(), "lampas")
			}()
		}
		wg.Wait()
		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("round %d: %v, %v", round, errs[0], errs[1])
		}
		if got[0] == got[1] || got[0] < 637 || got[1] < 637 {
			t.Fatalf("round %d: expected two different numbers above 636, got %d and %d", round, got[0], got[1])
		}
	}
}

// A number is taken above what the claim store says and above every folder, so
// numbers never go backwards.
func TestBatchNumbersStayAboveEveryFolderAndEveryClaim(t *testing.T) {
	here, _ := twoHosts(t)
	write(t, here, "runs/batch/lampas/700/outcome.txt", "x")
	pushFrom(t, here, "batch 700")
	notes := &sharedNotes{m: map[string]string{application.BatchNumberKey("lampas"): "650 tok laptop 2026-11-23T00:00:00Z"}}
	v := vault.New(here, vault.WithBatchNumbers(numbersOf(notes, "desktop")))
	if got, err := v.NewBatch(context.Background(), "lampas"); err != nil || got != 701 {
		t.Fatalf("expected 701 above the folders, got %d (%v)", got, err)
	}
	// And the store's own number holds when the folders are behind it.
	if got, err := v.NewBatch(context.Background(), "lampas"); err != nil || got != 702 {
		t.Fatalf("expected 702, got %d (%v)", got, err)
	}
	notes.m[application.BatchNumberKey("lampas")] = "900 tok laptop 2026-11-23T00:00:00Z"
	if got, err := v.NewBatch(context.Background(), "lampas"); err != nil || got != 901 {
		t.Fatalf("expected 901 above the store's 900, got %d (%v)", got, err)
	}
}

// A store that cannot be reached numbers the batch as before and says so.
func TestBatchNumberingFallsBackWhenTheStoreCannotBeReached(t *testing.T) {
	here, _ := twoHosts(t)
	write(t, here, "runs/batch/lampas/5/outcome.txt", "x")
	pushFrom(t, here, "batch 5")
	notes := &sharedNotes{down: true}
	v := vault.New(here, vault.WithBatchNumbers(numbersOf(notes, "desktop")))
	got, err := v.NewBatch(context.Background(), "lampas")
	if err != nil || got != 6 {
		t.Fatalf("expected 6 from the folders, got %d (%v)", got, err)
	}
	note, err := os.ReadFile(filepath.Join(here, "runs/batch/lampas/6", vault.BatchNumberingNoteFile))
	if err != nil || !strings.Contains(string(note), "beads server") {
		t.Fatalf("expected the unreachable store said in the numbering note, got %q (%v)", note, err)
	}
}

// A batch folder another host already pushed is not written over: this
// checkout's own is kept apart and the origin's is told apart from it.
func TestAFolderTheOriginHoldsIsKeptApartAndNeverOverwritten(t *testing.T) {
	here, there := twoHosts(t)
	ctx := context.Background()
	write(t, there, "runs/batch/lampas/637/outcome.txt", "landed aaa on main, 6 stories: a\n")
	pushFrom(t, there, "batch 637")

	write(t, here, "runs/batch/lampas/637/outcome.txt", "landed bbb on main, 2 stories: b\n")
	v := vault.New(here)
	taken, err := v.BatchTakenInOrigin(ctx, "lampas", 637)
	if err != nil || !taken {
		t.Fatalf("expected 637 taken in the origin, got %v (%v)", taken, err)
	}
	if taken, _ := v.BatchTakenInOrigin(ctx, "lampas", 638); taken {
		t.Fatalf("expected 638 free")
	}
	apart, err := v.KeepBatchApart(ctx, "lampas", 637, "NOT KEPT AS BATCH 637: taken")
	if err != nil || apart != "runs/batch/lampas/637.apart" {
		t.Fatalf("expected the folder moved to 637.apart, got %q (%v)", apart, err)
	}
	if _, err := os.Stat(filepath.Join(here, "runs/batch/lampas/637")); err == nil {
		t.Fatalf("expected no numbered folder left to be overwritten by or to overwrite the origin's")
	}
	got, _ := os.ReadFile(filepath.Join(here, apart, "outcome.txt"))
	if !strings.Contains(string(got), "landed bbb") || !strings.HasSuffix(string(got), "NOT KEPT AS BATCH 637: taken\n") {
		t.Fatalf("expected the outcome to keep its text and say it was not kept, got %q", got)
	}
	// BatchOf does not take the apart folder for a batch.
	if n, ok, _ := v.BatchOf(ctx, "lampas", "b"); ok {
		t.Fatalf("expected no batch for story b, got %d", n)
	}
}
