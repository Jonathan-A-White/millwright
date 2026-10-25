package rig_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestCommitOfReadsACommitAsTheCheckoutHasItWithWhenItWasCommitted(t *testing.T) {
	here, _ := aRig(t)
	write(t, here, "NOTES.md", "a note\n")
	run(t, here, "git", "add", "-A")
	cmd := exec.Command("git", "commit", "-qm", "Merge branch 'mw/mw-x.1' into HEAD")
	cmd.Dir = here
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE=2026-09-20T15:04:00Z")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("committing: %v: %s", err, out)
	}
	full := run(t, here, "git", "rev-parse", "HEAD")

	got, err := rig.New().CommitOf(context.Background(), here, full[:7])
	if err != nil {
		t.Fatalf("CommitOf: %v", err)
	}
	if got.Commit != full || got.Subject != "Merge branch 'mw/mw-x.1' into HEAD" || !got.At.Equal(time.Date(2026, 9, 20, 15, 4, 0, 0, time.UTC)) {
		t.Fatalf("CommitOf = %+v, want commit %s, the merge subject, at 2026-09-20T15:04:00Z", got, full)
	}
	if _, err := rig.New().CommitOf(context.Background(), here, "deadbeef"); err == nil {
		t.Fatal("CommitOf of a commit that is not there succeeded")
	}
}

func TestChangedFilesAreWhatTheStoryBranchChangedSinceItsMergeBase(t *testing.T) {
	here := aRigWithAServer(t)
	ctx := context.Background()
	worktrees := rig.New()

	run(t, here, "git", "checkout", "-q", "-b", "mw/story")
	write(t, here, "src/app.txt", "a newer app\n")
	write(t, here, "src/Reader.tsx", "a reader\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The story's change")

	// What the target gained meanwhile is not the story's change.
	run(t, here, "git", "checkout", "-q", "main")
	write(t, here, "server/other.txt", "another story's route\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Another story landed")

	got, err := worktrees.ChangedFiles(ctx, here, "main", "mw/story")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"src/Reader.tsx", "src/app.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestABranchThatIsNotThereHasChangedNothing(t *testing.T) {
	here := aRigWithAServer(t)
	got, err := rig.New().ChangedFiles(context.Background(), here, "main", "mw/worked-elsewhere")
	if err != nil || len(got) != 0 {
		t.Errorf("expected nothing changed and no error, got %q, %v", got, err)
	}
}

func TestABranchWithNoCommitsYetHasChangedNothing(t *testing.T) {
	here := aRigWithAServer(t)
	run(t, here, "git", "branch", "mw/just-cut", "main")
	got, err := rig.New().ChangedFiles(context.Background(), here, "main", "mw/just-cut")
	if err != nil || len(got) != 0 {
		t.Errorf("expected nothing changed and no error, got %q, %v", got, err)
	}
}

func TestAddedFilesAreTheNewFilesOfTheBranchWithTheirSizeAndWhetherTheyAreExecutables(t *testing.T) {
	here := aRigWithAServer(t)
	run(t, here, "git", "checkout", "-q", "-b", "mw/story")
	write(t, here, "src/app.txt", "a newer app, which is not added\n")
	write(t, here, "notes/new.md", "new notes\n")
	write(t, here, "mw", "\x7fELF"+strings.Repeat("\x00", 60))
	write(t, here, "tools/mw", "\x7fELF"+strings.Repeat("\x00", 60))
	write(t, here, "build.sh", "#!/bin/sh\necho hello\n")
	write(t, here, "dump.bin", strings.Repeat("\x00", 3000))
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The story's change")

	got, err := rig.New().AddedFiles(context.Background(), here, "main", "mw/story")
	if err != nil {
		t.Fatal(err)
	}
	want := []application.AddedFile{
		{Path: "build.sh", Size: 21},
		{Path: "dump.bin", Size: 3000},
		{Path: "mw", Size: 64, Executable: true},
		{Path: "notes/new.md", Size: 10},
		{Path: "tools/mw", Size: 64},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

func TestABranchThatIsNotThereHasAddedNothing(t *testing.T) {
	here := aRigWithAServer(t)
	got, err := rig.New().AddedFiles(context.Background(), here, "main", "mw/worked-elsewhere")
	if err != nil || len(got) != 0 {
		t.Errorf("expected nothing added and no error, got %+v, %v", got, err)
	}
}

func TestAFileOfTheBranchIsReadAsTheBranchHasIt(t *testing.T) {
	here := aRigWithAServer(t)
	ctx := context.Background()
	run(t, here, "git", "checkout", "-q", "-b", "mw/story")
	write(t, here, "grinds/maker-request.json", `{"forward":"mayor"}`)
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "A grind")
	run(t, here, "git", "checkout", "-q", "main")

	got, found, err := rig.New().ReadBranchFile(ctx, here, "mw/story", "grinds/maker-request.json")
	if err != nil || !found || string(got) != `{"forward":"mayor"}` {
		t.Errorf("expected the branch's file, got %q, %v, %v", got, found, err)
	}
	if _, found, err := rig.New().ReadBranchFile(ctx, here, "mw/story", "grinds/none.json"); found || err != nil {
		t.Errorf("expected a missing file not found without error, got %v, %v", found, err)
	}
	if _, found, err := rig.New().ReadBranchFile(ctx, here, "mw/worked-elsewhere", "grinds/maker-request.json"); found || err != nil {
		t.Errorf("expected a missing branch not found without error, got %v, %v", found, err)
	}
}
