package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The vault a sync scenario runs against is three real git repositories in a
// temp directory: one standing in for the remote both hosts share, and one
// clone per host. Nothing reaches the network, and the factory's own vault is
// never touched. The beads database is the fake gateway: a real `bd sync` would
// publish whatever this machine happened to hold.

// The ledger both hosts append to, and the lines they append to it.
const (
	mayorLedger = "seats/mayor/ledger.md"
	otherLine   = "2026-09-18 the other host worked mw-gq6.9."
	thisLine    = "2026-09-18 this host worked mw-gq6.11."
)

// syncedAt is the time a scenario's sync is pinned to, so that the note a host
// leaves can be compared exactly.
var syncedAt = time.Date(2026, 9, 18, 14, 30, 0, 0, time.UTC)

// syncContext holds the two hosts' clones, the fake beads database they share,
// and what the last sync did.
type syncContext struct {
	root  string // the temp directory holding all three repositories
	here  string // this host's clone of the vault
	there string // the other host's clone

	host    string
	tracker *apptest.FakeTracker

	headBefore   string // this host's HEAD before the sync
	remoteBefore string // the shared remote's main before the sync

	report application.SyncReport
	err    error
}

// InitializeSyncScenario registers the steps of features/sync.feature.
func InitializeSyncScenario(ctx *godog.ScenarioContext) {
	c := &syncContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = syncContext{tracker: apptest.NewFakeTracker()}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^a vault shared by both hosts$`, c.aVaultSharedByBothHosts)
	ctx.Given(`^this host is "([^"]*)"$`, c.thisHostIs)
	ctx.Given(`^the vault marks ledgers as append-only$`, c.theVaultMarksLedgers)
	ctx.Given(`^the vault does not mark ledgers as append-only$`, c.theVaultDoesNotMarkLedgers)
	ctx.Given(`^the other host appended a line to the Mayor's ledger and pushed it$`, c.theOtherHostAppendedAndPushed)
	ctx.Given(`^this host appended its own line to the Mayor's ledger$`, c.thisHostAppended)
	ctx.Given(`^this host has an uncommitted change to the Mayor's ledger$`, c.thisHostHasAnUncommittedChange)
	ctx.Given(`^bd sync will exit (\d+)$`, c.bdSyncWillExit)

	ctx.When(`^this host syncs$`, c.thisHostSyncs)

	ctx.Then(`^the sync succeeds$`, c.theSyncSucceeds)
	ctx.Then(`^the sync fails, and mw stops with a non-zero exit$`, c.theSyncFails)
	ctx.Then(`^the sync stops with the vault blocked$`, c.theSyncStopsWithTheVaultBlocked)
	ctx.Then(`^mw exits (\d+)$`, c.mwExits)
	ctx.Then(`^mw exits (\d+), which is neither a plain failure nor one of bd's own$`, c.mwExitsItsOwnStatus)
	ctx.Then(`^the failure is one line$`, c.theFailureIsOneLine)
	ctx.Then(`^the uncommitted change is still there, and nothing was pulled over it$`, c.theUncommittedChangeIsStillThere)
	ctx.Then(`^the failure says, in plain words:$`, c.theFailureSays)
	ctx.Then(`^the Mayor's ledger on this host holds, in this order:$`, c.theLedgerHoldsInThisOrder)
	ctx.Then(`^no conflict is left in the vault$`, c.noConflictIsLeft)
	ctx.Then(`^the other host sees both lines once it pulls$`, c.theOtherHostSeesBothLines)
	ctx.Then(`^the sync reports (\d+) commit pulled and (\d+) commit pushed$`, c.theSyncReports)
	ctx.Then(`^the sync reports nothing pulled and nothing pushed$`, c.theSyncReportsNothing)
	ctx.Then(`^the vault is where it was on both hosts$`, c.theVaultIsWhereItWasOnBothHosts)
	ctx.Then(`^the vault is where it was on this host$`, c.theVaultIsWhereItWasOnThisHost)
	ctx.Then(`^the beads database was synced once$`, c.theDatabaseWasSyncedOnce)
	ctx.Then(`^the beads database holds the time of the sync under (\S+)$`, c.theDatabaseHoldsTheTimeUnder)
	ctx.Then(`^the other host's next sync reads the time of the sync under (\S+)$`, c.theOtherHostReadsTheTimeUnder)
	ctx.Then(`^nothing is recorded under (\S+)$`, c.nothingIsRecordedUnder)
	ctx.Then(`^the vault marks (\S+) as (\S+)$`, c.theVaultMarks)
	ctx.Then(`^the sync reports that the mark was added$`, c.theSyncReportsTheMark)
}

// runGit runs one git command in a directory, for the scenario's own setup and
// checking. The adapter under test runs its own.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command(vault.Git, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// writeFile puts one file in a clone, making the directories above it.
func writeFile(dir, name, contents string) error {
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// appendLine adds a line to a file in a clone.
func appendLine(dir, name, line string) error {
	existing, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}
	return writeFile(dir, name, string(existing)+line+"\n")
}

func (c *syncContext) aVaultSharedByBothHosts() error {
	if _, err := exec.LookPath(vault.Git); err != nil {
		return fmt.Errorf("%s is not on PATH: %w", vault.Git, err)
	}
	root, err := os.MkdirTemp("", "mw-sync-")
	if err != nil {
		return fmt.Errorf("making the vaults: %w", err)
	}
	c.root = root

	remote := filepath.Join(root, "origin.git")
	if _, err := runGit(root, "init", "--bare", "-q", "-b", "main", remote); err != nil {
		return err
	}

	c.here = filepath.Join(root, "here")
	if _, err := runGit(root, "clone", "-q", remote, c.here); err != nil {
		return err
	}
	if err := c.identify(c.here); err != nil {
		return err
	}
	if err := writeFile(c.here, mayorLedger, "2026-09-17 the factory opened.\n"); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"},
		{"commit", "-qm", "The vault opens"},
		{"push", "-q", "-u", "origin", "main"},
	} {
		if _, err := runGit(c.here, args...); err != nil {
			return err
		}
	}

	c.there = filepath.Join(root, "there")
	if _, err := runGit(root, "clone", "-q", remote, c.there); err != nil {
		return err
	}
	return c.identify(c.there)
}

// identify gives a clone an author, so that committing in it works wherever
// these scenarios run.
func (c *syncContext) identify(dir string) error {
	if _, err := runGit(dir, "config", "user.name", "millwright test"); err != nil {
		return err
	}
	_, err := runGit(dir, "config", "user.email", "test@millwright.invalid")
	return err
}

func (c *syncContext) thisHostIs(host string) error {
	c.host = host
	return nil
}

func (c *syncContext) theVaultMarksLedgers() error {
	if err := writeFile(c.here, vault.AttributesFile, application.LedgerMark+"\n"); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"},
		{"commit", "-qm", "Ledgers merge by keeping every line"},
		{"push", "-q"},
	} {
		if _, err := runGit(c.here, args...); err != nil {
			return err
		}
	}
	_, err := runGit(c.there, "pull", "-q", "--rebase")
	return err
}

// theVaultDoesNotMarkLedgers takes the mark back out on both hosts, so that a
// scenario can start from a vault nobody has told about ledgers yet.
func (c *syncContext) theVaultDoesNotMarkLedgers() error {
	if err := os.RemoveAll(filepath.Join(c.here, vault.AttributesFile)); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"commit", "-qam", "Nobody has told git about ledgers yet"},
		{"push", "-q"},
	} {
		if _, err := runGit(c.here, args...); err != nil {
			return err
		}
	}
	_, err := runGit(c.there, "pull", "-q", "--rebase")
	return err
}

func (c *syncContext) theOtherHostAppendedAndPushed() error {
	if err := appendLine(c.there, mayorLedger, otherLine); err != nil {
		return err
	}
	if _, err := runGit(c.there, "commit", "-qam", "The other host works a story"); err != nil {
		return err
	}
	_, err := runGit(c.there, "push", "-q")
	return err
}

func (c *syncContext) thisHostAppended() error {
	if err := appendLine(c.here, mayorLedger, thisLine); err != nil {
		return err
	}
	_, err := runGit(c.here, "commit", "-qam", "This host works a story")
	return err
}

func (c *syncContext) thisHostHasAnUncommittedChange() error {
	return appendLine(c.here, mayorLedger, thisLine)
}

func (c *syncContext) bdSyncWillExit(code int) error {
	c.tracker.SyncExits(code, fmt.Sprintf("bd sync exited %d", code))
	return nil
}

func (c *syncContext) thisHostSyncs() error {
	var err error
	if c.headBefore, err = runGit(c.here, "rev-parse", "HEAD"); err != nil {
		return err
	}
	if c.remoteBefore, err = runGit(filepath.Join(c.root, "origin.git"), "rev-parse", "main"); err != nil {
		return err
	}

	sync := application.Sync{
		Vault:   vault.New(c.here),
		Tracker: c.tracker,
		Host:    c.host,
		Now:     func() time.Time { return syncedAt },
	}
	c.report, c.err = sync.Run(context.Background())
	return nil
}

func (c *syncContext) theSyncSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("the sync failed: %w", c.err)
	}
	return nil
}

// theSyncFails also covers mw's exit status: mw prints the reason cobra hands
// it and exits 1 on any error a command returns (cmd/mw/main.go), so a sync
// that comes back with an error is a mw that stops with a non-zero exit.
func (c *syncContext) theSyncFails() error {
	if c.err == nil {
		return fmt.Errorf("expected the sync to fail, but it reported %s", c.report)
	}
	return nil
}

// theSyncStopsWithTheVaultBlocked reads the failure as what it is: somebody's
// uncommitted work in the vault, naming the files, rather than an ordinary
// failure a timer would have to read the words of to tell apart.
func (c *syncContext) theSyncStopsWithTheVaultBlocked() error {
	if c.err == nil {
		return fmt.Errorf("expected the vault half to be blocked, but the sync reported %s", c.report)
	}
	blocked, stopped := application.Blocked(c.err)
	if !stopped {
		return fmt.Errorf("expected a blocked vault, got %v", c.err)
	}
	if len(blocked.Files) == 0 {
		return fmt.Errorf("expected the blocked vault to name the files in the way, got none")
	}
	return nil
}

// mwExits checks the status mw leaves with for this failure. mw's own exit is
// application.ExitStatus of whatever the command returned (cmd/mw/sync.go,
// cmd/mw/main.go), so this is the number a timer branches on.
func (c *syncContext) mwExits(status int) error {
	if got := application.ExitStatus(c.err); got != status {
		return fmt.Errorf("expected mw to leave with %d, got %d (from %v)", status, got, c.err)
	}
	return nil
}

// mwExitsItsOwnStatus also holds the status apart from every other one a sync
// can leave with: 1 is any plain failure and 2, 3 and 4 are bd's own, so a
// blocked vault has to be none of them for a timer to tell it from a fault.
func (c *syncContext) mwExitsItsOwnStatus(status int) error {
	if err := c.mwExits(status); err != nil {
		return err
	}
	for _, taken := range []int{1, 2, 3, 4} {
		if status == taken {
			return fmt.Errorf("expected a status of its own, got %d, which is already taken", status)
		}
	}
	return nil
}

// theFailureIsOneLine keeps the message a timer's log holds to one line.
func (c *syncContext) theFailureIsOneLine() error {
	if c.err == nil {
		return fmt.Errorf("expected the sync to stop, but it reported %s", c.report)
	}
	if said := c.err.Error(); strings.Contains(said, "\n") {
		return fmt.Errorf("expected one line, got %q", said)
	}
	return nil
}

// theUncommittedChangeIsStillThere is the promise that mw touched nobody's
// work: the edit is still uncommitted and the other host's line was not pulled
// over it.
func (c *syncContext) theUncommittedChangeIsStillThere() error {
	ledger, err := os.ReadFile(filepath.Join(c.here, mayorLedger))
	if err != nil {
		return fmt.Errorf("reading the ledger: %w", err)
	}
	if !strings.Contains(string(ledger), thisLine) {
		return fmt.Errorf("expected the uncommitted line to be left alone, the ledger holds %q", ledger)
	}
	if strings.Contains(string(ledger), otherLine) {
		return fmt.Errorf("expected nothing to have been pulled over the uncommitted change, the ledger holds %q", ledger)
	}
	left, err := runGit(c.here, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if !strings.Contains(left, mayorLedger) {
		return fmt.Errorf("expected %s to still be uncommitted, git says %q", mayorLedger, left)
	}
	return nil
}

func (c *syncContext) theFailureSays(table *godog.Table) error {
	if c.err == nil {
		return fmt.Errorf("expected the sync to fail, but it reported %s", c.report)
	}
	for _, row := range table.Rows {
		if want := row.Cells[0].Value; !strings.Contains(c.err.Error(), want) {
			return fmt.Errorf("expected the failure to say %q, got %q", want, c.err)
		}
	}
	return nil
}

func (c *syncContext) theLedgerHoldsInThisOrder(table *godog.Table) error {
	ledger, err := os.ReadFile(filepath.Join(c.here, mayorLedger))
	if err != nil {
		return fmt.Errorf("reading the ledger: %w", err)
	}
	at := 0
	for _, row := range table.Rows {
		want := row.Cells[0].Value
		found := strings.Index(string(ledger)[at:], want)
		if found < 0 {
			if strings.Contains(string(ledger), want) {
				return fmt.Errorf("the ledger holds %q, but out of order", want)
			}
			return fmt.Errorf("the ledger does not hold %q, it holds %q", want, ledger)
		}
		at += found + len(want)
	}
	return nil
}

func (c *syncContext) noConflictIsLeft() error {
	ledger, err := os.ReadFile(filepath.Join(c.here, mayorLedger))
	if err != nil {
		return fmt.Errorf("reading the ledger: %w", err)
	}
	for _, marker := range []string{"<<<<<<<", ">>>>>>>"} {
		if strings.Contains(string(ledger), marker) {
			return fmt.Errorf("the ledger holds a conflict marker: %q", ledger)
		}
	}
	left, err := runGit(c.here, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if left != "" {
		return fmt.Errorf("the vault was left with %q uncommitted", left)
	}
	for _, half := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD"} {
		if _, err := os.Stat(filepath.Join(c.here, ".git", half)); err == nil {
			return fmt.Errorf("the vault was left half-way through a merge: .git/%s is there", half)
		}
	}
	return nil
}

func (c *syncContext) theOtherHostSeesBothLines() error {
	if _, err := runGit(c.there, "pull", "-q", "--rebase"); err != nil {
		return err
	}
	ledger, err := os.ReadFile(filepath.Join(c.there, mayorLedger))
	if err != nil {
		return fmt.Errorf("reading the other host's ledger: %w", err)
	}
	for _, want := range []string{otherLine, thisLine} {
		if !strings.Contains(string(ledger), want) {
			return fmt.Errorf("the other host's ledger does not hold %q, it holds %q", want, ledger)
		}
	}
	return nil
}

func (c *syncContext) theSyncReports(pulled, pushed int) error {
	if err := c.theSyncSucceeds(); err != nil {
		return err
	}
	if c.report.Pulled != pulled || c.report.Pushed != pushed {
		return fmt.Errorf("expected %d pulled and %d pushed, got %d and %d",
			pulled, pushed, c.report.Pulled, c.report.Pushed)
	}
	return nil
}

func (c *syncContext) theSyncReportsNothing() error {
	if err := c.theSyncSucceeds(); err != nil {
		return err
	}
	if !c.report.Quiet() {
		return fmt.Errorf("expected a sync with nothing to do, got %s", c.report)
	}
	return nil
}

func (c *syncContext) theVaultIsWhereItWasOnThisHost() error {
	head, err := runGit(c.here, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != c.headBefore {
		return fmt.Errorf("expected this host to be at %s, it is at %s", c.headBefore, head)
	}
	return nil
}

func (c *syncContext) theVaultIsWhereItWasOnBothHosts() error {
	if err := c.theVaultIsWhereItWasOnThisHost(); err != nil {
		return err
	}
	remote, err := runGit(filepath.Join(c.root, "origin.git"), "rev-parse", "main")
	if err != nil {
		return err
	}
	if remote != c.remoteBefore {
		return fmt.Errorf("expected the shared remote to be at %s, it is at %s", c.remoteBefore, remote)
	}
	return nil
}

func (c *syncContext) theDatabaseWasSyncedOnce() error {
	if got := c.tracker.Syncs(); got != 1 {
		return fmt.Errorf("expected one synchronisation cycle, got %d", got)
	}
	return nil
}

func (c *syncContext) theDatabaseHoldsTheTimeUnder(key string) error {
	note, err := c.tracker.Note(context.Background(), key)
	if err != nil {
		return fmt.Errorf("reading %s: %w", key, err)
	}
	want := syncedAt.Format(application.LastSyncFormat)
	if note != want {
		return fmt.Errorf("expected %s to be %q, got %q", key, want, note)
	}
	if !c.report.At.Equal(syncedAt) {
		return fmt.Errorf("expected the sync to report it was level at %s, got %s", syncedAt, c.report.At)
	}
	return nil
}

// theOtherHostReadsTheTimeUnder reads the note as the other host's next sync
// would find it: as this host's sync published it, not as it sits in this
// host's own database.
func (c *syncContext) theOtherHostReadsTheTimeUnder(key string) error {
	got, want := c.tracker.PublishedNote(key), syncedAt.Format(application.LastSyncFormat)
	if got != want {
		return fmt.Errorf("expected the other host to read %s as %q after this one sync, got %q", key, want, got)
	}
	return nil
}

func (c *syncContext) nothingIsRecordedUnder(key string) error {
	note, err := c.tracker.Note(context.Background(), key)
	if err != nil {
		return fmt.Errorf("reading %s: %w", key, err)
	}
	if note != "" {
		return fmt.Errorf("expected nothing under %s, got %q", key, note)
	}
	return nil
}

func (c *syncContext) theVaultMarks(pattern, attribute string) error {
	attributes, err := os.ReadFile(filepath.Join(c.here, vault.AttributesFile))
	if err != nil {
		return fmt.Errorf("reading %s: %w", vault.AttributesFile, err)
	}
	for _, line := range strings.Split(string(attributes), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == pattern {
			for _, field := range fields[1:] {
				if field == attribute {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("expected %s to mark %s as %s, it holds %q", vault.AttributesFile, pattern, attribute, attributes)
}

func (c *syncContext) theSyncReportsTheMark() error {
	if err := c.theSyncSucceeds(); err != nil {
		return err
	}
	if !c.report.Marked {
		return fmt.Errorf("expected the sync to report that it marked the ledgers, got %s", c.report)
	}
	if !strings.Contains(c.report.String(), application.LedgerPattern) {
		return fmt.Errorf("expected the report to name what it marked, got %q", c.report.String())
	}
	return nil
}
