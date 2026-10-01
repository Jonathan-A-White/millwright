package rig_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// aRigWithAServer is a rig whose main holds a server/ and a site, and a story
// branch cut from it.
func aRigWithAServer(t *testing.T) (here string) {
	t.Helper()
	here, _ = aRig(t)
	write(t, here, "server/main.txt", "the backend\n")
	write(t, here, "src/app.txt", "the app\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "A server and an app")
	run(t, here, "git", "push", "-q", "origin", "main")
	return here
}

func TestChangedSaysWhetherTheStoryTouchedTheDirectory(t *testing.T) {
	here := aRigWithAServer(t)
	ctx := context.Background()
	worktrees := rig.New()

	run(t, here, "git", "checkout", "-q", "-b", "mw/app-only")
	write(t, here, "src/app.txt", "a newer app\n")
	run(t, here, "git", "commit", "-qam", "Only the app")
	run(t, here, "git", "checkout", "-q", "-b", "mw/both", "main")
	write(t, here, "server/new.txt", "a route\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "A route")

	for branch, want := range map[string]bool{"mw/app-only": false, "mw/both": true} {
		got, err := worktrees.Changed(ctx, here, "main", branch, "server")
		if err != nil || got != want {
			t.Errorf("%s: expected %v, got %v, %v", branch, want, got, err)
		}
	}

	// What the target gained meanwhile is not the story's change.
	run(t, here, "git", "checkout", "-q", "main")
	write(t, here, "server/other.txt", "another host's route\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Another host's route")
	if got, err := worktrees.Changed(ctx, here, "main", "mw/app-only", "server"); err != nil || got {
		t.Errorf("expected the story that left server/ alone to be so, got %v, %v", got, err)
	}

	if _, err := worktrees.Changed(ctx, here, "main", "mw/both", "../elsewhere"); err == nil {
		t.Error("expected a directory outside the rig to be refused")
	}
}

// Building leaves the binary where it was told and the rig as it was, and never
// reaches for install or systemctl: stand-ins for both are first on PATH, and
// either being run would leave its mark.
func TestBuildStagesTheBinaryAtTheCommitAndTouchesNothingLive(t *testing.T) {
	here := aRigWithAServer(t)
	ctx := context.Background()
	worktrees := rig.New()
	commit := run(t, here, "git", "rev-parse", "HEAD")

	bin := t.TempDir()
	marks := filepath.Join(t.TempDir(), "marks")
	for _, name := range []string{"install", "systemctl"} {
		script := "#!/bin/sh\necho \"" + name + " $*\" >> " + marks + "\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// A later commit the build must not see.
	write(t, here, "server/main.txt", "a later backend\n")
	run(t, here, "git", "commit", "-qam", "Later")

	live := filepath.Join(t.TempDir(), "postern")
	if err := os.WriteFile(live, []byte("the live binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "stage", "postern-"+commit[:7])
	if err := worktrees.Build(ctx, here, commit, "server", "cat main.txt > {out}", out); err != nil {
		t.Fatalf("building: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil || string(got) != "the backend\n" {
		t.Fatalf("expected the binary built at the landed commit in server/, got %q, %v", got, err)
	}
	if left, _ := filepath.Glob(out + "*"); len(left) != 1 {
		t.Errorf("expected only the binary beside itself, got %v", left)
	}
	if kept, _ := os.ReadFile(live); string(kept) != "the live binary\n" {
		t.Errorf("expected the live binary untouched, got %q", kept)
	}
	if marked, err := os.ReadFile(marks); err == nil {
		t.Errorf("expected neither install nor systemctl to be run, got %q", marked)
	}
	if listed := run(t, here, "git", "worktree", "list"); strings.Count(listed, "\n") != 0 {
		t.Errorf("expected the throwaway worktree gone, got\n%s", listed)
	}
	if siblings, _ := filepath.Glob(filepath.Join(filepath.Dir(here), rig.BackendWorktreePrefix+"*")); len(siblings) != 0 {
		t.Errorf("expected no directory left beside the rig, got %v", siblings)
	}

	// Building it again is not building it again: the binary stays as it is.
	if err := os.WriteFile(out, []byte("kept\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := worktrees.Build(ctx, here, commit, "server", "false", out); err != nil {
		t.Fatalf("expected a staged binary to be left alone, got %v", err)
	}
}

func TestAFailedBuildLeavesNothingStagedAndNothingBehind(t *testing.T) {
	here := aRigWithAServer(t)
	commit := run(t, here, "git", "rev-parse", "HEAD")
	out := filepath.Join(t.TempDir(), "postern-x")

	err := rig.New().Build(context.Background(), here, commit, "server", "echo half > {out}; echo the compiler said no; exit 3", out)
	if err == nil || !strings.Contains(err.Error(), "the compiler said no") {
		t.Fatalf("expected the failure with what the build printed, got %v", err)
	}
	if left, _ := filepath.Glob(out + "*"); len(left) != 0 {
		t.Errorf("expected nothing staged, got %v", left)
	}
	if listed := run(t, here, "git", "worktree", "list"); strings.Count(listed, "\n") != 0 {
		t.Errorf("expected the throwaway worktree gone, got\n%s", listed)
	}
}

// A commit the other host landed is not in this checkout until it is fetched.
func TestBuildFetchesACommitThisCheckoutHasNotSeen(t *testing.T) {
	here, other := aRig(t)
	write(t, other, "server/main.txt", "from the other host\n")
	run(t, other, "git", "add", "-A")
	run(t, other, "git", "commit", "-qm", "The other host's backend")
	run(t, other, "git", "push", "-q", "origin", "main")
	commit := run(t, other, "git", "rev-parse", "HEAD")

	out := filepath.Join(t.TempDir(), "postern-y")
	if err := rig.New().Build(context.Background(), here, commit, "server", "cat main.txt > {out}", out); err != nil {
		t.Fatalf("building: %v", err)
	}
	if got, _ := os.ReadFile(out); string(got) != "from the other host\n" {
		t.Fatalf("expected the other host's backend built, got %q", got)
	}
}
