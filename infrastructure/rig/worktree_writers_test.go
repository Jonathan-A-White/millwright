package rig_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// A Builder's session can end while a test run it started in the background goes
// on writing inside its worktree (mw-5r3p30.882): git then cannot take the
// worktree away ("Directory not empty"), and the husk it leaves refuses every
// later attempt of the story (mw-gq6.876). These tests start such a process under
// a real worktree and show the clean-up ends it.

// aStoryWorktree cuts the worktree of story id off a rig and returns it with its
// branch.
func aStoryWorktree(t *testing.T, id string) (here, dir, branch string) {
	t.Helper()
	here, _ = aRig(t)
	dir = application.WorktreeDir(here, id)
	branch = application.StoryBranch(id)
	if err := rig.New().Add(context.Background(), here, dir, branch, "origin/main"); err != nil {
		t.Fatalf("cutting the worktree: %v", err)
	}
	return here, dir, branch
}

// startIn starts the shell script in dir and returns its pid; the test waits
// for it to have been reaped, and kills it if it is still there at the end.
func startIn(t *testing.T, dir, script string) (pid int, gone func() bool) {
	t.Helper()
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %q: %v", script, err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	return cmd.Process.Pid, func() bool {
		select {
		case <-done:
			return true
		case <-time.After(5 * time.Second):
			return false
		}
	}
}

func TestRemovingAWorktreeEndsTheTestRunStillWritingInIt(t *testing.T) {
	here, dir, branch := aStoryWorktree(t, "mw-gq6.w1")

	// A run that went on after its session was turn-capped: it writes
	// test-results all the while, and a sleeping child sits in the worktree.
	pid, gone := startIn(t, dir, `mkdir -p test-results; sleep 300 & while :; do : > test-results/f$RANDOM; done`)
	time.Sleep(200 * time.Millisecond)

	if err := rig.New().Remove(context.Background(), here, dir, branch); err != nil {
		t.Fatalf("removing a worktree a run still writes in: %v", err)
	}
	if !gone() {
		t.Fatalf("expected the run %d to have been ended by the clean-up", pid)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("expected %s to be gone", dir)
	}
	if out := run(t, here, "git", "branch", "--list", branch); out != "" {
		t.Fatalf("expected the branch to be gone, got %q", out)
	}
}

func TestRemovingWithoutForceEndsTheSleepingChildInTheWorktreeToo(t *testing.T) {
	here, dir, _ := aStoryWorktree(t, "mw-gq6.w2")
	pid, gone := startIn(t, dir, `sleep 300`)
	time.Sleep(200 * time.Millisecond)

	if err := rig.New().RemoveWithoutForce(context.Background(), here, dir); err != nil {
		t.Fatalf("removing: %v", err)
	}
	if !gone() {
		t.Fatalf("expected the child %d to have been ended", pid)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("expected %s to be gone", dir)
	}
}

// deadLink is what the failed removal of mw-5r3p30.882 left: the directory is
// there, and git has already lost its record of the worktree.
func deadLink(t *testing.T, here, dir string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(here, ".git", "worktrees", filepath.Base(dir))); err != nil {
		t.Fatalf("pruning the record: %v", err)
	}
}

func TestAWorktreeWhoseGitLinkIsDeadIsTakenAwayWhenItsTrackedFilesAreOnTheBranch(t *testing.T) {
	here, dir, branch := aStoryWorktree(t, "mw-gq6.w3")
	write(t, dir, "test-results/shot.png", "junk")
	write(t, dir, "node_modules/x/index.js", "junk")
	deadLink(t, here, dir)

	if err := rig.New().RemoveWithoutForce(context.Background(), here, dir); err != nil {
		t.Fatalf("removing a husk with a dead link: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("expected %s to be gone", dir)
	}
	// The branch is left to the caller, as RemoveWithoutForce leaves it.
	if out := run(t, here, "git", "branch", "--list", branch); out == "" {
		t.Fatal("expected the branch to be kept")
	}
}

func TestAWorktreeWhoseGitLinkIsDeadIsKeptWhenATrackedFileIsNotOnTheBranch(t *testing.T) {
	here, dir, _ := aStoryWorktree(t, "mw-gq6.w4")
	write(t, dir, "README.md", "# changed after the last commit\n")
	deadLink(t, here, dir)

	err := rig.New().RemoveWithoutForce(context.Background(), here, dir)
	if err == nil {
		t.Fatal("expected a husk holding a tracked file the branch lacks to be refused")
	}
	if !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("expected the refusal to name README.md, got %v", err)
	}
	if got, rerr := os.ReadFile(filepath.Join(dir, "README.md")); rerr != nil || !strings.Contains(string(got), "changed") {
		t.Fatalf("expected the file to be left as it was: %q %v", got, rerr)
	}
}

func TestAForcedRemovalTakesAwayAHuskWithADeadLink(t *testing.T) {
	here, dir, branch := aStoryWorktree(t, "mw-gq6.w5")
	write(t, dir, "README.md", "# changed\n")
	deadLink(t, here, dir)

	if err := rig.New().Remove(context.Background(), here, dir, branch); err != nil {
		t.Fatalf("removing a husk: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("expected %s to be gone", dir)
	}
	if out := run(t, here, "git", "branch", "--list", branch); out != "" {
		t.Fatalf("expected the branch to be gone, got %q", out)
	}
}

// failingGit is a git whose `worktree remove` says "Directory not empty" the
// first `times` it is run, then does what git does.
func failingGit(t *testing.T, times int) string {
	t.Helper()
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	script := filepath.Join(dir, "git")
	body := `#!/bin/sh
if [ "$1" = worktree ] && [ "$2" = remove ]; then
  n=$(cat '` + count + `' 2>/dev/null || echo 0)
  n=$((n+1)); echo $n > '` + count + `'
  if [ $n -le ` + strconv.Itoa(times) + ` ]; then echo "error: failed to delete 'x': Directory not empty" >&2; exit 1; fi
fi
exec git "$@"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestADirectoryNotEmptyIsTriedOnceMoreAfterTheWritersAreGone(t *testing.T) {
	here, dir, branch := aStoryWorktree(t, "mw-gq6.w6")
	if err := rig.New(rig.WithProgram(failingGit(t, 1))).Remove(context.Background(), here, dir, branch); err != nil {
		t.Fatalf("expected the second try to pass: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("expected %s to be gone", dir)
	}
}

func TestTheRefusalNamesTheProcessesItFound(t *testing.T) {
	here, dir, branch := aStoryWorktree(t, "mw-gq6.w7")
	pid, _ := startIn(t, dir, `exec sleep 301`)
	time.Sleep(200 * time.Millisecond)

	worktrees := rig.New(rig.WithProgram(failingGit(t, 99)))
	rig.LeaveWritersAloneForTest(worktrees)
	err := worktrees.Remove(context.Background(), here, dir, branch)
	if err == nil {
		t.Fatal("expected the removal to be refused")
	}
	for _, want := range []string{"Directory not empty", strconv.Itoa(pid), "sleep 301"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected the refusal to say %q, got %v", want, err)
		}
	}
}
