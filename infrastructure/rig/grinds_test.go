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

// A grind landed on another host reaches the remote's main, not this
// checkout's: after a Refresh the grind is read from the remote's main, and
// the checkout's own main and working tree are left as they were.
func TestGrindsAreReadFromTheRemotesMainOnceRefreshed(t *testing.T) {
	here, other := aRig(t)
	ctx := context.Background()
	grinds := rig.NewGrinds()
	was := run(t, here, "git", "rev-parse", "HEAD")

	write(t, other, "grinds/sweep.json", `{"grind":2}`+"\n")
	run(t, other, "git", "add", "-A")
	run(t, other, "git", "commit", "-qm", "A newer sweep grind, landed on the other host")
	run(t, other, "git", "push", "-q", "origin", "main")
	landed := run(t, other, "git", "rev-parse", "HEAD")

	if commit, err := grinds.Commit(ctx, here); err != nil || commit != was {
		t.Fatalf("expected main unmoved at %s before any refresh, got %q %v", was, commit, err)
	}
	if err := grinds.Refresh(ctx, here); err != nil {
		t.Fatalf("refreshing a checkout whose remote is there: %v", err)
	}
	commit, err := grinds.Commit(ctx, here)
	if err != nil || commit != landed {
		t.Fatalf("expected the remote's main %s, got %q %v", landed, commit, err)
	}
	if data, found, err := grinds.ReadAt(ctx, here, commit, "grinds/sweep.json"); err != nil || !found || string(data) != `{"grind":2}`+"\n" {
		t.Fatalf("expected the remote's grind, got %q %v %v", data, found, err)
	}
	if now := run(t, here, "git", "rev-parse", "refs/heads/main"); now != was {
		t.Errorf("expected the checkout's own main left at %s, it is at %s", was, now)
	}
}

// A landing made on this host and not yet pushed is ahead of the remote's
// main: the grind is read from it, not from the older remote.
func TestGrindsFromALocalLandingAheadOfTheRemoteAreKept(t *testing.T) {
	here, _ := aRig(t)
	write(t, here, "grinds/sweep.json", `{"grind":3}`+"\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Landed here, not pushed yet")
	ahead := run(t, here, "git", "rev-parse", "HEAD")

	grinds := rig.NewGrinds()
	if err := grinds.Refresh(context.Background(), here); err != nil {
		t.Fatal(err)
	}
	if commit, err := grinds.Commit(context.Background(), here); err != nil || commit != ahead {
		t.Fatalf("expected the local main %s, got %q %v", ahead, commit, err)
	}
}

// A remote that cannot be reached is an error from Refresh, and Commit still
// answers from the checkout's own main.
func TestAnUnreachableRemoteIsAnErrorAndMainStillAnswers(t *testing.T) {
	here, _ := aRig(t)
	main := run(t, here, "git", "rev-parse", "HEAD")
	run(t, here, "git", "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	grinds := rig.NewGrinds()
	if err := grinds.Refresh(context.Background(), here); err == nil {
		t.Fatal("expected a remote that is gone to be an error")
	}
	if commit, err := grinds.Commit(context.Background(), here); err != nil || commit != main {
		t.Fatalf("expected the local main %s, got %q %v", main, commit, err)
	}
}

// The files under a directory at a commit are listed whole, sorted, and only
// what is committed there; a directory that is not there lists none.
func TestGrindsListTheFilesUnderADirectoryAtACommit(t *testing.T) {
	here, _ := aRig(t)
	write(t, here, "grinds/sweep.json", "{}\n")
	write(t, here, "grinds/examples/sweep/b.json", "{}\n")
	write(t, here, "grinds/examples/sweep/a.json", "{}\n")
	write(t, here, "grinds/examples/sweep/blank.jpg", "jpeg")
	write(t, here, "other/file.json", "{}\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Grinds and examples")
	write(t, here, "grinds/examples/sweep/half-done.json", "{}\n")

	ctx := context.Background()
	grinds := rig.NewGrinds()
	commit, err := grinds.Commit(ctx, here)
	if err != nil {
		t.Fatal(err)
	}
	files, err := grinds.List(ctx, here, commit, "grinds")
	want := "grinds/examples/sweep/a.json grinds/examples/sweep/b.json grinds/examples/sweep/blank.jpg grinds/sweep.json"
	if err != nil || strings.Join(files, " ") != want {
		t.Fatalf("expected %q, got %q %v", want, files, err)
	}
	if none, err := grinds.List(ctx, here, commit, "nothing"); err != nil || len(none) != 0 {
		t.Fatalf("expected none under a directory that is not there, got %q %v", none, err)
	}
}
