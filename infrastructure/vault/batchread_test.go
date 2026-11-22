package vault_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// pushFrom commits what was written in a clone and pushes it to the origin.
func pushFrom(t *testing.T, dir, message string) {
	t.Helper()
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", message)
	run(t, dir, "git", "push", "-q")
}

// A checkout behind another host's landing answers for the batch the other host
// landed, not for an older one (mw-gq6.1664).
func TestBatchOfReadsTheOriginsBatchesAheadOfThisCheckout(t *testing.T) {
	here, there := twoHosts(t)
	ctx := context.Background()

	write(t, here, "runs/batch/lampas/632/outcome.txt", "landed aaa on main, 1 stories: x\n")
	pushFrom(t, here, "batch 632")
	run(t, there, "git", "pull", "-q")

	// The other host lands 633 carrying story x again, and 634 carrying y.
	write(t, there, "runs/batch/lampas/633/outcome.txt", "landed bbb on main, 2 stories: x, z\n")
	write(t, there, "runs/batch/lampas/634/outcome.txt", "held: the rig's tests failed\n")
	pushFrom(t, there, "batches 633 and 634")

	v := vault.New(here)
	if n, ok, err := v.BatchOf(ctx, "lampas", "z"); err != nil || !ok || n != 633 {
		t.Fatalf("expected story z in batch 633 from the origin, got %d, %v (%v)", n, ok, err)
	}
	if n, ok, err := v.BatchOf(ctx, "lampas", "x"); err != nil || !ok || n != 633 {
		t.Fatalf("expected the newest batch of story x, 633, got %d, %v (%v)", n, ok, err)
	}
	if _, ok, err := v.BatchOf(ctx, "lampas", "nobody"); err != nil || ok {
		t.Fatalf("expected no batch for a story nobody landed, got %v (%v)", ok, err)
	}
}

// The newest bare batch is read from the origin too: a batch the other host has
// landed is not bare, and one it left without an outcome is named.
func TestWithoutOutcomeReadsTheOriginsBatchesAheadOfThisCheckout(t *testing.T) {
	here, there := twoHosts(t)
	ctx := context.Background()

	// Here: lampas batch 632 is bare; millwright 5 has an outcome.
	write(t, here, "runs/batch/lampas/632/gate.log", "x")
	write(t, here, "runs/batch/millwright/5/outcome.txt", "landed aaa on main, 1 stories: x\n")
	pushFrom(t, here, "batches")
	run(t, there, "git", "pull", "-q")

	// There: lampas 632 got its outcome, and lampas 633 landed; millwright 6 began.
	write(t, there, "runs/batch/lampas/632/outcome.txt", "landed bbb on main, 1 stories: y\n")
	write(t, there, "runs/batch/lampas/633/outcome.txt", "landed ccc on main, 1 stories: z\n")
	write(t, there, "runs/batch/millwright/6/gate.log", "x")
	write(t, there, "runs/batch/quiz/1/gate.log", "x")
	pushFrom(t, there, "more batches")

	got, err := vault.New(here).WithoutOutcome(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("expected millwright 6 and quiz 1 bare, got %v (%v)", got, err)
	}
	if got[0].Rig != "millwright" || got[0].Number != 6 || got[1].Rig != "quiz" || got[1].Number != 1 {
		t.Fatalf("expected millwright 6 then quiz 1, got %v", got)
	}
}

// With the origin gone, both read this checkout's folders alone, as before.
func TestBatchReadsFallBackToThisCheckoutWhenTheOriginCannotBeFetched(t *testing.T) {
	here, _ := twoHosts(t)
	ctx := context.Background()
	write(t, here, "runs/batch/lampas/3/outcome.txt", "landed aaa on main, 1 stories: x\n")
	write(t, here, "runs/batch/lampas/4/gate.log", "x")
	run(t, here, "git", "remote", "set-url", "origin", t.TempDir()+"/gone.git")

	v := vault.New(here)
	if n, ok, err := v.BatchOf(ctx, "lampas", "x"); err != nil || !ok || n != 3 {
		t.Fatalf("expected batch 3 from the checkout, got %d, %v (%v)", n, ok, err)
	}
	got, err := v.WithoutOutcome(ctx)
	if err != nil || len(got) != 1 || got[0].Rig != "lampas" || got[0].Number != 4 {
		t.Fatalf("expected batch 4 of lampas bare, got %v (%v)", got, err)
	}
}
