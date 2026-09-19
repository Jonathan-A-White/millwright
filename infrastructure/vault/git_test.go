package vault_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// These tests drive real git against throwaway clones in a temp directory:
// a bare repository standing in for the remote both hosts share, and two
// clones standing in for the two hosts. Nothing reaches the network and
// nothing touches the factory's own vault.

// twoHosts makes a bare repository with one commit in it and two clones of it,
// and returns the directory of each clone. The vault they hold has a Mayor's
// ledger, because that is the file both hosts append to.
func twoHosts(t *testing.T) (here, there string) {
	t.Helper()
	if _, err := exec.LookPath(vault.Git); err != nil {
		t.Skipf("%s is not on PATH", vault.Git)
	}

	root := t.TempDir()
	remote := filepath.Join(root, "origin.git")
	run(t, root, "git", "init", "--bare", "-q", "-b", "main", remote)

	here = filepath.Join(root, "here")
	run(t, root, "git", "clone", "-q", remote, here)
	identify(t, here)
	write(t, here, "seats/mayor/ledger.md", "2026-09-17 the factory opened.\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The vault opens")
	run(t, here, "git", "push", "-q", "-u", "origin", "main")

	there = filepath.Join(root, "there")
	run(t, root, "git", "clone", "-q", remote, there)
	identify(t, there)
	return here, there
}

// identify gives a clone an author, so that committing in it works wherever
// these tests run.
func identify(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "config", "user.name", "millwright test")
	run(t, dir, "git", "config", "user.email", "test@millwright.invalid")
}

// run runs one command in a directory and fails the test if it does not
// succeed.
func run(t *testing.T, dir, program string, args ...string) string {
	t.Helper()
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s in %s: %v: %s", program, strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// write puts one file in a clone, making the directories above it.
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

// append adds a line to a clone's Mayor ledger and commits it.
func appendLedger(t *testing.T, dir, line string) {
	t.Helper()
	path := filepath.Join(dir, "seats/mayor/ledger.md")
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	write(t, dir, "seats/mayor/ledger.md", string(existing)+line+"\n")
	run(t, dir, "git", "commit", "-qam", line)
}

func TestMarkLedgersWritesTheMarkOnceAndLeavesAMarkedVaultAlone(t *testing.T) {
	here, _ := twoHosts(t)
	files := vault.New(here)

	marked, err := files.MarkLedgers(context.Background())
	if err != nil {
		t.Fatalf("marking the ledgers: %v", err)
	}
	if !marked {
		t.Fatal("expected an unmarked vault to be marked")
	}

	attributes, err := os.ReadFile(filepath.Join(here, vault.AttributesFile))
	if err != nil {
		t.Fatalf("reading the attributes: %v", err)
	}
	if !strings.Contains(string(attributes), application.LedgerMark) {
		t.Fatalf("expected %q in the attributes, got %q", application.LedgerMark, attributes)
	}

	marked, err = files.MarkLedgers(context.Background())
	if err != nil {
		t.Fatalf("marking the ledgers again: %v", err)
	}
	if marked {
		t.Fatal("expected a vault that is already marked to be left alone")
	}
	again, err := os.ReadFile(filepath.Join(here, vault.AttributesFile))
	if err != nil {
		t.Fatalf("reading the attributes again: %v", err)
	}
	if string(again) != string(attributes) {
		t.Fatalf("expected the attributes to be untouched, got %q", again)
	}
}

func TestMarkLedgersKeepsWhatTheAttributesAlreadySay(t *testing.T) {
	here, _ := twoHosts(t)
	write(t, here, vault.AttributesFile, "*.md text\n")

	if _, err := vault.New(here).MarkLedgers(context.Background()); err != nil {
		t.Fatalf("marking the ledgers: %v", err)
	}
	attributes, err := os.ReadFile(filepath.Join(here, vault.AttributesFile))
	if err != nil {
		t.Fatalf("reading the attributes: %v", err)
	}
	for _, want := range []string{"*.md text", application.LedgerMark} {
		if !strings.Contains(string(attributes), want) {
			t.Fatalf("expected %q in the attributes, got %q", want, attributes)
		}
	}
}

func TestMarkLedgersReadsAMarkWrittenAnyOtherWay(t *testing.T) {
	here, _ := twoHosts(t)
	write(t, here, vault.AttributesFile, "seats/*/ledger.md text merge=union  # as the Mayor wrote it\n")

	marked, err := vault.New(here).MarkLedgers(context.Background())
	if err != nil {
		t.Fatalf("marking the ledgers: %v", err)
	}
	if marked {
		t.Fatal("expected a ledger already merged by union to count as marked")
	}
}

func TestUncommittedListsChangedTrackedFilesAndIgnoresTheRest(t *testing.T) {
	here, _ := twoHosts(t)
	files := vault.New(here)

	changed, err := files.Uncommitted(context.Background())
	if err != nil {
		t.Fatalf("reading what is uncommitted: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("expected a clean vault, got %q", changed)
	}

	write(t, here, "runs/mw-gq6.11/boot.md", "a session's boot file, not committed by anyone\n")
	changed, err = files.Uncommitted(context.Background())
	if err != nil {
		t.Fatalf("reading what is uncommitted: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("expected untracked files to be in nobody's way, got %q", changed)
	}

	write(t, here, "seats/mayor/ledger.md", "2026-09-18 half a line")
	changed, err = files.Uncommitted(context.Background())
	if err != nil {
		t.Fatalf("reading what is uncommitted: %v", err)
	}
	if len(changed) != 1 || changed[0] != "seats/mayor/ledger.md" {
		t.Fatalf("expected the changed ledger, got %q", changed)
	}
}

// committed is the paths one commit touched, newest commit first argument.
func committed(t *testing.T, dir, revision string) []string {
	t.Helper()
	out := run(t, dir, "git", "show", "--pretty=format:", "--name-only", revision)
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			names = append(names, strings.TrimSpace(line))
		}
	}
	return names
}

func TestCommitRecordsThePathsItWasGivenAndLeavesEverythingElseAlone(t *testing.T) {
	here, _ := twoHosts(t)
	files := vault.New(here)

	// What a story leaves behind: mw's own ledger line and the session's memory
	// of the rig, both new, beside work of somebody else's that is none of a
	// close-out's business.
	write(t, here, "seats/builder/ledger.md", "| 2026-09-19 | mw-gq6.40 | landed on main |\n")
	write(t, here, "seats/builder/rigs/millwright.md", "- the rig's memory, as the session left it\n")
	write(t, here, "seats/mayor/ledger.md", "2026-09-17 the factory opened.\nsomebody else's line\n")
	write(t, here, "runs/mw-gq6.40/result.json", "{}\n")

	paths := []string{"seats/builder/ledger.md", "seats/builder/rigs/millwright.md"}
	recorded, err := files.Commit(context.Background(), "mw next: a story (mw-gq6.40)", paths)
	if err != nil {
		t.Fatalf("committing: %v", err)
	}
	if strings.Join(recorded, "\n") != strings.Join(paths, "\n") {
		t.Fatalf("expected %q to be committed, got %q", paths, recorded)
	}
	if held := committed(t, here, "HEAD"); strings.Join(held, "\n") != strings.Join(paths, "\n") {
		t.Fatalf("expected the commit to hold exactly %q, got %q", paths, held)
	}
	if message := strings.TrimSpace(run(t, here, "git", "log", "-1", "--format=%B")); message != "mw next: a story (mw-gq6.40)" {
		t.Fatalf("unexpected commit message %q", message)
	}

	left, err := files.Uncommitted(context.Background())
	if err != nil {
		t.Fatalf("reading what is uncommitted: %v", err)
	}
	if len(left) != 1 || left[0] != "seats/mayor/ledger.md" {
		t.Fatalf("expected somebody else's work to be left exactly where it was, got %q", left)
	}
}

// authorOf reads who a commit is recorded under, name and email.
func authorOf(t *testing.T, dir, revision string) string {
	t.Helper()
	return strings.TrimSpace(run(t, dir, "git", "log", "-1", "--format=%an <%ae>", revision))
}

func TestCommitIsAuthoredAsMwAndLeavesTheClonesOwnIdentityAlone(t *testing.T) {
	here, _ := twoHosts(t)
	files := vault.New(here, vault.WithAuthor("mw@laptop"))

	write(t, here, "seats/builder/ledger.md", "| 2026-09-19 | mw-gq6.42 | landed on main |\n")
	if _, err := files.Commit(context.Background(), "Close out mw-gq6.42: a story", []string{"seats/builder/ledger.md"}); err != nil {
		t.Fatalf("committing: %v", err)
	}
	if got := authorOf(t, here, "HEAD"); got != "mw@laptop <mw@laptop>" {
		t.Fatalf("expected mw's commit to be authored as mw@laptop, got %q", got)
	}
	if got := strings.TrimSpace(run(t, here, "git", "log", "-1", "--format=%cn <%ce>")); got != "mw@laptop <mw@laptop>" {
		t.Fatalf("expected mw's commit to be committed as mw@laptop, got %q", got)
	}

	// The clone's own identity was not rewritten to get there ...
	if got := strings.TrimSpace(run(t, here, "git", "config", "user.name")); got != "millwright test" {
		t.Fatalf("expected the clone's user.name to be untouched, got %q", got)
	}
	if got := strings.TrimSpace(run(t, here, "git", "config", "user.email")); got != "test@millwright.invalid" {
		t.Fatalf("expected the clone's user.email to be untouched, got %q", got)
	}

	// ... so a commit made by hand in the same clone is still the clone's.
	write(t, here, "seats/mayor/ledger.md", "2026-09-17 the factory opened.\nby hand\n")
	run(t, here, "git", "commit", "-qam", "By hand")
	if got := authorOf(t, here, "HEAD"); got != "millwright test <test@millwright.invalid>" {
		t.Fatalf("expected a commit by hand to keep the clone's author, got %q", got)
	}
}

func TestCommitWithNoAuthorGivenLeavesItToTheClone(t *testing.T) {
	here, _ := twoHosts(t)

	write(t, here, "seats/builder/ledger.md", "| 2026-09-19 | mw-gq6.42 | landed on main |\n")
	if _, err := vault.New(here).Commit(context.Background(), "Close out mw-gq6.42: a story", []string{"seats/builder/ledger.md"}); err != nil {
		t.Fatalf("committing: %v", err)
	}
	if got := authorOf(t, here, "HEAD"); got != "millwright test <test@millwright.invalid>" {
		t.Fatalf("expected the clone's author, got %q", got)
	}
}

func TestCommitDoesNotCarryOffWhatSomebodyElseStaged(t *testing.T) {
	here, _ := twoHosts(t)

	write(t, here, "seats/builder/ledger.md", "| 2026-09-19 | mw-gq6.40 | landed on main |\n")
	write(t, here, "seats/mayor/ledger.md", "2026-09-17 the factory opened.\nstaged by hand\n")
	run(t, here, "git", "add", "seats/mayor/ledger.md")

	if _, err := vault.New(here).Commit(context.Background(), "mw next: a story (mw-gq6.40)", []string{"seats/builder/ledger.md"}); err != nil {
		t.Fatalf("committing: %v", err)
	}
	if held := committed(t, here, "HEAD"); len(held) != 1 || held[0] != "seats/builder/ledger.md" {
		t.Fatalf("expected the commit to hold the ledger alone, got %q", held)
	}
}

func TestCommitMakesNoCommitWhenNothingChanged(t *testing.T) {
	here, _ := twoHosts(t)
	files := vault.New(here)
	paths := []string{"seats/builder/ledger.md", "seats/builder/rigs/millwright.md"}

	recorded, err := files.Commit(context.Background(), "mw next: a story (mw-gq6.40)", paths)
	if err != nil {
		t.Fatalf("committing a vault with none of those files in it: %v", err)
	}
	if len(recorded) != 0 {
		t.Fatalf("expected nothing to be committed, got %q", recorded)
	}
	head := run(t, here, "git", "rev-parse", "HEAD")

	write(t, here, "seats/builder/ledger.md", "| 2026-09-19 | mw-gq6.40 | landed on main |\n")
	if _, err := files.Commit(context.Background(), "mw next: a story (mw-gq6.40)", paths); err != nil {
		t.Fatalf("committing: %v", err)
	}
	recorded, err = files.Commit(context.Background(), "mw next: a story (mw-gq6.40)", paths)
	if err != nil {
		t.Fatalf("committing again with nothing changed: %v", err)
	}
	if len(recorded) != 0 {
		t.Fatalf("expected a second commit of unchanged files to make none, got %q", recorded)
	}
	if now := run(t, here, "git", "rev-parse", "HEAD"); now == head {
		t.Fatal("expected the one real commit to have been made")
	}
	if count := strings.TrimSpace(run(t, here, "git", "rev-list", "--count", "HEAD")); count != "2" {
		t.Fatalf("expected exactly one commit on top of the vault's first, got %s", count)
	}
}

func TestCommitRefusesAPathThatWouldReachOutsideTheVault(t *testing.T) {
	here, _ := twoHosts(t)
	head := run(t, here, "git", "rev-parse", "HEAD")

	for _, reach := range []string{"../elsewhere.md", "/etc/passwd", "seats/../../out.md"} {
		if _, err := vault.New(here).Commit(context.Background(), "mw next: a story (mw-gq6.40)", []string{reach}); err == nil {
			t.Fatalf("expected %q to be refused", reach)
		}
	}
	if _, err := vault.New(here).Commit(context.Background(), "  ", []string{"seats/mayor/ledger.md"}); err == nil {
		t.Fatal("expected a commit with no message to be refused")
	}
	if now := run(t, here, "git", "rev-parse", "HEAD"); now != head {
		t.Fatalf("expected nothing to have been committed, HEAD went from %s to %s", head, now)
	}
}

func TestPullAndPushMoveOnlyWhatIsThere(t *testing.T) {
	here, there := twoHosts(t)
	files := vault.New(here)

	pulled, err := files.Pull(context.Background())
	if err != nil {
		t.Fatalf("pulling: %v", err)
	}
	pushed, err := files.Push(context.Background())
	if err != nil {
		t.Fatalf("pushing: %v", err)
	}
	if pulled != 0 || pushed != 0 {
		t.Fatalf("expected a level vault to move nothing, pulled %d and pushed %d", pulled, pushed)
	}

	appendLedger(t, there, "2026-09-18 the other host worked mw-gq6.9.")
	run(t, there, "git", "push", "-q")
	appendLedger(t, here, "2026-09-18 this host worked mw-gq6.11.")

	if _, err := files.MarkLedgers(context.Background()); err != nil {
		t.Fatalf("marking the ledgers: %v", err)
	}
	if pulled, err = files.Pull(context.Background()); err != nil {
		t.Fatalf("pulling: %v", err)
	}
	if pushed, err = files.Push(context.Background()); err != nil {
		t.Fatalf("pushing: %v", err)
	}
	if pulled != 1 || pushed != 1 {
		t.Fatalf("expected one commit each way, pulled %d and pushed %d", pulled, pushed)
	}

	ledger, err := os.ReadFile(filepath.Join(here, "seats/mayor/ledger.md"))
	if err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	for _, want := range []string{"mw-gq6.9", "mw-gq6.11"} {
		if !strings.Contains(string(ledger), want) {
			t.Fatalf("expected both hosts' lines in the ledger, %q is missing from %q", want, ledger)
		}
	}
}

func TestPullUndoesARebaseItCannotFinish(t *testing.T) {
	here, there := twoHosts(t)
	files := vault.New(here)

	// The same line of the same file written differently on both hosts is a
	// real conflict, mark or no mark: the vault must come back untouched.
	write(t, here, "seats/mayor/charter.md", "the charter as this host has it\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "This host's charter")
	run(t, here, "git", "push", "-q")
	run(t, there, "git", "pull", "-q", "--rebase")

	write(t, there, "seats/mayor/charter.md", "the charter as the other host has it\n")
	run(t, there, "git", "commit", "-qam", "The other host's charter")
	run(t, there, "git", "push", "-q")

	write(t, here, "seats/mayor/charter.md", "the charter as this host changed it\n")
	run(t, here, "git", "commit", "-qam", "This host changes the charter")
	head := run(t, here, "git", "rev-parse", "HEAD")

	if _, err := files.Pull(context.Background()); err == nil {
		t.Fatal("expected a real conflict to stop the pull")
	}
	if now := run(t, here, "git", "rev-parse", "HEAD"); now != head {
		t.Fatalf("expected the vault to be where it was, HEAD went from %s to %s", head, now)
	}
	if left := run(t, here, "git", "status", "--porcelain"); strings.TrimSpace(left) != "" {
		t.Fatalf("expected no half-finished rebase left behind, got %q", left)
	}
}

func TestPullSaysSoWhenTheBranchTracksNothing(t *testing.T) {
	if _, err := exec.LookPath(vault.Git); err != nil {
		t.Skipf("%s is not on PATH", vault.Git)
	}
	alone := t.TempDir()
	run(t, alone, "git", "init", "-q", "-b", "main", ".")
	identify(t, alone)
	write(t, alone, "seats/mayor/ledger.md", "alone\n")
	run(t, alone, "git", "add", "-A")
	run(t, alone, "git", "commit", "-qm", "Alone")

	_, err := vault.New(alone).Pull(context.Background())
	if err == nil {
		t.Fatal("expected a vault tracking nothing to have nothing to sync with")
	}
	if !strings.Contains(err.Error(), "tracks no remote branch") {
		t.Fatalf("expected the reason to say the branch tracks nothing, got %v", err)
	}
}
