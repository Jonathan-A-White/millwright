package steps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The check scenarios run against the same world the close-out ones do — a real
// rig, origin and worktree, a real vault, a fake tracker — so their steps hang
// off nextContext and are registered with it. What they add is the run of
// mw check and the proof that it changed nothing.

// registerCheckSteps registers the steps of features/check.feature.
func registerCheckSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the story "([^"]*)" has already been closed$`, c.theStoryWasAlreadyClosed)
	ctx.Given(`^another close-out is holding the merge slot of the rig$`, c.anotherCloseOutHoldsTheSlot)

	ctx.Given(`^the session of "([^"]*)" ran in permission mode "([^"]*)"$`, c.theSessionRanInPermissionMode)
	ctx.Given(`^the host's load reads in turn: (.+)$`, c.theHostsLoadReads)
	ctx.Given(`^the rig's tests fail the first time and pass the second, saying "([^"]*)"$`, c.theRigsTestsFailOncePassNext)

	ctx.When(`^a session checks "([^"]*)"$`, c.aSessionChecks)
	ctx.When(`^a session checks "([^"]*)" while the other close-out finishes after (\d+) milliseconds$`, c.aSessionChecksWhileTheSlotIsHeld)
	ctx.When(`^a session checks "([^"]*)" without the merge slot$`, c.aSessionChecksWithoutTheSlot)
	registerBackgroundCheckSteps(ctx, c)
	registerCheckPartsSteps(ctx, c)

	ctx.Then(`^the check passes$`, c.theCheckPasses)
	ctx.Then(`^the check waited at least (\d+) milliseconds for the merge slot$`, c.theCheckWaited)
	ctx.Then(`^the check did not wait for the merge slot$`, c.theCheckDidNotWait)
	ctx.Then(`^the check said it was waiting for the merge slot, held by the other close-out$`, c.theCheckSaidItWasWaiting)
	ctx.Then(`^the check fails$`, c.theCheckFails)
	ctx.Then(`^the check fails, saying: (.+)$`, c.theCheckFailsSaying)
	ctx.Then(`^the check says the branch would be landed$`, c.theCheckSaysItWouldLand)
	ctx.Then(`^the check prints: (.+)$`, c.theCheckPrints)
	ctx.Then(`^the check does not print: (.+)$`, c.theCheckDoesNotPrint)
	ctx.Then(`^the check prints "([^"]*)" (\d+) time\(s\)$`, c.theCheckPrintsTimes)
	ctx.Then(`^the check names the signed commit$`, c.theCheckNamesTheSignedCommit)
	ctx.Then(`^the check wrote nothing to the tracker, the ledger, the vault or git$`, c.theCheckWroteNothing)
}

// touched is everything mw check must leave as it found it, written down so
// that it can be compared afterwards.
type touched struct {
	comments string
	state    string
	status   string
	ledger   string
	vault    string
	origin   string
	branch   string
	worktree string
	commits  int
}

// snapshot reads what a check must not change about one story.
func (c *nextContext) snapshot(id string) (touched, error) {
	var t touched
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return t, err
	}
	t.comments = strings.Join(c.tracker.Comments(id), "\n--\n")
	t.state = c.tracker.State(id, application.RunState)
	t.status = detail.Status

	lines, err := c.ledgerLines()
	if err != nil {
		return t, err
	}
	t.ledger = strings.Join(lines, "\n")

	if t.vault, err = digest(c.vault); err != nil {
		return t, err
	}
	if t.origin, err = gitSay(c.origin(), "rev-parse", "main"); err != nil {
		return t, err
	}
	if t.branch, err = gitSay(c.rig, "rev-parse", application.StoryBranch(id)); err != nil {
		return t, err
	}
	if t.worktree, err = gitSay(application.WorktreeDir(c.rig, id), "status", "--porcelain=v1", "-uall"); err != nil {
		return t, err
	}
	t.commits = len(c.files.Commits())
	return t, nil
}

// isCheckLog reports whether path is runs/<story>/check-<n>.log under dir.
func isCheckLog(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 3 || parts[0] != application.RunsDir {
		return false
	}
	return strings.HasPrefix(parts[2], "check-") && strings.HasSuffix(parts[2], ".log")
}

// digest is a fingerprint of every file under dir, by path and contents.
func digest(dir string) (string, error) {
	var lines []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		// A check keeps its log in the run of its story: that is what it is asked
		// to write, and nothing else in the vault.
		if isCheckLog(dir, path) {
			return nil
		}
		held, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s %x", path, sha256.Sum256(held)))
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n"), err
}

func (c *nextContext) theStoryWasAlreadyClosed(id string) error {
	return c.tracker.CloseStory(context.Background(), id, "landed by hand")
}

// otherCloseOut is the holder another close-out writes into the merge slot.
const otherCloseOut = "builder@vps closing out mw-gq6.99"

// slotHeldMilliseconds is how long a scenario that does not release the slot
// keeps it, far longer than a check that does not wait takes.
const slotHeldMilliseconds = 30000

func (c *nextContext) anotherCloseOutHoldsTheSlot() error {
	held, err := rig.NewSlots().Take(context.Background(), c.rig, otherCloseOut)
	if err != nil {
		return err
	}
	c.otherHolding = held
	return nil
}

// releaseTheOtherCloseOut gives the slot back, once, whenever it is asked to.
func (c *nextContext) releaseTheOtherCloseOut() {
	c.holdingMu.Lock()
	defer c.holdingMu.Unlock()
	if c.otherHolding != nil {
		_ = c.otherHolding.Release(context.Background())
		c.otherHolding = nil
	}
}

func (c *nextContext) aSessionChecksWhileTheSlotIsHeld(id string, millis int) error {
	if c.otherHolding == nil {
		return fmt.Errorf("no other close-out holds the merge slot in this scenario")
	}
	// The other close-out finishes millis after the check says it is waiting
	// for the slot, not millis after the check began: the check's own work
	// before the slot may take longer than that on a loaded host (mw-gq6.440).
	var finishing sync.Once
	var timerMu sync.Mutex
	var timer *time.Timer
	defer func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if timer != nil {
			timer.Stop()
		}
	}()
	started := time.Now()
	err := c.checkWith(id, rig.NewSlots(
		rig.WithSlotPoll(20*time.Millisecond),
		rig.WithSlotNotice(func(said string) {
			c.stderr.WriteString(said)
			finishing.Do(func() {
				timerMu.Lock()
				defer timerMu.Unlock()
				timer = time.AfterFunc(time.Duration(millis)*time.Millisecond, c.releaseTheOtherCloseOut)
			})
		}),
	))
	c.checkWaited = time.Since(started)
	return err
}

func (c *nextContext) aSessionChecksWithoutTheSlot(id string) error {
	started := time.Now()
	err := c.checkWith(id, nil)
	c.checkWaited = time.Since(started)
	return err
}

func (c *nextContext) theCheckWaited(millis int) error {
	if want := time.Duration(millis) * time.Millisecond; c.checkWaited < want {
		return fmt.Errorf("expected the check to wait at least %s for the merge slot, it took %s", want, c.checkWaited)
	}
	return nil
}

func (c *nextContext) theCheckDidNotWait() error {
	if limit := time.Duration(slotHeldMilliseconds/2) * time.Millisecond; c.checkWaited >= limit {
		return fmt.Errorf("expected the check not to wait for the merge slot, it took %s", c.checkWaited)
	}
	return nil
}

func (c *nextContext) theCheckSaidItWasWaiting() error {
	said := c.stderr.String()
	if !strings.Contains(said, "waiting for the merge slot") || !strings.Contains(said, otherCloseOut) {
		return fmt.Errorf("expected the check to say it was waiting for the merge slot held by %q, got:\n%s", otherCloseOut, said)
	}
	return nil
}

func (c *nextContext) aSessionChecks(id string) error {
	return c.checkWith(id, rig.NewSlots())
}

// checkWith runs mw check with the given merge slot, nil for none.
func (c *nextContext) checkWith(id string, slots *rig.Slots) error {
	before, err := c.snapshot(id)
	if err != nil {
		return err
	}
	c.touchedBefore, c.touchedFor = &before, id

	// A wrapper that writes down every git command, so that the scenario can
	// say which kinds mw check did not ask for.
	if err := os.Remove(c.gitLog); err != nil && !os.IsNotExist(err) {
		return err
	}
	worktrees := rig.New(rig.WithProgram(c.gitProgram))
	c.printed.Reset()
	var slot application.MergeSlot
	if slots != nil {
		slot = slots
	}
	// A host's load is only read when a scenario says what it reads, and the
	// waits for it to calm take no time.
	var load application.HostLoad
	if c.checkLoad != nil {
		load = c.checkLoad
	}
	k := c.newCheck(worktrees, slot, load, &c.printed)
	k.Logs = vault.New(c.vault)
	k.Records = vault.New(c.vault)
	c.checked, c.err = k.Run(context.Background(), id)
	return nil
}

// newCheck is the use case mw check runs, over the scenario's world.
func (c *nextContext) newCheck(worktrees *rig.Worktrees, slot application.MergeSlot, load application.HostLoad, out io.Writer) application.Check {
	return application.Check{
		Tracker:    c.tracker,
		Slot:       slot,
		Landing:    worktrees,
		Checks:     rig.NewChecks(rig.WithCommand(c.checkCommand), rig.WithSpecCommands(c.specCommands)),
		SpecChecks: rig.NewChecks(rig.WithCommand(c.checkCommand), rig.WithSpecCommands(c.specCommands)),
		Parts:      rig.NewChecks(rig.WithParts(map[string][]application.GatePart{c.rigKey(): c.gateParts})),
		Part:       c.partToCheck,
		Host:       nextHost,
		Rigs:       map[string]string{c.rigKey(): c.rig},
		Out:        out,
		Load:       load,
		LoadWait:   func(context.Context, time.Duration) error { return nil },
		Modes:      c.sessionModes,
	}
}

// sessionMode is the permission mode a scenario's session is said to have run
// in, which is what the harness's transcript of it would show.
type sessionMode string

func (m sessionMode) PermissionMode(context.Context, string) (string, error) { return string(m), nil }

func (c *nextContext) theSessionRanInPermissionMode(_, mode string) error {
	c.sessionModes = sessionMode(mode)
	return nil
}

func (c *nextContext) theCheckPasses() error {
	if c.err != nil || !c.checked.Passed() {
		return fmt.Errorf("expected the check to pass, got %+v (error: %v)\n%s", c.checked, c.err, c.printed.String())
	}
	return nil
}

func (c *nextContext) theCheckFails() error {
	if c.err == nil {
		return fmt.Errorf("expected the check to fail, but it returned no error:\n%s", c.printed.String())
	}
	return nil
}

// theCheckFailsSaying is a check that could not be made at all: the error
// mw check exits with says why, and there is no verdict to print.
func (c *nextContext) theCheckFailsSaying(words string) error {
	if err := c.theCheckFails(); err != nil {
		return err
	}
	if want := strings.TrimSpace(words); !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected the check to fail saying %q, got: %v", want, c.err)
	}
	return nil
}

func (c *nextContext) theCheckSaysItWouldLand() error {
	if said := c.printed.String(); !strings.Contains(said, "mw next would land it") {
		return fmt.Errorf("expected the check to say mw next would land the branch, got:\n%s", said)
	}
	return nil
}

func (c *nextContext) theCheckPrints(words string) error {
	if said, want := c.printed.String(), strings.TrimSpace(words); !strings.Contains(said, want) {
		return fmt.Errorf("expected the check to print %q, got:\n%s", want, said)
	}
	return nil
}

func (c *nextContext) theCheckDoesNotPrint(words string) error {
	if said, want := c.printed.String(), strings.TrimSpace(words); strings.Contains(said, want) {
		return fmt.Errorf("expected the check not to print %q, got:\n%s", want, said)
	}
	return nil
}

func (c *nextContext) theCheckNamesTheSignedCommit() error {
	if c.signed == "" {
		return fmt.Errorf("no commit was signed in this scenario")
	}
	return c.theCheckPrints(c.signed)
}

// theCheckWroteNothing is the check's whole promise: the story in the tracker,
// the ledger, the vault, the branch, the worktree and the origin are as they
// were, and git was never asked for anything that writes.
func (c *nextContext) theCheckWroteNothing() error {
	if c.touchedBefore == nil {
		return fmt.Errorf("no check was run in this scenario")
	}
	after, err := c.snapshot(c.touchedFor)
	if err != nil {
		return err
	}
	before := *c.touchedBefore
	for name, pair := range map[string][2]string{
		"the comments on the story":      {before.comments, after.comments},
		"the story's run state":          {before.state, after.state},
		"the story's status":             {before.status, after.status},
		"the ledger":                     {before.ledger, after.ledger},
		"the vault":                      {before.vault, after.vault},
		"the target branch at origin":    {before.origin, after.origin},
		"the story's branch":             {before.branch, after.branch},
		"the worktree's uncommitted set": {before.worktree, after.worktree},
	} {
		if pair[0] != pair[1] {
			return fmt.Errorf("expected %s to be as it was:\nbefore: %s\nafter:  %s", name, pair[0], pair[1])
		}
	}
	if after.commits != before.commits {
		return fmt.Errorf("expected nothing committed in the vault, got %d commit(s)", after.commits-before.commits)
	}

	logged, err := os.ReadFile(c.gitLog)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range bytes.Split(logged, []byte("\n")) {
		fields := strings.Fields(string(line))
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "fetch", "merge", "push", "commit", "worktree", "branch", "reset", "checkout", "add", "update-ref", "tag", "rebase", "pull":
			return fmt.Errorf("expected git to be asked for nothing that writes, but it was asked: git %s", line)
		}
	}
	return nil
}

// theHostsLoadReads says what the host's load reads, one reading at a time and
// the last again after: "calm" is load 3 of 20 cores and "busy" is 39 of 20.
func (c *nextContext) theHostsLoadReads(words string) error {
	var readings []application.LoadReading
	for _, word := range strings.Split(words, ",") {
		switch strings.TrimSpace(word) {
		case "calm":
			readings = append(readings, application.LoadReading{Load: 3, Cores: 20})
		case "busy":
			readings = append(readings, application.LoadReading{Load: 39, Cores: 20})
		default:
			return fmt.Errorf("a reading is calm or busy, not %q", word)
		}
	}
	c.checkLoad = &apptest.FakeHostLoad{Readings: readings}
	return nil
}

// theRigsTestsFailOncePassNext is the rig's tests as a loaded host runs them:
// the first run, which is the first line its log gets, fails.
func (c *nextContext) theRigsTestsFailOncePassNext(saying string) error {
	c.checkCommand = fmt.Sprintf("printf 'run\\n' >> %s; if [ \"$(wc -l < %s)\" -le 1 ]; then echo '%s'; exit 1; fi", c.checkLog, c.checkLog, saying)
	return nil
}

func (c *nextContext) theCheckPrintsTimes(words string, times int) error {
	if said, got := c.printed.String(), strings.Count(c.printed.String(), words); got != times {
		return fmt.Errorf("expected the check to print %q %d time(s), it printed it %d time(s):\n%s", words, times, got, said)
	}
	return nil
}
