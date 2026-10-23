package vault_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
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

// A close-out writes a run's files into the vault and commits them a moment
// later. A pull that lands between the two meets the files untracked, in the
// way of the same files coming from the other host, and git refuses it. The
// close-out's commit is only a moment behind, so the pull waits and tries once
// more, and finds the vault level.
func TestPullWaitsOutACloseOutThatIsWritingTheFilesTheOtherHostSent(t *testing.T) {
	here, there := twoHosts(t)

	write(t, there, "runs/mw-x.1/result.json", "{}\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "The other host's run files")
	run(t, there, "git", "push", "-q")

	// The close-out has written the files and has not yet committed them.
	write(t, here, "runs/mw-x.1/result.json", "{}\n")

	var waited []time.Duration
	files := vault.New(here, vault.WithWait(func(_ context.Context, d time.Duration) error {
		waited = append(waited, d)
		// The close-out commits while the pull waits.
		run(t, here, "git", "add", "-A")
		run(t, here, "git", "commit", "-qm", "Close-out of mw-x.1")
		return nil
	}))

	if _, err := files.Pull(context.Background()); err != nil {
		t.Fatalf("expected the pull to wait out the close-out and finish, got %v", err)
	}
	if len(waited) != 1 {
		t.Fatalf("expected one wait before the one retry, got %v", waited)
	}
	if left := run(t, here, "git", "status", "--porcelain"); strings.TrimSpace(left) != "" {
		t.Fatalf("expected a level vault, got %q", left)
	}
}

// The wait is for that one refusal and no other: a pull that git refuses for
// any other reason fails at once, as it always did.
func TestPullDoesNotWaitOutAnyOtherRefusal(t *testing.T) {
	fakeGit(t, "fatal: refusing to merge unrelated histories")

	waited := 0
	files := vault.New(t.TempDir(), vault.WithWait(func(context.Context, time.Duration) error {
		waited++
		return nil
	}))
	if _, err := files.Pull(context.Background()); err == nil {
		t.Fatal("expected the refusal to be reported")
	}
	if waited != 0 {
		t.Fatalf("expected no wait for another refusal, waited %d times", waited)
	}
}

// A close-out that never commits does not hold the pull for ever: it is tried
// once more, and the second refusal comes back as it was said.
func TestPullFailsWhenTheFilesAreStillInTheWayAfterTheOneRetry(t *testing.T) {
	here, there := twoHosts(t)

	write(t, there, "runs/mw-x.1/result.json", "{}\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "The other host's run files")
	run(t, there, "git", "push", "-q")
	write(t, here, "runs/mw-x.1/result.json", "{}\n")

	waited := 0
	files := vault.New(here, vault.WithWait(func(context.Context, time.Duration) error {
		waited++
		return nil
	}))
	_, err := files.Pull(context.Background())
	if err == nil || !strings.Contains(err.Error(), "untracked working tree files would be overwritten") {
		t.Fatalf("expected git's refusal after the retry, got %v", err)
	}
	if waited != 1 {
		t.Fatalf("expected exactly one wait, waited %d times", waited)
	}
}

// commitDuringFetch installs a hook in a clone that, once, makes a commit of
// change to a tracked file when a fetch updates the remote-tracking branch: a
// Clerk's `mw memory` committing in the vault at the moment mw is pulling it
// (mw-gq6.713).
func commitDuringFetch(t *testing.T, dir, file string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE\n" +
		"[ \"$1\" = committed ] || exit 0\n" +
		"grep -q refs/remotes/origin/main || exit 0\n" +
		"[ -e '" + dir + "/.git/raced' ] && exit 0\n" +
		"touch '" + dir + "/.git/raced'\n" +
		"cd '" + dir + "' && echo clerk >> " + file + " && git commit -qm 'A Clerk commits' -- " + file + "\n"
	hook := filepath.Join(dir, ".git", "hooks", "reference-transaction")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatalf("making the hooks directory: %v", err)
	}
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the hook: %v", err)
	}
}

// A commit that lands in this vault while the pull fetches is not a reason to
// fail: `git pull --rebase` notices that the fetch "updated the current branch
// head", tries to fast-forward and dies with "Not possible to fast-forward"
// when the local commit diverges. The pull rebases the local commit onto the
// fetched head, and the push after it carries the commit out (mw-gq6.713).
func TestPullRebasesACommitThatLandsLocallyWhileItFetches(t *testing.T) {
	here, there := twoHosts(t)
	write(t, there, "seats/mayor/other.md", "the other host\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "The other host writes")
	run(t, there, "git", "push", "-q")
	commitDuringFetch(t, here, "seats/mayor/ledger.md")

	files := vault.New(here)
	pulled, err := files.Pull(context.Background())

	if err != nil {
		t.Fatalf("expected the local commit to be rebased onto the fetched head, got %v", err)
	}
	if pulled != 1 {
		t.Fatalf("expected the other host's one commit to come in, got %d", pulled)
	}
	if _, err := os.Stat(filepath.Join(here, "seats/mayor/other.md")); err != nil {
		t.Fatalf("expected the other host's file after the pull: %v", err)
	}
	if ledger, err := os.ReadFile(filepath.Join(here, "seats/mayor/ledger.md")); err != nil || !strings.Contains(string(ledger), "clerk") {
		t.Fatalf("expected the Clerk's line in the ledger, got %q (%v)", ledger, err)
	}
	pushed, err := files.Push(context.Background())
	if err != nil || pushed != 1 {
		t.Fatalf("expected the Clerk's commit to be pushed, pushed %d, err %v", pushed, err)
	}
	run(t, there, "git", "pull", "-q", "--rebase")
	if ledger, err := os.ReadFile(filepath.Join(there, "seats/mayor/ledger.md")); err != nil || !strings.Contains(string(ledger), "clerk") {
		t.Fatalf("expected the other host to have the Clerk's line, got %q (%v)", ledger, err)
	}
}

// A rebase that git cannot start because another git holds the index lock (a
// Clerk's commit in the same moment) is waited out once and tried again.
func TestPullWaitsOnceForAnIndexLockAndTriesAgain(t *testing.T) {
	here, there := twoHosts(t)
	write(t, there, "seats/mayor/other.md", "the other host\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "The other host writes")
	run(t, there, "git", "push", "-q")

	lock := filepath.Join(here, ".git", "index.lock")
	write(t, here, ".git/index.lock", "")
	var waited []time.Duration
	files := vault.New(here, vault.WithWait(func(_ context.Context, d time.Duration) error {
		waited = append(waited, d)
		return os.Remove(lock)
	}))
	if _, err := files.Pull(context.Background()); err != nil {
		t.Fatalf("expected the pull to finish once the lock went, got %v", err)
	}
	if len(waited) != 1 {
		t.Fatalf("expected one wait before the one retry, got %v", waited)
	}
}

// The retry is one: a lock that is still held after the wait is the pull's
// failure, and the vault is left as it was.
func TestPullFailsWhenTheIndexLockIsStillHeldAfterTheOneRetry(t *testing.T) {
	here, there := twoHosts(t)
	write(t, there, "seats/mayor/other.md", "the other host\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "The other host writes")
	run(t, there, "git", "push", "-q")

	write(t, here, ".git/index.lock", "")
	waited := 0
	files := vault.New(here, vault.WithWait(func(context.Context, time.Duration) error {
		waited++
		return nil
	}))
	if _, err := files.Pull(context.Background()); err == nil {
		t.Fatal("expected a lock that stays to fail the pull")
	}
	if waited != 1 {
		t.Fatalf("expected exactly one wait, waited %d times", waited)
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

// fakeGit puts a `git` that says one thing on stderr and exits 128 first on the
// PATH, so that the vault meets a failure the way a host that has just woken up
// does, with no network to reach and no real name to look up.
func fakeGit(t *testing.T, says string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho '" + says + "' >&2\nexit 128\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for git: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestAFetchThatCannotResolveTheRemoteIsANameThatCouldNotBeResolved(t *testing.T) {
	fakeGit(t, "ssh: Could not resolve hostname github.com: Temporary failure in name resolution")

	_, err := vault.New(t.TempDir()).Push(context.Background())
	unresolved, ok := application.Unresolved(err)
	if !ok {
		t.Fatalf("expected a name that could not be resolved, got %T: %v", err, err)
	}
	if !strings.Contains(unresolved.Said, "Could not resolve hostname github.com") {
		t.Fatalf("expected the line git said, got %q", unresolved.Said)
	}
}

func TestAGitThatCannotConnectToTheRemoteIsARemoteThatCouldNotBeReached(t *testing.T) {
	fakeGit(t, "fatal: unable to access https://github.com/x/y/: Failed to connect to github.com port 443 after 21000 ms: Connection timed out")

	_, err := vault.New(t.TempDir()).Push(context.Background())
	gone, ok := application.Unreachable(err)
	if !ok {
		t.Fatalf("expected a remote that could not be reached, got %T: %v", err, err)
	}
	if gone.Remote != "github.com" || !strings.Contains(gone.Said, "Failed to connect to github.com port 443") {
		t.Fatalf("expected github.com and the line git said, got %q and %q", gone.Remote, gone.Said)
	}
	if _, resolved := application.Unresolved(err); resolved {
		t.Fatalf("expected a refused connection not to be a name that could not be resolved")
	}
}

func TestARefusedKeyIsNotARemoteThatCouldNotBeReached(t *testing.T) {
	fakeGit(t, "git@github.com: Permission denied (publickey).")

	_, err := vault.New(t.TempDir()).Push(context.Background())
	if _, ok := application.Unreachable(err); ok {
		t.Fatalf("expected a refused key not to be an unreachable remote, got %v", err)
	}
}

func TestAnyOtherGitFailureIsLeftExactlyAsGitSaidIt(t *testing.T) {
	fakeGit(t, "git@github.com: Permission denied (publickey).")

	_, err := vault.New(t.TempDir()).Push(context.Background())
	if err == nil {
		t.Fatal("expected the failure to be reported")
	}
	if _, ok := application.Unresolved(err); ok {
		t.Fatalf("expected a refused key not to be a name that could not be resolved, got %v", err)
	}
	if !strings.Contains(err.Error(), "Permission denied (publickey)") {
		t.Fatalf("expected git's own words, got %v", err)
	}
}

func TestBranchAndHeadSubjectNameTheCheckedOutBranchAndItsHeadsFirstLine(t *testing.T) {
	here, _ := twoHosts(t)
	files := vault.New(here)
	write(t, here, "notes.md", "a note\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Add a note", "-m", "and a body line")

	branch, err := files.Branch(context.Background())
	if err != nil || branch != "main" {
		t.Fatalf("expected branch main, got %q, %v", branch, err)
	}
	subject, err := files.HeadSubject(context.Background())
	if err != nil || subject != "Add a note" {
		t.Fatalf("expected the head's subject, got %q, %v", subject, err)
	}
}

func TestLastSignIsTheNewestCommitAuthoredAsThatHostAndNoOtherHosts(t *testing.T) {
	here, _ := twoHosts(t)
	ctx := context.Background()

	commitAs := func(author, name string) {
		t.Helper()
		write(t, here, name, name+"\n")
		if _, err := vault.New(here, vault.WithAuthor(author)).Commit(ctx, "Close out "+name, []string{name}); err != nil {
			t.Fatalf("committing as %s: %v", author, err)
		}
	}
	commitAs("mw@cloud2", "runs/mw-gq6.1/report.md")
	commitAs("mw@cloud22", "runs/mw-gq6.2/report.md")

	sign, err := vault.New(here).LastSign(ctx, "cloud2")
	if err != nil {
		t.Fatalf("reading the sign of cloud2: %v", err)
	}
	if sign.What != vault.SignOfLifeCommit || sign.At.IsZero() || time.Since(sign.At) > time.Minute {
		t.Fatalf("expected a vault commit made just now, got %+v", sign)
	}

	// mw@cloud is a prefix of both of them, and has committed nothing.
	if sign, err := vault.New(here).LastSign(ctx, "cloud"); err != nil || !sign.At.IsZero() {
		t.Fatalf("expected no sign of cloud, got %+v, %v", sign, err)
	}
}

// syncOnce runs one real sync of a clone with no work tracker beyond the fake.
func syncOnce(t *testing.T, dir string) application.SyncReport {
	t.Helper()
	report, err := application.Sync{
		Vault:   vault.New(dir),
		Tracker: apptest.NewFakeTracker(),
		Host:    "cloud3",
		Now:     func() time.Time { return time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC) },
		Sleep:   func(context.Context, time.Duration) error { return nil },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing %s: %v", dir, err)
	}
	return report
}

func TestSyncCommitsTheLedgerMarkItWritesIntoAFreshClone(t *testing.T) {
	for name, before := range map[string]string{
		"a clone with no attributes yet":         "",
		"a clone whose attributes carry no mark": "*.md text\n",
	} {
		t.Run(name, func(t *testing.T) {
			here, there := twoHosts(t)
			if before != "" {
				write(t, there, vault.AttributesFile, before)
				run(t, there, "git", "add", "-A")
				run(t, there, "git", "commit", "-qm", "Attributes without the mark")
				run(t, there, "git", "push", "-q")
				run(t, here, "git", "pull", "-q", "--rebase")
			}

			if report := syncOnce(t, here); !report.Marked {
				t.Fatalf("expected the first sync to write the mark, got %+v", report)
			}
			if status := run(t, here, "git", "status", "--porcelain", "--untracked-files=no"); strings.TrimSpace(status) != "" {
				t.Fatalf("expected a clean vault after one sync, got %q", status)
			}
			if committed := run(t, here, "git", "show", "HEAD:"+vault.AttributesFile); !strings.Contains(committed, application.LedgerMark) {
				t.Fatalf("expected the mark to be committed, HEAD has %q", committed)
			}
			head := run(t, here, "git", "rev-parse", "HEAD")

			if report := syncOnce(t, here); report.Marked || report.Blocked != nil {
				t.Fatalf("expected the second sync to write nothing and not be blocked, got %+v", report)
			}
			if again := run(t, here, "git", "rev-parse", "HEAD"); again != head {
				t.Fatalf("expected the second sync to commit nothing, HEAD moved from %s to %s", head, again)
			}
		})
	}
}

// A commit that meets another git's index.lock (a batch landing at the same
// moment) waits for it and commits; it does not give up at once and leave its
// files staged (mw-gq6.554).
func TestCommitWaitsOutAnotherGitsIndexLockAndCommitsThePaths(t *testing.T) {
	here, _ := twoHosts(t)
	lock := filepath.Join(here, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	released := time.AfterFunc(2*time.Second, func() { os.Remove(lock) })
	defer released.Stop()

	write(t, here, "runs/mw-gq6.554/result.json", "{}\n")
	paths := []string{"runs/mw-gq6.554/result.json"}
	recorded, err := vault.New(here).Commit(context.Background(), "Close out mw-gq6.554", paths)
	if err != nil {
		t.Fatalf("expected the commit to wait for the lock and finish, got %v", err)
	}
	if strings.Join(recorded, "\n") != strings.Join(paths, "\n") {
		t.Fatalf("expected %q to be committed, got %q", paths, recorded)
	}
	if held := committed(t, here, "HEAD"); strings.Join(held, "\n") != strings.Join(paths, "\n") {
		t.Fatalf("expected the commit to hold %q, got %q", paths, held)
	}
	if left := run(t, here, "git", "status", "--porcelain"); strings.TrimSpace(left) != "" {
		t.Fatalf("expected nothing left staged, got %q", left)
	}
}

// A lock held past the bound is a failure, and it says what git said.
func TestCommitFailsOnceAnIndexLockOutlastsTheBound(t *testing.T) {
	here, _ := twoHosts(t)
	if err := os.WriteFile(filepath.Join(here, ".git", "index.lock"), nil, 0o644); err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	write(t, here, "runs/mw-gq6.554/result.json", "{}\n")

	files := vault.New(here, vault.WithLockWait(500*time.Millisecond, 50*time.Millisecond))
	start := time.Now()
	_, err := files.Commit(context.Background(), "Close out mw-gq6.554", []string{"runs/mw-gq6.554/result.json"})
	if err == nil || !strings.Contains(err.Error(), "index.lock") {
		t.Fatalf("expected the commit to fail naming the lock, got %v", err)
	}
	if took := time.Since(start); took < 400*time.Millisecond {
		t.Fatalf("expected the commit to wait out its bound before failing, it took %s", took)
	}
}

// Any other git failure is not waited for.
func TestCommitDoesNotWaitOutAnyOtherFailure(t *testing.T) {
	here, _ := twoHosts(t)
	write(t, here, "runs/mw-gq6.554/result.json", "{}\n")
	files := vault.New(here, vault.WithLockWait(time.Minute, time.Minute))
	start := time.Now()
	_, err := files.Commit(context.Background(), " ", []string{"runs/mw-gq6.554/result.json"})
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("expected an immediate failure, got %v after %s", err, time.Since(start))
	}
}

// A push the remote refuses because the other host got there first is rebased
// onto what came in and tried again (mw-gq6.583).
func TestPushRebasesOntoWhatTheRemoteGotMeanwhileAndTriesAgain(t *testing.T) {
	here, there := twoHosts(t)
	files := vault.New(here)

	write(t, there, "seats/builder/other.md", "the other host's file\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "The other host writes")
	run(t, there, "git", "push", "-q")

	write(t, here, "seats/builder/mine.md", "this host's file\n")
	if _, err := files.Commit(context.Background(), "this host writes", []string{"seats/builder/mine.md"}); err != nil {
		t.Fatalf("committing: %v", err)
	}
	pushed, err := files.Push(context.Background())
	if err != nil {
		t.Fatalf("pushing after the remote moved: %v", err)
	}
	if pushed != 1 {
		t.Fatalf("expected the one commit pushed, got %d", pushed)
	}
	remote := strings.TrimSpace(run(t, here, "git", "rev-parse", "origin/main"))
	if head := strings.TrimSpace(run(t, here, "git", "rev-parse", "HEAD")); remote != head {
		t.Fatalf("the remote is at %s, the clone at %s", remote, head)
	}
	run(t, there, "git", "pull", "-q", "--rebase")
	for _, name := range []string{"seats/builder/mine.md", "seats/builder/other.md"} {
		if _, err := os.Stat(filepath.Join(there, name)); err != nil {
			t.Errorf("the remote lacks %s: %v", name, err)
		}
	}
}

// refuseWith installs a pre-receive hook in the remote of a clone that refuses
// a push the way GitHub does when another push landed in the same second, for
// as many pushes as the file named by counter allows: the hook refuses while the
// file holds a number above zero and counts it down.
func refuseWith(t *testing.T, here string, refusals int) {
	t.Helper()
	remote := strings.TrimSpace(run(t, here, "git", "config", "remote.origin.url"))
	counter := filepath.Join(t.TempDir(), "refusals")
	if err := os.WriteFile(counter, []byte(fmt.Sprintf("%d\n", refusals)), 0o644); err != nil {
		t.Fatalf("writing the counter: %v", err)
	}
	hook := fmt.Sprintf(`#!/bin/sh
left=$(cat %[1]s)
if [ "$left" -gt 0 ]; then
	echo $((left - 1)) > %[1]s
	echo "cannot lock ref 'refs/heads/main': is at 3e031e3 but expected 9c1d0aa" >&2
	exit 1
fi
`, counter)
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatalf("writing the hook: %v", err)
	}
}

// A push GitHub refuses with "cannot lock ref" (another push landed in the same
// second) is followed by one pull and one more push, and when that goes the
// push has succeeded (mw-gq6.636).
func TestPushTriesAgainAfterTheRemoteCouldNotLockTheRef(t *testing.T) {
	here, _ := twoHosts(t)
	files := vault.New(here, vault.WithWait(func(context.Context, time.Duration) error { return nil }))
	write(t, here, "seats/builder/mine.md", "this host's file\n")
	if _, err := files.Commit(context.Background(), "this host writes", []string{"seats/builder/mine.md"}); err != nil {
		t.Fatalf("committing: %v", err)
	}
	refuseWith(t, here, 1)

	pushed, err := files.Push(context.Background())
	if err != nil {
		t.Fatalf("pushing after one cannot-lock-ref refusal: %v", err)
	}
	if pushed != 1 {
		t.Fatalf("expected the one commit pushed, got %d", pushed)
	}
	remote := strings.TrimSpace(run(t, here, "git", "ls-remote", "origin", "refs/heads/main"))
	if head := strings.TrimSpace(run(t, here, "git", "rev-parse", "HEAD")); !strings.HasPrefix(remote, head) {
		t.Fatalf("the remote holds %q, the clone is at %s", remote, head)
	}
}

// A push GitHub keeps refusing with "cannot lock ref" while other pushers
// land (the mill's close-outs, the Mayor, the desktop) is pulled and tried
// again after a short wait, up to three times: three refusals then an accepting
// remote is a push that went (mw-gq6.689).
func TestPushWaitsAndTriesAgainWhileTheRemoteKeepsRefusingToLockTheRef(t *testing.T) {
	here, _ := twoHosts(t)
	var waits []time.Duration
	files := vault.New(here, vault.WithWait(func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}))
	write(t, here, "seats/builder/mine.md", "this host's file\n")
	if _, err := files.Commit(context.Background(), "this host writes", []string{"seats/builder/mine.md"}); err != nil {
		t.Fatalf("committing: %v", err)
	}
	refuseWith(t, here, 3)

	pushed, err := files.Push(context.Background())
	if err != nil {
		t.Fatalf("pushing after three cannot-lock-ref refusals: %v", err)
	}
	if pushed != 1 {
		t.Fatalf("expected the one commit pushed, got %d", pushed)
	}
	want := []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}
	if fmt.Sprint(waits) != fmt.Sprint(want) {
		t.Fatalf("waited %v before the retries, want %v", waits, want)
	}
	remote := strings.TrimSpace(run(t, here, "git", "ls-remote", "origin", "refs/heads/main"))
	if head := strings.TrimSpace(run(t, here, "git", "rev-parse", "HEAD")); !strings.HasPrefix(remote, head) {
		t.Fatalf("the remote holds %q, the clone is at %s", remote, head)
	}
}

// A remote that refuses every push is still a failure: the error names the
// first refusal as well as the last, and says how many pushes were made, as the
// VaultPushRaced a dispatch tick answers with a warning (mw-gq6.689).
func TestPushRefusedAgainAfterTheRetriesFailsNamingBothAttempts(t *testing.T) {
	here, _ := twoHosts(t)
	files := vault.New(here, vault.WithWait(func(context.Context, time.Duration) error { return nil }))
	write(t, here, "seats/builder/mine.md", "this host's file\n")
	if _, err := files.Commit(context.Background(), "this host writes", []string{"seats/builder/mine.md"}); err != nil {
		t.Fatalf("committing: %v", err)
	}
	refuseWith(t, here, 100)

	_, err := files.Push(context.Background())
	if err == nil {
		t.Fatalf("expected the push to fail when every attempt is refused")
	}
	said := err.Error()
	if !strings.Contains(said, "cannot lock ref") || !strings.Contains(said, "first push") || !strings.Contains(said, "tried again") {
		t.Fatalf("expected the error to name the refusal of both attempts, got %v", err)
	}
	raced, ok := application.PushRaced(err)
	if !ok || raced.Attempts != 4 {
		t.Fatalf("expected a VaultPushRaced after 4 pushes, got %+v (%v)", raced, ok)
	}
}

// A push refused for a reason a wait does not cure is not a race: no wait, no
// VaultPushRaced.
func TestPushThatCannotRebaseIsNotARace(t *testing.T) {
	here, there := twoHosts(t)
	write(t, there, "seats/builder/same.md", "theirs\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "The other host writes")
	run(t, there, "git", "push", "-q")

	write(t, here, "seats/builder/same.md", "mine\n")
	waited := false
	files := vault.New(here, vault.WithWait(func(context.Context, time.Duration) error { waited = true; return nil }))
	if _, err := files.Commit(context.Background(), "this host writes", []string{"seats/builder/same.md"}); err != nil {
		t.Fatalf("committing: %v", err)
	}
	_, err := files.Push(context.Background())
	if err == nil {
		t.Fatal("expected a conflicting rebase to fail the push")
	}
	if _, raced := application.PushRaced(err); raced || waited {
		t.Fatalf("a conflict is not a race (raced=%v, waited=%v): %v", raced, waited, err)
	}
}

// A rebase that stops on a conflict is undone and the push says why it did not go.
func TestPushThatCannotRebaseSaysSoAndLeavesTheCloneAsItWas(t *testing.T) {
	here, there := twoHosts(t)
	write(t, there, "seats/builder/same.md", "theirs\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "The other host writes")
	run(t, there, "git", "push", "-q")

	write(t, here, "seats/builder/same.md", "mine\n")
	files := vault.New(here)
	if _, err := files.Commit(context.Background(), "this host writes", []string{"seats/builder/same.md"}); err != nil {
		t.Fatalf("committing: %v", err)
	}
	before := strings.TrimSpace(run(t, here, "git", "rev-parse", "HEAD"))
	if _, err := files.Push(context.Background()); err == nil {
		t.Fatalf("expected the push to fail on a conflict")
	}
	if after := strings.TrimSpace(run(t, here, "git", "rev-parse", "HEAD")); after != before {
		t.Fatalf("the clone moved from %s to %s", before, after)
	}
	if status := run(t, here, "git", "status", "--porcelain", "--untracked-files=no"); status != "" {
		t.Fatalf("the clone is left mid-rebase: %s", status)
	}
}

// A clone that tracks nothing is not a rejected push: the error names it.
func TestPushOfACloneThatTracksNothingSaysItHasNoUpstream(t *testing.T) {
	if _, err := exec.LookPath(vault.Git); err != nil {
		t.Skipf("%s is not on PATH", vault.Git)
	}
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	identify(t, dir)
	write(t, dir, "a.md", "a\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "first")
	_, err := vault.New(dir).Push(context.Background())
	if !errors.Is(err, application.ErrVaultNoUpstream) {
		t.Fatalf("expected ErrVaultNoUpstream, got %v", err)
	}
}

// A close-out also rewrites tracked files (seats/builder/ledger.md) before it
// commits them. A pull that lands between the write and the commit is refused
// with "Please commit or stash them"; the refusal is handed up as
// application.ErrVaultBeingWritten, so that a sync waits and pulls again
// (mw-gq6.706), and the vault is left as the close-out had it.
func TestPullNamesARefusalForFilesASiblingHasWrittenAndNotCommitted(t *testing.T) {
	here, there := twoHosts(t)

	write(t, there, "other.md", "from the other host\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "The other host's file")
	run(t, there, "git", "push", "-q")

	write(t, here, "seats/mayor/ledger.md", "2026-09-17 the factory opened.\nand a line the close-out has not committed.\n")

	files := vault.New(here, vault.WithWait(func(context.Context, time.Duration) error { return nil }))
	_, err := files.Pull(context.Background())
	if !errors.Is(err, application.ErrVaultBeingWritten) {
		t.Fatalf("expected the refusal to be ErrVaultBeingWritten, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(here, "seats/mayor/ledger.md")); !strings.Contains(string(got), "close-out has not committed") {
		t.Fatalf("expected the close-out's write left as it was, got %q", got)
	}

	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Close-out")
	if _, err := files.Pull(context.Background()); err != nil {
		t.Fatalf("expected the pull to work once the commit is made, got %v", err)
	}
}

// Other refusals are not it.
func TestPullDoesNotCallAnotherRefusalASiblingsWrite(t *testing.T) {
	fakeGit(t, "fatal: refusing to merge unrelated histories")

	files := vault.New(t.TempDir(), vault.WithWait(func(context.Context, time.Duration) error { return nil }))
	_, err := files.Pull(context.Background())
	if err == nil || errors.Is(err, application.ErrVaultBeingWritten) {
		t.Fatalf("expected a plain failure, got %v", err)
	}
}
