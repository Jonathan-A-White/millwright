package rig_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// These tests drive real git against throwaway repositories in a temp
// directory: a bare repository standing in for the rig's origin, a clone
// standing in for the other host, and the checkout this host would dispatch
// from. Nothing reaches the network and nothing touches the factory's rigs.

// aRig makes the bare origin, the other host's clone and this host's checkout,
// and returns the last two.
func aRig(t *testing.T) (here, other string) {
	t.Helper()
	if !rig.Available() {
		t.Skipf("%s is not on PATH", rig.Program)
	}

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	run(t, root, "git", "init", "--bare", "-q", "-b", "main", origin)

	other = filepath.Join(root, "other")
	run(t, root, "git", "clone", "-q", origin, other)
	identify(t, other)
	write(t, other, "README.md", "# the rig\n")
	run(t, other, "git", "add", "-A")
	run(t, other, "git", "commit", "-qm", "The rig opens")
	run(t, other, "git", "push", "-q", "-u", "origin", "main")

	here = filepath.Join(root, "rigs", "millwright")
	run(t, root, "git", "clone", "-q", origin, here)
	identify(t, here)
	return here, other
}

func run(t *testing.T, dir, program string, args ...string) string {
	t.Helper()
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s in %s: %v: %s", program, strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func identify(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "config", "user.name", "millwright test")
	run(t, dir, "git", "config", "user.email", "test@millwright.invalid")
}

func write(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestAWorktreeIsCutFromWhatTheOtherHostHasPushed(t *testing.T) {
	here, other := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	// The other host pushes while this host is not looking.
	write(t, other, "later.md", "the other host pushed this\n")
	run(t, other, "git", "add", "-A")
	run(t, other, "git", "commit", "-qm", "A later commit")
	run(t, other, "git", "push", "-q", "origin", "main")

	dir := application.WorktreeDir(here, "mw-gq6.7")
	branch := application.StoryBranch("mw-gq6.7")
	start := application.StartPoint(worktrees.Remote(), "main")

	// Without a fetch, this host's origin/main is the older commit.
	if err := worktrees.Fetch(ctx, here); err != nil {
		t.Fatalf("fetching: %v", err)
	}
	if err := worktrees.Add(ctx, here, dir, branch, start); err != nil {
		t.Fatalf("cutting the worktree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "later.md")); err != nil {
		t.Fatalf("expected the worktree to hold what the other host pushed: %v", err)
	}
	if on := run(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"); on != branch {
		t.Fatalf("expected the worktree to be on %q, got %q", branch, on)
	}
	if dir != filepath.Join(filepath.Dir(here), application.WorktreesDir, "mw-gq6.7") {
		t.Fatalf("expected the worktree beside the rig, got %q", dir)
	}

	// A second worktree of the same story is refused: a story's worktree is its
	// own, and reusing one would hide work nobody looked at.
	if err := worktrees.Add(ctx, here, dir, branch, start); err == nil {
		t.Fatal("expected cutting the same worktree twice to fail")
	}

	// Removing takes both the worktree and its branch away.
	if err := worktrees.Remove(ctx, here, dir, branch); err != nil {
		t.Fatalf("removing the worktree: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("expected %s to be gone", dir)
	}
	if branches := run(t, here, "git", "branch", "--list", branch); branches != "" {
		t.Fatalf("expected the branch %s to be gone, got %q", branch, branches)
	}

	// And removing what is not there is how a failed dispatch tidies up.
	if err := worktrees.Remove(ctx, here, dir, branch); err != nil {
		t.Fatalf("expected removing nothing to be harmless, got %v", err)
	}
}

func TestAWorktreeNeedsSomewhereToBeCutFrom(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	if err := worktrees.Add(ctx, here, filepath.Join(here, "..", "nowhere"), "mw/x", ""); err == nil {
		t.Fatal("expected a worktree with no start point to be refused")
	}
	if err := worktrees.Add(ctx, here, "", "mw/x", "origin/main"); err == nil {
		t.Fatal("expected a worktree with no directory to be refused")
	}
	if err := worktrees.Add(ctx, here, filepath.Join(here, "..", "nowhere"), "mw/x", "origin/no-such-branch"); err == nil {
		t.Fatal("expected a worktree cut from a branch that is not there to be refused")
	}
}

func TestAddNoteWritesTheTxidAsANoteOnTheCommitUnderRefsNotesChain(t *testing.T) {
	here, _ := aRig(t)
	commit := run(t, here, "git", "rev-parse", "HEAD")
	w := rig.New()

	if err := w.AddNote(context.Background(), here, "chain", commit, "txid-one"); err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if got := run(t, here, "git", "notes", "--ref=chain", "show", commit); got != "txid-one" {
		t.Fatalf("note = %q, want txid-one", got)
	}
	if got := run(t, here, "git", "rev-parse", "--verify", "refs/notes/chain"); got == "" {
		t.Fatal("refs/notes/chain is not there")
	}

	// A second note replaces the first: -f.
	if err := w.AddNote(context.Background(), here, "chain", commit, "txid-two"); err != nil {
		t.Fatalf("AddNote again: %v", err)
	}
	if got := run(t, here, "git", "notes", "--ref=chain", "show", commit); got != "txid-two" {
		t.Fatalf("note = %q, want txid-two", got)
	}
}

func TestAddNoteOnARevisionThatIsNotThereFails(t *testing.T) {
	here, _ := aRig(t)
	if err := rig.New().AddNote(context.Background(), here, "chain", "no-such-commit", "txid"); err == nil {
		t.Fatal("a note on a missing commit was written")
	}
}

func TestResolveReadsTheHeadOfTheRemotesDefaultBranchAfterAFetch(t *testing.T) {
	here, other := aRig(t)
	ctx := context.Background()

	// The other host pushes while this host is not looking: Resolve fetches.
	write(t, other, "later.md", "pushed later\n")
	run(t, other, "git", "add", "-A")
	run(t, other, "git", "commit", "-qm", "A later commit")
	run(t, other, "git", "push", "-q", "origin", "main")
	want := run(t, other, "git", "rev-parse", "HEAD")

	got, err := rig.New().Resolve(ctx, here, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Commit != want || got.Branch != "main" || got.Subject != "A later commit" {
		t.Fatalf("Resolve = %+v, want commit %s on main, subject %q", got, want, "A later commit")
	}
}

func TestResolveFallsBackToTheRemotesWordWhenTheCloneHasNoOriginHead(t *testing.T) {
	here, _ := aRig(t)
	run(t, here, "git", "remote", "set-head", "origin", "-d")
	want := run(t, here, "git", "rev-parse", "origin/main")

	got, err := rig.New().Resolve(context.Background(), here, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Commit != want || got.Branch != "main" {
		t.Fatalf("Resolve = %+v, want commit %s on main", got, want)
	}
}

func TestResolveExpandsACommitNamedByItsFirstCharacters(t *testing.T) {
	here, _ := aRig(t)
	full := run(t, here, "git", "rev-parse", "origin/main")

	got, err := rig.New().Resolve(context.Background(), here, full[:8])
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Commit != full || got.Subject != "The rig opens" {
		t.Fatalf("Resolve = %+v, want commit %s, subject %q", got, full, "The rig opens")
	}
	if _, err := rig.New().Resolve(context.Background(), here, "deadbeef"); err == nil {
		t.Fatal("Resolve of a commit that is not there succeeded")
	}
}
