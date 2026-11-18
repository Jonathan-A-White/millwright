package vault_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/gittest"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// These tests are of a rebase that a pull left stopped in the vault
// (mw-gq6.1447): git status said "interactive rebase in progress ... Last
// command done: noop. No commands remaining.", HEAD was on no branch, and the
// next two mw commits went onto no branch and could not be pushed.

// said is what one git command prints in dir, and whether it succeeded.
func said(dir string, args ...string) (string, error) {
	cmd := gittest.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// onMain reports whether dir's HEAD is on the branch main, not detached.
func onMain(dir string) bool {
	branch, err := said(dir, "symbolic-ref", "-q", "HEAD")
	return err == nil && branch == "refs/heads/main"
}

// rebaseLeft reports whether dir's git directory still holds a rebase's state.
func rebaseLeft(dir string) bool {
	for _, state := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(dir, ".git", state)); err == nil {
			return true
		}
	}
	return false
}

// hook writes dir's post-checkout hook, which git runs inside a rebase right
// after it has checked out what it rebases onto: HEAD is detached, and the
// rebase has not ended.
func hook(t *testing.T, dir, script string) {
	t.Helper()
	path := filepath.Join(dir, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("writing the hook: %v", err)
	}
	t.Cleanup(func() { os.Remove(path) })
}

// behind makes two hosts, the other of which has pushed a commit this host
// has fetched and not yet taken: what a pull finds on most runs, and what git
// rebases with the one command "noop".
func behind(t *testing.T) (here, there string) {
	t.Helper()
	here, there = twoHosts(t)
	write(t, there, "runs/mw-1/closeout.md", "the other host closed a story out\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "Close out mw-1")
	run(t, there, "git", "push", "-q")
	run(t, here, "git", "fetch", "-q")
	return here, there
}

// stoppedWithNothingLeft leaves here as the Mayor found the vault: the pull's
// rebase was ended right after its checkout, so its state is still there, its
// one command (noop) is done, none remain and HEAD is on no branch.
func stoppedWithNothingLeft(t *testing.T) (here, there string) {
	t.Helper()
	here, there = behind(t)
	hook(t, here, "kill -9 $PPID")
	rebase := gittest.Command("git", "rebase", "@{upstream}")
	rebase.Dir = here
	if err := rebase.Run(); err == nil {
		t.Fatal("expected the rebase to be killed before it ended")
	}
	if err := os.Remove(filepath.Join(here, ".git", "hooks", "post-checkout")); err != nil {
		t.Fatalf("removing the hook: %v", err)
	}
	status, _ := said(here, "status")
	for _, want := range []string{"interactive rebase in progress", "noop", "No commands remaining"} {
		if !strings.Contains(status, want) {
			t.Fatalf("expected the stopped rebase of the report (%q), got:\n%s", want, status)
		}
	}
	if onMain(here) || !rebaseLeft(here) {
		t.Fatalf("expected HEAD on no branch and the rebase's state left, got:\n%s", status)
	}
	return here, there
}

// The report: a commit made after the pull's rebase stopped went onto no
// branch, and its push failed with "fatal: HEAD does not point to a branch".
func TestACommitAfterAPullsRebaseStoppedWithNothingLeftLandsOnMainAndIsPushed(t *testing.T) {
	here, there := stoppedWithNothingLeft(t)
	files := vault.New(here)
	ctx := context.Background()
	const message = "mw memory demote millwright the-one-lock-order-is"

	// The fact's file is tracked and changed, as a rig's memory is.
	write(t, here, "seats/mayor/ledger.md", "2026-09-17 the factory opened.\nA fact was demoted.\n")
	committed, err := files.Commit(ctx, message, []string{"seats/mayor/ledger.md"})
	if err != nil || len(committed) != 1 {
		t.Fatalf("expected the commit to be made, got %v (%v)", committed, err)
	}
	if pushed, err := files.Push(ctx); err != nil || pushed != 1 {
		t.Fatalf("expected the commit to be pushed, got %d (%v)", pushed, err)
	}

	if !onMain(here) {
		t.Error("expected HEAD back on main")
	}
	if rebaseLeft(here) {
		t.Error("expected the stopped rebase to be finished, its state is still there")
	}
	if subject, _ := said(here, "log", "-1", "--format=%s", "main"); subject != message {
		t.Errorf("expected the commit on main, its newest is %q", subject)
	}
	run(t, there, "git", "pull", "-q", "--rebase")
	if subject, _ := said(there, "log", "-1", "--format=%s"); subject != message {
		t.Errorf("expected the other host to get the commit, its newest is %q", subject)
	}
	if _, err := os.Stat(filepath.Join(here, "runs/mw-1/closeout.md")); err != nil {
		t.Errorf("expected what the pull brought in to be kept: %v", err)
	}
}

func TestAPullFinishesARebaseThatStoppedWithNothingLeft(t *testing.T) {
	here, _ := stoppedWithNothingLeft(t)

	if _, err := vault.New(here).Pull(context.Background()); err != nil {
		t.Fatalf("expected the pull to finish the stopped rebase and go on, got %v", err)
	}

	if !onMain(here) || rebaseLeft(here) {
		status, _ := said(here, "status")
		t.Fatalf("expected HEAD on main and no rebase left, got:\n%s", status)
	}
	main, _ := said(here, "rev-parse", "main")
	if upstream, _ := said(here, "rev-parse", "origin/main"); main != upstream {
		t.Errorf("expected main level with the remote (%s), it is at %s", upstream, main)
	}
}

// stoppedOnAConflict leaves here in a rebase that stopped part-way through a
// commit of this host's: nothing can finish it without a person.
func stoppedOnAConflict(t *testing.T) (here string) {
	t.Helper()
	here, there := twoHosts(t)
	write(t, there, "seats/mayor/ledger.md", "the other host's line\n")
	run(t, there, "git", "commit", "-qam", "The other host")
	run(t, there, "git", "push", "-q")
	write(t, here, "seats/mayor/ledger.md", "this host's line\n")
	run(t, here, "git", "commit", "-qam", "This host")
	run(t, here, "git", "fetch", "-q")
	rebase := gittest.Command("git", "rebase", "origin/main")
	rebase.Dir = here
	if err := rebase.Run(); err == nil {
		t.Fatal("expected the rebase to stop on a conflict")
	}
	if onMain(here) || !rebaseLeft(here) {
		t.Fatal("expected HEAD on no branch and the rebase's state left")
	}
	return here
}

func TestACommitRefusesOnADetachedHeadNamingTheStoppedRebase(t *testing.T) {
	here := stoppedOnAConflict(t)
	files := vault.New(here)
	ctx := context.Background()
	before, _ := said(here, "rev-parse", "HEAD")

	write(t, here, "rigs/millwright.md", "a fact\n")
	committed, err := files.Commit(ctx, "mw memory add millwright a-fact", []string{"rigs/millwright.md"})
	if err == nil {
		t.Fatalf("expected the commit to be refused on a detached HEAD, it committed %v", committed)
	}
	for _, want := range []string{"detached", "no branch", "rebase", "nothing was committed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to say %q, got %q", want, err)
		}
	}
	if after, _ := said(here, "rev-parse", "HEAD"); after != before {
		t.Errorf("expected nothing committed onto no branch, HEAD went from %s to %s", before, after)
	}
	if kept, err := os.ReadFile(filepath.Join(here, "rigs/millwright.md")); err != nil || string(kept) != "a fact\n" {
		t.Errorf("expected the written file to be left as it was, got %q (%v)", kept, err)
	}
}

// A pull that meets a rebase stopped part-way through this host's commits
// undoes it, as it undoes its own, and then pulls: the branch holds every
// commit this host made, and the other host's.
func TestAPullUndoesARebaseThatStoppedOnAConflictAndThenPulls(t *testing.T) {
	here := stoppedOnAConflict(t)
	files := vault.New(here)
	ctx := context.Background()

	// The pull merges a ledger by keeping both hosts' lines, so where the plain
	// rebase stopped, the pull's own goes through.
	if _, err := files.Pull(ctx); err != nil {
		t.Fatalf("expected the pull to undo the stopped rebase and pull, got %v", err)
	}
	if !onMain(here) || rebaseLeft(here) {
		status, _ := said(here, "status")
		t.Fatalf("expected HEAD on main and no rebase left, got:\n%s", status)
	}
	subjects, _ := said(here, "log", "--format=%s", "main")
	for _, want := range []string{"This host", "The other host"} {
		if !strings.Contains(subjects, want) {
			t.Errorf("expected the commit %q on main, it holds:\n%s", want, subjects)
		}
	}

	write(t, here, "rigs/millwright.md", "a fact\n")
	if committed, err := files.Commit(ctx, "mw memory add millwright a-fact", []string{"rigs/millwright.md"}); err != nil || len(committed) != 1 {
		t.Fatalf("expected the next commit to be made, got %v (%v)", committed, err)
	}
	if subject, _ := said(here, "log", "-1", "--format=%s", "main"); subject != "mw memory add millwright a-fact" {
		t.Errorf("expected the commit on main, its newest is %q", subject)
	}
}

// heldRebase makes here's next rebase wait, right after its checkout, until
// the test lets it go: it returns a file that appears when the rebase is
// there, and the func that lets it go.
func heldRebase(t *testing.T, here string) (reached string, letGo func()) {
	t.Helper()
	dir := t.TempDir()
	reached, gone := filepath.Join(dir, "reached"), filepath.Join(dir, "go")
	hook(t, here, fmt.Sprintf(`: > %q
n=0
while [ ! -e %q ] && [ $n -lt 200 ]; do sleep 0.05; n=$((n+1)); done`, reached, gone))
	return reached, func() {
		if err := os.WriteFile(gone, nil, 0o644); err != nil {
			t.Fatalf("letting the rebase go: %v", err)
		}
	}
}

// waitFor waits for a file to appear.
func waitFor(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

// mw told to stop while its pull is in the rebase: the rebase is not cut off
// half-way, and no state of it is left for the next commit to trip over.
func TestAPullEndedInTheMiddleOfItsRebaseLeavesNoStoppedRebase(t *testing.T) {
	here, _ := behind(t)
	reached, letGo := heldRebase(t, here)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = vault.New(here).Pull(ctx) // what it returns is not this test's: the vault's state is
	}()
	waitFor(t, reached)
	cancel()
	letGo()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("expected the pull to return")
	}

	if !onMain(here) || rebaseLeft(here) {
		status, _ := said(here, "status")
		t.Fatalf("expected HEAD on main and no rebase left, got:\n%s", status)
	}
	main, _ := said(here, "rev-parse", "main")
	if upstream, _ := said(here, "rev-parse", "origin/main"); main != upstream {
		t.Errorf("expected the rebase to have run to its end (main at %s), main is at %s", upstream, main)
	}
}

// A sibling mw command's commit that arrives while a pull is in its rebase
// waits for the rebase to end, and lands on main.
func TestACommitDuringAPullsRebaseWaitsForItAndLandsOnMain(t *testing.T) {
	here, _ := behind(t)
	reached, letGo := heldRebase(t, here)
	ctx := context.Background()
	const message = "mw memory add millwright a-new-fact"

	pulled := make(chan error, 1)
	go func() { _, err := vault.New(here).Pull(ctx); pulled <- err }()
	waitFor(t, reached)

	write(t, here, "rigs/millwright.md", "a new fact\n")
	committed := make(chan error, 1)
	go func() {
		_, err := vault.New(here).Commit(ctx, message, []string{"rigs/millwright.md"})
		committed <- err
	}()
	// Long enough for a commit that does not wait to have landed in the rebase.
	select {
	case err := <-committed:
		committed <- err
	case <-time.After(500 * time.Millisecond):
	}
	letGo()

	if err := <-pulled; err != nil {
		t.Errorf("expected the pull to end well, got %v", err)
	}
	if err := <-committed; err != nil {
		t.Errorf("expected the commit to be made, got %v", err)
	}
	if !onMain(here) || rebaseLeft(here) {
		status, _ := said(here, "status")
		t.Fatalf("expected HEAD on main and no rebase left, got:\n%s", status)
	}
	if subject, _ := said(here, "log", "-1", "--format=%s", "main"); subject != message {
		t.Errorf("expected the commit on main, its newest is %q", subject)
	}
	if _, err := os.Stat(filepath.Join(here, "runs/mw-1/closeout.md")); err != nil {
		t.Errorf("expected what the pull brought in to be kept: %v", err)
	}
}

// A sibling mw command writes a tracked file while a pull that has nothing of
// this host's to replay is in its rebase: git stops the rebase after its
// checkout ("cannot rebase: You have unstaged changes"). Undoing it would
// reset the sibling's write away; the pull finishes it instead.
func TestAPullWithNothingToReplayKeepsATrackedFileASiblingWroteDuringItsRebase(t *testing.T) {
	here, _ := behind(t)
	const written = "2026-09-17 the factory opened.\nA line a sibling wrote and has not committed yet.\n"
	hook(t, here, "echo 'A line a sibling wrote and has not committed yet.' >> seats/mayor/ledger.md")

	if _, err := vault.New(here).Pull(context.Background()); err != nil {
		t.Fatalf("expected the pull to end well, got %v", err)
	}

	if !onMain(here) || rebaseLeft(here) {
		status, _ := said(here, "status")
		t.Fatalf("expected HEAD on main and no rebase left, got:\n%s", status)
	}
	if kept, err := os.ReadFile(filepath.Join(here, "seats/mayor/ledger.md")); err != nil || string(kept) != written {
		t.Errorf("expected the sibling's write to be kept, got %q (%v)", kept, err)
	}
	main, _ := said(here, "rev-parse", "main")
	if upstream, _ := said(here, "rev-parse", "origin/main"); main != upstream {
		t.Errorf("expected main level with the remote (%s), it is at %s", upstream, main)
	}
}
