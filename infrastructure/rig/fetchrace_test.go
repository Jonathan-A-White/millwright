package rig_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// lostRefLock is what git said when a close-out's fetch met another fetch in
// the same checkout (mw-ev7oyy.14, mw-gq6.439).
const lostRefLock = "error: cannot lock ref 'refs/remotes/origin/main': is at 4b67387 but expected 732d955"

// aGitFailingFirst is a stand-in for git that fails its first fails calls
// with what, exit status 1, and works after that. It returns the program and
// a function that counts the calls made of it so far.
func aGitFailingFirst(t *testing.T, fails int, what string) (string, func() int) {
	t.Helper()
	dir := t.TempDir()
	program := filepath.Join(dir, "git")
	calls := filepath.Join(dir, "calls")
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >> %q
n=$(wc -l < %q)
if [ "$n" -le %d ]; then
	echo %q >&2
	exit 1
fi
exit 0
`, calls, calls, fails, what)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for git: %v", err)
	}
	return program, func() int {
		data, err := os.ReadFile(calls)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "\n")
	}
}

// A fetch that lost the lock on origin/main to another fetch in the same
// checkout is a passing fault, which a close-out tries again (mw-gq6.439);
// tried again, it passes.
func TestAFetchThatLostTheRefLockToAnotherFetchIsAPassingFaultAndPassesTriedAgain(t *testing.T) {
	program, calls := aGitFailingFirst(t, 1, lostRefLock)
	worktrees := rig.New(rig.WithProgram(program))
	dir := t.TempDir()

	err := worktrees.Fetch(context.Background(), dir)
	fault, ok := application.Faulted(err)
	if !ok || fault.Why != "cannot lock ref" {
		t.Fatalf("expected a passing fault, cannot lock ref, got %v", err)
	}
	if !strings.Contains(err.Error(), "is at 4b67387 but expected 732d955") {
		t.Errorf("expected the fault to keep git's own words, got %v", err)
	}
	if err := worktrees.Fetch(context.Background(), dir); err != nil {
		t.Fatalf("expected the fetch tried again to pass, got %v", err)
	}
	if calls() != 2 {
		t.Fatalf("expected two fetches, got %d", calls())
	}
}

// git says a lost lock a second way, on the line of the ref it could not move.
func TestAFetchThatWasUnableToUpdateALocalRefIsAPassingFault(t *testing.T) {
	program, _ := aGitFailingFirst(t, 1, " ! 732d955..4b67387  main       -> origin/main  (unable to update local ref)")
	worktrees := rig.New(rig.WithProgram(program))

	err := worktrees.Fetch(context.Background(), t.TempDir())
	if fault, ok := application.Faulted(err); !ok || fault.Why != "unable to update local ref" {
		t.Fatalf("expected a passing fault, unable to update local ref, got %v", err)
	}
}

// The mill's grind refresh has nobody to try it again but itself: a lost lock
// is tried again there and then, a short wait apart, and the refresh passes.
func TestAGrindRefreshThatLostTheRefLockIsTriedAgainAndPasses(t *testing.T) {
	program, calls := aGitFailingFirst(t, 1, lostRefLock)
	grinds := rig.NewGrinds(rig.WithProgram(program), rig.WithRaceWait(0))

	if err := grinds.Refresh(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("expected the refresh to pass once tried again, got %v", err)
	}
	if calls() != 2 {
		t.Fatalf("expected two fetches, got %d", calls())
	}
}

// A lock that stays lost is tried again RaceRetries times and then reported,
// in git's words.
func TestAGrindRefreshThatKeepsLosingTheRefLockStopsAfterItsRetries(t *testing.T) {
	program, calls := aGitFailingFirst(t, 100, lostRefLock)
	grinds := rig.NewGrinds(rig.WithProgram(program), rig.WithRaceWait(0))

	err := grinds.Refresh(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "cannot lock ref") {
		t.Fatalf("expected the lost lock reported, got %v", err)
	}
	if calls() != 1+rig.RaceRetries {
		t.Fatalf("expected %d fetches, got %d", 1+rig.RaceRetries, calls())
	}
}

// Any other failure of the refresh stops as it always did, at once.
func TestAGrindRefreshThatFailsForAnotherReasonIsNotTriedAgain(t *testing.T) {
	program, calls := aGitFailingFirst(t, 100, "fatal: couldn't find remote ref main")
	grinds := rig.NewGrinds(rig.WithProgram(program), rig.WithRaceWait(0))

	if err := grinds.Refresh(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected the refresh to fail")
	}
	if calls() != 1 {
		t.Fatalf("expected one fetch, got %d", calls())
	}
}
