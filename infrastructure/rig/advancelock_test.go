package rig_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// aGitThatWritesSlowly is a git whose merge is slow to write the tree: it
// dirties README.md, tells the test it has begun (the marker file), waits, puts
// the file back and then merges, as a fast-forward onto a big batch looks to
// anyone reading the checkout's status meanwhile (mw-gq6.794).
func aGitThatWritesSlowly(t *testing.T) (program, begun string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is not on PATH: %v", err)
	}
	dir := t.TempDir()
	begun = filepath.Join(dir, "begun")
	program = filepath.Join(dir, "git")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = merge ]; then
  echo half-written >> README.md
  : > %q
  sleep 1
  %q checkout -q -- README.md
fi
exec %q "$@"
`, begun, real, real)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in git: %v", err)
	}
	return program, begun
}

// TestTwoLevellersOnOneCheckoutMoveItOnceAndLeaveNothingAlone: mw land's own
// fast-forward and the follower that levels the mill's checkout after a
// landing ran at once, and the second read the first's half-written tree as
// work in progress (mw-gq6.794). Each is its own Worktrees, as two processes
// are; the lock is the checkout's, not the adapter's. The second waits, finds
// the checkout level and says so: no Left, so no mail to the Mayor.
func TestTwoLevellersOnOneCheckoutMoveItOnceAndLeaveNothingAlone(t *testing.T) {
	here, _ := aRig(t)
	merged := landed(t, here)
	program, begun := aGitThatWritesSlowly(t)

	var wg sync.WaitGroup
	results := make([]application.Advanced, 2)
	errs := make([]error, 2)
	advance := func(i int) {
		defer wg.Done()
		results[i], errs[i] = rig.New(rig.WithProgram(program)).Advance(context.Background(), here, "main", merged.Commit)
	}
	wg.Add(1)
	go advance(0)
	// The second starts while the first's merge is in the middle of its write.
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(begun); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first fast-forward never began")
		}
		time.Sleep(5 * time.Millisecond)
	}
	wg.Add(1)
	go advance(1)
	wg.Wait()

	moved := 0
	for i, got := range results {
		if errs[i] != nil {
			t.Fatalf("leveller %d: %v", i, errs[i])
		}
		if got.Left != "" || got.Held != "" {
			t.Errorf("leveller %d left the checkout alone or held it, which is a false alarm: %+v", i, got)
		}
		if !got.Current() {
			t.Errorf("leveller %d did not find the checkout current: %+v", i, got)
		}
		if got.Moved {
			moved++
		}
	}
	if moved != 1 {
		t.Errorf("expected exactly one of the two to fast-forward the checkout, %d did: %+v", moved, results)
	}
	if !results[1].Level || results[1].Moved {
		t.Errorf("expected the second to find the checkout already level, got %+v", results[1])
	}
	if got := run(t, here, "git", "rev-parse", "HEAD"); got != merged.Commit {
		t.Errorf("expected the checkout at %s, got %s", merged.Commit, got)
	}
}
