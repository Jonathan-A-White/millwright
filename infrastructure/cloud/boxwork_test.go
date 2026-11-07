package cloud_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/cloud"
	"github.com/Jonathan-A-White/millwright/infrastructure/gittest"
)

func TestMain(m *testing.M) {
	gittest.Isolate()
	os.Exit(m.Run())
}

func runIn(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := gittest.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, out)
	}
}

// aBox makes the home directory of a box whose ssh is the shell on this host:
// ~/.mw-worktrees/mw-x.1 is a clone of a bare origin, with one commit ahead of
// origin's main, a file changed and a file added and not committed. The fake
// ssh runs the remote command it is given here, under the box's HOME.
func aBox(t *testing.T) (box cloud.BoxWork, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	origin := filepath.Join(home, "origin.git")
	seed := filepath.Join(home, "seed")
	wt := filepath.Join(home, ".mw-worktrees", "mw-x.1")
	for _, d := range []string{origin, seed, filepath.Dir(wt)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runIn(t, origin, "git", "init", "-q", "--bare", "-b", "main")
	runIn(t, seed, "git", "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(t, seed, "git", "add", "-A")
	runIn(t, seed, "git", "commit", "-q", "-m", "first")
	runIn(t, seed, "git", "push", "-q", origin, "main")
	runIn(t, home, "git", "clone", "-q", origin, wt)
	runIn(t, wt, "git", "checkout", "-q", "-b", "mw/mw-x.1")
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(t, wt, "git", "add", "-A")
	runIn(t, wt, "git", "commit", "-q", "-m", "ahead")
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("one\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("added\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ssh := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nfor last; do :; done\neval \"$last\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return cloud.BoxWork{
		Command: ssh,
		Address: func(context.Context, string) (application.CloudAddress, error) {
			return application.CloudAddress{User: "mw", Host: "10.88.0.8"}, nil
		},
	}, home
}

func TestBoxWorkListsABoxsWorktreesWithWhatTheyHold(t *testing.T) {
	box, home := aBox(t)
	trees, err := box.Worktrees(context.Background(), "cloud4")
	if err != nil {
		t.Fatal(err)
	}
	if len(trees) != 1 {
		t.Fatalf("expected one worktree, got %+v", trees)
	}
	got := trees[0]
	if got.Story != "mw-x.1" || got.Branch != "mw/mw-x.1" || got.Ahead != 1 || got.Dir != filepath.Join(home, ".mw-worktrees", "mw-x.1") || got.Head == "" {
		t.Errorf("expected mw-x.1 on mw/mw-x.1, one ahead, got %+v", got)
	}
	if strings.Join(got.Uncommitted, ",") != "a.txt,new.txt" {
		t.Errorf("expected a.txt and new.txt uncommitted, got %v", got.Uncommitted)
	}
}

func TestBoxWorkBundlesAndVerifiesTheBranchAndDiffsWhatIsUncommitted(t *testing.T) {
	box, home := aBox(t)
	ctx := context.Background()
	trees, err := box.Worktrees(ctx, "cloud4")
	if err != nil || len(trees) != 1 {
		t.Fatalf("worktrees: %v %+v", err, trees)
	}
	dest := filepath.Join(t.TempDir(), "runs", "mw-x.1", "attempt-1.bundle")
	if err := box.Bundle(ctx, "cloud4", trees[0], dest); err != nil {
		t.Fatal(err)
	}
	if err := box.VerifyBundle(ctx, "cloud4", trees[0], dest); err != nil {
		t.Errorf("expected the bundle to verify: %v", err)
	}
	heads, err := gittest.Command("git", "bundle", "list-heads", dest).CombinedOutput()
	if err != nil || !strings.Contains(string(heads), trees[0].Head) {
		t.Errorf("expected the bundle to hold %s, got %s (%v)", trees[0].Head, heads, err)
	}
	if _, err := os.Stat(dest + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected no part file left, got %v", err)
	}
	diff, err := box.UncommittedDiff(ctx, "cloud4", trees[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"+changed", "new.txt", "+added"} {
		if !strings.Contains(diff, want) {
			t.Errorf("expected %q in the diff:\n%s", want, diff)
		}
	}
	// The diff applies to a clean checkout of the head, and the box's own
	// index is as it was.
	clean := filepath.Join(t.TempDir(), "clean")
	runIn(t, home, "git", "clone", "-q", filepath.Join(home, "origin.git"), clean)
	runIn(t, clean, "git", "merge", "-q", "--ff-only", "HEAD")
	patch := filepath.Join(t.TempDir(), "p.patch")
	if err := os.WriteFile(patch, []byte(diff), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(t, clean, "git", "fetch", "-q", filepath.Join(home, ".mw-worktrees", "mw-x.1"), "mw/mw-x.1")
	runIn(t, clean, "git", "checkout", "-q", "FETCH_HEAD")
	runIn(t, clean, "git", "apply", patch)
	status, _ := exec.Command("git", "-C", trees[0].Dir, "diff", "--cached", "--name-only").Output()
	if strings.TrimSpace(string(status)) != "" {
		t.Errorf("expected the box's index untouched, got %q", status)
	}
}

func TestBoxWorkRefusesAnUnreachableBoxAndAnUnreadableWorktree(t *testing.T) {
	box, _ := aBox(t)
	box.Address = func(context.Context, string) (application.CloudAddress, error) {
		return application.CloudAddress{}, errors.New("no peer cloud4 on the hub")
	}
	if _, err := box.Worktrees(context.Background(), "cloud4"); err == nil || !strings.Contains(err.Error(), "no peer cloud4") {
		t.Errorf("expected the address failure, got %v", err)
	}
	box, home := aBox(t)
	broken := filepath.Join(home, ".mw-worktrees", "mw-y.2")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, ".git"), []byte("gitdir: /nowhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := box.Worktrees(context.Background(), "cloud4"); err == nil || !strings.Contains(err.Error(), "mw-y.2") {
		t.Errorf("expected an error naming the unreadable worktree, got %v", err)
	}
}
