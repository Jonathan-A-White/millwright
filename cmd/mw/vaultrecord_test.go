package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A vault-writing verb a seat runs commits the files it wrote and pushes them,
// so that a seat's record never leaves the vault dirty for the next dispatch to
// refuse (mw-gq6.501). These tests run the whole command against a real git
// vault with a bare repository for its remote.

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func putFile(t *testing.T, dir, name, text string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

const recordedClass = "---\nsymptom: a test clock\nkinds: pwa\nsince: 2026-10-14\n---\n"

// gitVaultWithRemote makes a vault clone with a remote it tracks (or none), holding
// a register class, a rig folder for memory and an unrelated tracked file, and
// points MW_VAULT at it. It returns the clone and the bare remote.
func gitVaultWithRemote(t *testing.T, withRemote bool) (vaultDir, remote string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("MW_HOST", "laptop")
	remote = filepath.Join(root, "origin.git")
	vaultDir = filepath.Join(root, "vault")
	gitIn(t, root, "init", "--bare", "-q", "-b", "main", remote)
	gitIn(t, root, "clone", "-q", remote, vaultDir)
	gitIn(t, vaultDir, "config", "user.name", "test")
	gitIn(t, vaultDir, "config", "user.email", "test@millwright.invalid")
	putFile(t, vaultDir, "seats/builder/common-errors/old-class.md", recordedClass)
	putFile(t, vaultDir, "seats/builder/rigs/demo/about.md", "a demo rig\n")
	putFile(t, vaultDir, "seats/builder/rigs/demo/facts/old-fact.md", "kept\n")
	putFile(t, vaultDir, "seats/mayor/ledger.md", "one\n")
	gitIn(t, vaultDir, "add", "-A")
	gitIn(t, vaultDir, "commit", "-qm", "The vault opens")
	if withRemote {
		gitIn(t, vaultDir, "push", "-q", "-u", "origin", "main")
	}
	t.Setenv("MW_VAULT", vaultDir)
	return vaultDir, remote
}

func runMW(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("mw %s: %v\n%s%s", strings.Join(args, " "), err, out, errOut)
	}
	return out.String(), errOut.String()
}

// theCommitHolds asserts the vault has exactly one new commit since before,
// holding only want, and no tracked change left.
func theCommitHolds(t *testing.T, vaultDir, before string, want ...string) {
	t.Helper()
	if n := gitIn(t, vaultDir, "rev-list", "--count", before+"..HEAD"); n != "1" {
		t.Fatalf("want one new commit, got %s", n)
	}
	got := strings.Fields(gitIn(t, vaultDir, "show", "--name-only", "--format=", "HEAD"))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the commit holds %v, want %v", got, want)
	}
	if dirty := gitIn(t, vaultDir, "status", "--porcelain", "--untracked-files=no"); dirty != "" {
		t.Errorf("the vault is left with tracked changes:\n%s", dirty)
	}
}

func TestSeatRecordVerbsCommitTheirFilesAndPush(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
		in   string // a phrase the commit message holds
	}{
		{"errors add", []string{"errors", "add", "new-class", "--symptom", "a clock", "--kinds", "pwa"},
			[]string{"seats/builder/common-errors/new-class.md"}, "mw errors add new-class"},
		{"errors checked", []string{"errors", "checked", "old-class", "lampas", "found:mw-a.1"},
			[]string{"seats/builder/common-errors/old-class.md"}, "mw errors checked old-class"},
		{"memory add", []string{"memory", "add", "demo", "--kind", "gotcha", "--subject", "clock", "--source", "mw-a.1", "a new sentence here"},
			[]string{"seats/builder/rigs/demo/facts/a-new-sentence-here.md"}, "mw memory add demo a-new-sentence-here"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vaultDir, remote := gitVaultWithRemote(t, true)
			before := gitIn(t, vaultDir, "rev-parse", "HEAD")
			_, stderr := runMW(t, c.args...)
			theCommitHolds(t, vaultDir, before, c.want...)
			if subject := gitIn(t, vaultDir, "log", "-1", "--format=%s"); !strings.Contains(subject, c.in) {
				t.Errorf("the commit message %q does not name %q", subject, c.in)
			}
			if pushed := gitIn(t, remote, "rev-parse", "main"); pushed != gitIn(t, vaultDir, "rev-parse", "HEAD") {
				t.Errorf("the commit was not pushed (remote at %s)", pushed)
			}
			if stderr != "" {
				t.Errorf("a push that worked said something: %q", stderr)
			}
		})
	}
}

func TestSeatRecordVerbWithNoRemoteKeepsTheCommitAndSaysWhyNotPushed(t *testing.T) {
	vaultDir, _ := gitVaultWithRemote(t, false)
	before := gitIn(t, vaultDir, "rev-parse", "HEAD")
	_, stderr := runMW(t, "errors", "add", "new-class", "--symptom", "a clock", "--kinds", "pwa")
	theCommitHolds(t, vaultDir, before, "seats/builder/common-errors/new-class.md")
	if !strings.Contains(stderr, "not pushed") {
		t.Errorf("stderr should say why it was not pushed, got %q", stderr)
	}
}

func TestSeatRecordVerbDoesNotSweepInSomeoneElsesUncommittedChange(t *testing.T) {
	vaultDir, _ := gitVaultWithRemote(t, true)
	putFile(t, vaultDir, "seats/mayor/ledger.md", "one\ntwo, somebody else's\n")
	before := gitIn(t, vaultDir, "rev-parse", "HEAD")

	runMW(t, "memory", "add", "demo", "--kind", "gotcha", "--subject", "clock", "--source", "mw-a.1", "a new sentence here")

	got := strings.Fields(gitIn(t, vaultDir, "show", "--name-only", "--format=", "HEAD"))
	if len(got) != 1 || got[0] != "seats/builder/rigs/demo/facts/a-new-sentence-here.md" {
		t.Errorf("the commit holds %v, want only the fact", got)
	}
	if n := gitIn(t, vaultDir, "rev-list", "--count", before+"..HEAD"); n != "1" {
		t.Errorf("want one new commit, got %s", n)
	}
	if dirty := gitIn(t, vaultDir, "status", "--porcelain", "--untracked-files=no"); dirty != "M seats/mayor/ledger.md" {
		t.Errorf("somebody else's change should stay uncommitted, status: %q", dirty)
	}
}

func TestMemorySupersedeAndMigrateCommitEveryFileTheyTouched(t *testing.T) {
	vaultDir, remote := gitVaultWithRemote(t, true)
	putFile(t, vaultDir, "seats/builder/rigs/demo/facts/old-fact.md",
		"---\nstatus: current\nkind: gotcha\nsubject: clock\nsource: mw-a.1\nsince: 2026-10-01\n---\nan old sentence\n")
	putFile(t, vaultDir, "seats/builder/rigs/legacy.md", "# legacy\n\nA rig.\n\n- A thing to know about it (mw-b.1)\n")
	gitIn(t, vaultDir, "add", "-A")
	gitIn(t, vaultDir, "commit", "-qm", "facts")
	gitIn(t, vaultDir, "push", "-q")

	before := gitIn(t, vaultDir, "rev-parse", "HEAD")
	runMW(t, "memory", "supersede", "demo", "old-fact", "--source", "mw-a.2", "--slug", "new-fact", "a new sentence")
	theCommitHolds(t, vaultDir, before,
		"seats/builder/rigs/demo/facts/new-fact.md", "seats/builder/rigs/demo/facts/old-fact.md")

	before = gitIn(t, vaultDir, "rev-parse", "HEAD")
	runMW(t, "memory", "migrate", "legacy")
	if n := gitIn(t, vaultDir, "rev-list", "--count", before+"..HEAD"); n != "1" {
		t.Fatalf("migrate: want one new commit, got %s", n)
	}
	if dirty := gitIn(t, vaultDir, "status", "--porcelain", "--untracked-files=all"); dirty != "" {
		t.Errorf("migrate left the vault dirty:\n%s", dirty)
	}
	if !strings.Contains(gitIn(t, vaultDir, "show", "--name-status", "--format=", "HEAD"), "D\tseats/builder/rigs/legacy.md") {
		t.Errorf("migrate's commit does not record the memory file as removed")
	}
	if gitIn(t, remote, "rev-parse", "main") != gitIn(t, vaultDir, "rev-parse", "HEAD") {
		t.Errorf("migrate's commit was not pushed")
	}
}

// mw memory demote --question appends the question to eval.md and commits it
// with the fact in one commit, leaving no tracked change (mw-gq6.574). The other
// verbs that write a fact (retire, recheck, promote) commit their one file.
func TestMemoryDemoteWithAQuestionCommitsEvalAndFactTogether(t *testing.T) {
	vaultDir, _ := gitVaultWithRemote(t, true)
	putFile(t, vaultDir, "seats/builder/rigs/demo/facts/old-fact.md",
		"---\nstatus: current\nkind: gotcha\nsubject: clock\nsource: mw-a.1\nsince: 2026-10-01\n---\nan old sentence about clocks\n")
	putFile(t, vaultDir, "seats/builder/rigs/demo/eval.md", "Q: something else\nexpect: other\n")
	gitIn(t, vaultDir, "add", "-A")
	gitIn(t, vaultDir, "commit", "-qm", "facts")
	gitIn(t, vaultDir, "push", "-q")

	before := gitIn(t, vaultDir, "rev-parse", "HEAD")
	runMW(t, "memory", "demote", "demo", "old-fact", "--question", "old sentence clocks")
	theCommitHolds(t, vaultDir, before,
		"seats/builder/rigs/demo/eval.md", "seats/builder/rigs/demo/facts/old-fact.md")

	before = gitIn(t, vaultDir, "rev-parse", "HEAD")
	runMW(t, "memory", "promote", "demo", "old-fact")
	theCommitHolds(t, vaultDir, before, "seats/builder/rigs/demo/facts/old-fact.md")
	runMW(t, "memory", "recheck", "demo", "old-fact", "--why", "doubt")
	runMW(t, "memory", "retire", "demo", "old-fact", "--reason", "gone")
	if dirty := gitIn(t, vaultDir, "status", "--porcelain", "--untracked-files=all"); dirty != "" {
		t.Errorf("the vault is left dirty:\n%s", dirty)
	}
}

// mw invest add and mw invest outcome commit the investments ledger in the
// step that writes it and push, so that a tracked ledger is never left for a
// person to commit before dispatch will run (mw-gq6.502).
func TestInvestAddAndOutcomeCommitTheLedgerAndPush(t *testing.T) {
	vaultDir, remote := gitVaultWithRemote(t, true)
	const ledger = "plans/investments/ledger.jsonl"

	before := gitIn(t, vaultDir, "rev-parse", "HEAD")
	runMW(t, "invest", "add", "mw-a.1", "--aim", "cut merge conflicts", "--predict", "tokens -10%", "--measure", "tokens per story", "--review", "14")
	theCommitHolds(t, vaultDir, before, ledger)
	if subject := gitIn(t, vaultDir, "log", "-1", "--format=%s"); !strings.Contains(subject, "mw invest") || !strings.Contains(subject, "mw-a.1") {
		t.Errorf("the commit message %q does not name mw invest and the bead", subject)
	}

	before = gitIn(t, vaultDir, "rev-parse", "HEAD")
	runMW(t, "invest", "outcome", "mw-a.1", "--result", "win", "--actual", "tokens -12%", "--lesson", "keep going")
	theCommitHolds(t, vaultDir, before, ledger)

	if pushed := gitIn(t, remote, "rev-parse", "main"); pushed != gitIn(t, vaultDir, "rev-parse", "HEAD") {
		t.Errorf("the commits were not pushed (remote at %s)", pushed)
	}
}

// moveTheRemote makes the vault's remote move without the vault's clone
// hearing of it, as it does on the busy home vault when a landing commits the
// ledger: another clone pushes, and the clone's push is then refused as not a
// fast-forward. It returns that other clone.
func moveTheRemote(t *testing.T, vaultDir, remote string) (other string) {
	t.Helper()
	root := filepath.Dir(vaultDir)
	other = filepath.Join(root, "other")
	gitIn(t, root, "clone", "-q", remote, other)
	gitIn(t, other, "config", "user.name", "test")
	gitIn(t, other, "config", "user.email", "test@millwright.invalid")
	putFile(t, other, "seats/mayor/ledger.md", "one\na landing's line\n")
	gitIn(t, other, "add", "-A")
	gitIn(t, other, "commit", "-qm", "A landing commits the ledger")
	gitIn(t, other, "push", "-q", "origin", "main")
	return other
}

func TestSeatRecordVerbPushesEvenWhenTheRemoteMovedAfterTheCommit(t *testing.T) {
	vaultDir, remote := gitVaultWithRemote(t, true)
	moveTheRemote(t, vaultDir, remote)

	_, stderr := runMW(t, "errors", "add", "new-class", "--symptom", "a clock", "--kinds", "pwa")

	if pushed := gitIn(t, remote, "rev-parse", "main"); pushed != gitIn(t, vaultDir, "rev-parse", "HEAD") {
		t.Errorf("the commit was not pushed (remote at %s)", pushed)
	}
	if files := gitIn(t, remote, "ls-tree", "-r", "--name-only", "main"); !strings.Contains(files, "common-errors/new-class.md") {
		t.Errorf("the remote lacks the new class:\n%s", files)
	}
	if stderr != "" {
		t.Errorf("a push that worked said something: %q", stderr)
	}
}

func TestSeatRecordVerbFailsPlainlyWhenTheRebasedPushStillCannotGo(t *testing.T) {
	vaultDir, remote := gitVaultWithRemote(t, true)
	// The other host's commit changes the very file this verb adds, so the rebase conflicts.
	other := moveTheRemote(t, vaultDir, remote)
	putFile(t, other, "seats/builder/common-errors/new-class.md", recordedClass+"theirs\n")
	gitIn(t, other, "add", "-A")
	gitIn(t, other, "commit", "-qm", "The same class from the other host")
	gitIn(t, other, "push", "-q", "origin", "main")

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs([]string{"errors", "add", "new-class", "--symptom", "a clock", "--kinds", "pwa"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("mw errors add exited well though its commit could not be pushed")
	}
	if !strings.Contains(err.Error(), "not pushed") {
		t.Errorf("the error should say the commit was not pushed, got %v", err)
	}
	if dirty := gitIn(t, vaultDir, "status", "--porcelain", "--untracked-files=no"); dirty != "" {
		t.Errorf("the vault is left mid-rebase or dirty:\n%s", dirty)
	}
}
