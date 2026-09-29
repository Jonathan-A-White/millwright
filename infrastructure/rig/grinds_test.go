package rig_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// An app's grind is read from its checkout's local main at a commit: what is
// committed there, never what is half-done in the working tree, and a path
// that is not a file there is not found rather than an error.
func TestGrindsAreReadFromMainAtACommit(t *testing.T) {
	here, _ := aRig(t)
	write(t, here, "grinds/sweep.json", `{"grind":1}`+"\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The sweep grind")
	committed := run(t, here, "git", "rev-parse", "HEAD")
	write(t, here, "grinds/sweep.json", `{"grind":2, "half done":true}`+"\n")

	ctx := context.Background()
	grinds := rig.NewGrinds()
	commit, err := grinds.Commit(ctx, here)
	if err != nil || commit != committed {
		t.Fatalf("expected main at %s, got %q %v", committed, commit, err)
	}
	data, found, err := grinds.ReadAt(ctx, here, commit, "grinds/sweep.json")
	if err != nil || !found || string(data) != `{"grind":1}`+"\n" {
		t.Fatalf("expected the committed grind, got %q %v %v", data, found, err)
	}
	for _, missing := range []string{"grinds/trip.json", "grinds", "../outside.json"} {
		if _, found, err := grinds.ReadAt(ctx, here, commit, missing); found || (err != nil && !strings.Contains(missing, "..")) {
			t.Errorf("expected %s not found, got found=%v %v", missing, found, err)
		}
	}
}

// A checkout whose main has moved on is read where main is, not where a
// branch someone has checked out is.
func TestGrindsFollowMainNotTheCheckedOutBranch(t *testing.T) {
	here, _ := aRig(t)
	main := run(t, here, "git", "rev-parse", "HEAD")
	run(t, here, "git", "checkout", "-qb", "mw/story")
	write(t, here, "grinds/sweep.json", "{}\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Not landed yet")

	grinds := rig.NewGrinds()
	commit, err := grinds.Commit(context.Background(), here)
	if err != nil || commit != main {
		t.Fatalf("expected main's commit %s, got %q %v", main, commit, err)
	}
	if _, found, _ := grinds.ReadAt(context.Background(), here, commit, "grinds/sweep.json"); found {
		t.Fatal("a grind not on main was read")
	}
}

func TestGrindsOfADirectoryThatIsNoCheckoutIsAnError(t *testing.T) {
	if !rig.Available() {
		t.Skip("git is not on PATH")
	}
	dir := filepath.Join(t.TempDir(), "nothing")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.NewGrinds().Commit(context.Background(), dir); err == nil {
		t.Fatal("expected a directory with no main to be an error")
	}
}
