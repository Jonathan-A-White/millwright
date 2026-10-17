package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// The steps of the batch fixer (features/batch_land.feature, plans/0117 Land
// Rush 5). No harness runs: the fixer is a runner that, the moment mw land
// starts it, does to the batch's landing worktree what the scenario says a
// fixer did, leaves the result a harness leaves, and ends.

// fakeFixer is the runner mw land starts its fixer through in a scenario.
type fakeFixer struct {
	*apptest.FakeRunner
	vault string
	// fix is what the fixer does in the landing worktree.
	fix func(dir string) error
	// touched is the story the fixer says it touched.
	touched string
	// stopped, when set, is the harness's word on a fixer it stopped for a
	// limit of the factory's: the fixer does nothing, and its result says so.
	stopped string

	signed  string // the hash of the signed commit the fixer made, when it made one
	started []application.SessionSpec
	boot    []string          // the boot file each fixer was primed with, read as it started
	present []map[string]bool // the files each fixer's worktree held as it started
	err     error
}

func (f *fakeFixer) Start(ctx context.Context, spec application.SessionSpec) error {
	if err := f.FakeRunner.Start(ctx, spec); err != nil {
		return err
	}
	f.started = append(f.started, spec)
	name := spec.Env["MW_STORY"]
	boot, err := os.ReadFile(filepath.Join(f.vault, application.RunsDir, name, application.BootFileName))
	if err != nil {
		f.err = fmt.Errorf("reading the fixer's boot file: %w", err)
	}
	f.boot = append(f.boot, string(boot))
	present := map[string]bool{}
	if entries, err := os.ReadDir(spec.Dir); err == nil {
		for _, entry := range entries {
			present[entry.Name()] = true
		}
	}
	f.present = append(f.present, present)

	result := fmt.Sprintf(`{"type":"result","subtype":"success","is_error":true,"num_turns":1,"result":%q}`, f.stopped)
	if f.stopped == "" {
		if err := f.fix(spec.Dir); err != nil && f.err == nil {
			f.err = fmt.Errorf("the fake fixer: %w", err)
		}
		result = fmt.Sprintf(`{"type":"result","subtype":"success","is_error":false,"num_turns":7,`+
			`"result":"Mended the batch.\nTOUCHED %[1]s: the files it named\nCAUSE %[1]s: its change and another's did not agree",`+
			`"modelUsage":{"claude-haiku":{"inputTokens":1200,"outputTokens":300}}}`, f.touched)
	}
	if err := os.WriteFile(filepath.Join(f.vault, application.RunsDir, name, application.ResultFileName), []byte(result), 0o644); err != nil && f.err == nil {
		f.err = fmt.Errorf("writing the fixer's result: %w", err)
	}
	f.FakeRunner.Exit(spec.Name, 0)
	return nil
}

// registerBatchFixSteps registers the fixer's steps of
// features/batch_land.feature.
func registerBatchFixSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's tests fail on the batch, saying "([^"]*)", until "([^"]*)" is committed$`, c.theTestsFailUntilCommitted)
	ctx.Given(`^mw land may start a fixer, which commits "([^"]*)" and says it touched "([^"]*)"$`, c.aFixerThatCommits)
	ctx.Given(`^mw land may start a fixer, which commits "([^"]*)" under a message saying "([^"]*)" and says it touched "([^"]*)"$`, c.aFixerThatCommitsSigned)
	ctx.Then(`^the batch's hold names the fixer's signed commit$`, c.theHoldNamesTheFixersSignedCommit)
	ctx.Given(`^mw land may start a fixer, which merges "([^"]*)" keeping both sides and says it touched "([^"]*)"$`, c.aFixerThatMerges)
	ctx.Given(`^mw land may start a fixer, which merges "([^"]*)" and "([^"]*)" keeping both sides and says it touched "([^"]*)"$`, c.aFixerThatMergesBoth)
	ctx.Then(`^no fixer was started$`, c.noFixerWasStarted)
	ctx.Then(`^exactly one fixer was started, for batch (\d+), in a landing worktree holding the work of every story in it$`, c.exactlyOneFixerWasStarted)
	ctx.Then(`^the fixer was started on the model "([^"]*)" with at most (\d+) turns$`, c.theFixerWasStartedOn)
	ctx.Then(`^the fixer's prompt names the branch of every story in the batch and the tests' last words "([^"]*)"$`, c.theFixersPromptNamesTheBranches)
	ctx.Then(`^the fixer's prompt says it may not push, revert a branch, drop a story's change, or delete, skip or loosen a test without saying so on that story$`, c.theFixersPromptSaysWhatItMayNotDo)
	ctx.Then(`^the fixer's prompt names "([^"]*)" in the conflict of "([^"]*)"$`, c.theFixersPromptNamesTheConflict)
	ctx.Then(`^the rig's tests were run twice on the batch$`, c.theTestsRanTwiceOnTheBatch)
	ctx.Then(`^"([^"]*)" is closed and recorded as landed, and "([^"]*)" at the rig's origin holds the words of both "([^"]*)" and "([^"]*)"$`, c.landedKeepingBothSides)
	ctx.Then(`^the ledger line of "([^"]*)" says "([^"]*)", and those of the others do not:$`, c.theLedgerLineSays)
	ctx.Then(`^metrics/factory.jsonl holds a line for batch (\d+) of the rig: (\d+) stories, (\d+) gate runs?, a fixer, outcome "([^"]*)"$`, c.theMetricsHoldAFixedBatch)
	ctx.Then(`^the Mayor's mail about the batch says the fixer was given it and no second fixer was started$`, c.theMailSaysTheFixerWasGivenIt)
	ctx.Given(`^mw land may start a fixer, which the harness stops saying "([^"]*)"$`, c.aFixerTheHarnessStops)
	ctx.Then(`^the batch is held for the harness's stop of its fixer, not its work, until "([^"]*)"$`, c.theBatchIsHeldForTheHarness)
	ctx.Then(`^this host claims nothing new until "([^"]*)"$`, c.thisHostClaimsNothingNewUntil)
	ctx.Then(`^the fixer was started only once$`, c.theFixerWasStartedOnlyOnce)
}

// theTestsFailUntilCommitted has the rig's tests fail, saying saying, in any
// worktree that does not hold file.
func (c *nextContext) theTestsFailUntilCommitted(saying, file string) error {
	c.checkCommand = fmt.Sprintf("printf 'run\\n' >> %s; if [ -f %s ]; then exit 0; fi; echo '%s'; exit 1", c.checkLog, file, saying)
	return nil
}

func (c *nextContext) newFixer(touched string, fix func(dir string) error) {
	c.batch.fixer = &fakeFixer{FakeRunner: apptest.NewFakeRunner(), vault: c.vault, touched: touched, fix: fix}
}

func (c *nextContext) aFixerThatCommits(file, touched string) error {
	c.newFixer(touched, func(dir string) error {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("the fixer's mend\n"), 0o644); err != nil {
			return err
		}
		return commitIn(dir, "Mend the batch: "+file)
	})
	return nil
}

// aFixerThatCommitsSigned commits file under a message carrying line, and
// remembers the commit's hash.
func (c *nextContext) aFixerThatCommitsSigned(file, line, touched string) error {
	c.newFixer(touched, nil)
	f := c.batch.fixer
	f.fix = func(dir string) error {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("the fixer's mend\n"), 0o644); err != nil {
			return err
		}
		if err := commitIn(dir, "Mend the batch: "+file+"\n\n"+line); err != nil {
			return err
		}
		hash, err := gitSay(dir, "rev-parse", "--short", "HEAD")
		f.signed = hash
		return err
	}
	return nil
}

func (c *nextContext) theHoldNamesTheFixersSignedCommit() error {
	f, err := c.theFixer()
	if err != nil {
		return err
	}
	if f.signed == "" || !strings.Contains(c.batch.report.Held, f.signed) {
		return fmt.Errorf("expected the hold to name the fixer's commit %q, got %q", f.signed, c.batch.report.Held)
	}
	return nil
}

// aFixerThatMerges merges the story's branch as the origin has it and settles
// every conflicting file by keeping both sides, the batch's first.
func (c *nextContext) aFixerThatMerges(id, touched string) error {
	return c.aFixerThatMergesAll([]string{id}, touched)
}

// aFixerThatMergesBoth does the same for two stories, one after the other.
func (c *nextContext) aFixerThatMergesBoth(first, second, touched string) error {
	return c.aFixerThatMergesAll([]string{first, second}, touched)
}

func (c *nextContext) aFixerThatMergesAll(ids []string, touched string) error {
	c.newFixer(touched, func(dir string) error {
		for _, id := range ids {
			if err := mergeKeepingBothSides(dir, application.StartPoint("origin", application.StoryBranch(id))); err != nil {
				return err
			}
		}
		return nil
	})
	return nil
}

// mergeKeepingBothSides merges branch into the worktree dir and settles every
// conflicting file by keeping both sides, the batch's first.
func mergeKeepingBothSides(dir, branch string) error {
	if err := gitRun(dir, "git", "merge", "--no-edit", branch); err == nil {
		return nil
	}
	conflicted, err := gitSay(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return err
	}
	for _, file := range strings.Fields(conflicted) {
		ours, err := gitSay(dir, "show", "HEAD:"+file)
		if err != nil {
			return err
		}
		theirs, err := gitSay(dir, "show", branch+":"+file)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(ours+"\n"+theirs+"\n"), 0o644); err != nil {
			return err
		}
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "--no-edit"}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theFixer() (*fakeFixer, error) {
	f := c.batch.fixer
	if f == nil {
		return nil, fmt.Errorf("the scenario gave mw land no fixer")
	}
	if f.err != nil {
		return nil, f.err
	}
	if len(f.started) == 0 {
		return nil, fmt.Errorf("expected mw land to start a fixer, it started none\n%s", c.said())
	}
	return f, nil
}

func (c *nextContext) noFixerWasStarted() error {
	f := c.batch.fixer
	if f == nil {
		return fmt.Errorf("the scenario gave mw land no fixer, so it proves nothing")
	}
	if f.err != nil {
		return f.err
	}
	if len(f.started) != 0 {
		return fmt.Errorf("expected no fixer, got %d: %+v", len(f.started), f.started)
	}
	return nil
}

func (c *nextContext) exactlyOneFixerWasStarted(number int) error {
	f, err := c.theFixer()
	if err != nil {
		return err
	}
	if len(f.started) != 1 {
		return fmt.Errorf("expected exactly one fixer, got %d: %+v", len(f.started), f.started)
	}
	spec := f.started[0]
	if want := application.SessionName(application.BatchFixerName(c.rigKey(), number)); spec.Name != want {
		return fmt.Errorf("expected the fixer's session to be %s, got %s", want, spec.Name)
	}
	if spec.Dir == c.rig || strings.HasPrefix(spec.Dir, application.WorktreeDir(c.rig, "")) {
		return fmt.Errorf("expected the fixer to work in the batch's landing worktree, got %s", spec.Dir)
	}
	// What the batch merged before the fixer was started is in its worktree;
	// what the fixer merged itself is not yet.
	for _, story := range c.batch.report.Merged {
		if !story.ByFixer && !f.present[0][story.ID+".md"] {
			return fmt.Errorf("expected the fixer's worktree to hold the work of %s, merged into the batch, it held %v", story.ID, f.present[0])
		}
	}
	if got := spec.Env["MW_SEAT"]; !strings.HasPrefix(got, nextSeat+"@") {
		return fmt.Errorf("expected the fixer booted into the %s seat, got %q", nextSeat, got)
	}
	return nil
}

func (c *nextContext) theFixerWasStartedOn(model string, turns int) error {
	f, err := c.theFixer()
	if err != nil {
		return err
	}
	line := strings.Join(f.started[0].Command, " ")
	// A story's session is started on its model's full id, not the alias (mw-gq6.545).
	for _, want := range []string{"--model " + domain.FullModelID(domain.Model(model)), fmt.Sprintf("--max-turns %d", turns)} {
		if !strings.Contains(line, want) {
			return fmt.Errorf("expected the fixer's command to hold %q, got:\n%s", want, line)
		}
	}
	return nil
}

// prompt is everything the fixer was told: its boot file and its command line.
func (f *fakeFixer) prompt() string {
	return f.boot[0] + "\n" + strings.Join(f.started[0].Command, " ")
}

func (c *nextContext) theFixersPromptNamesTheBranches(said string) error {
	f, err := c.theFixer()
	if err != nil {
		return err
	}
	prompt := f.prompt()
	for _, story := range c.batch.report.Queue {
		if !strings.Contains(prompt, story.Branch) {
			return fmt.Errorf("expected the fixer's prompt to name %s, got:\n%s", story.Branch, prompt)
		}
	}
	if !strings.Contains(prompt, said) {
		return fmt.Errorf("expected the fixer's prompt to hold the tests' last words %q, got:\n%s", said, prompt)
	}
	return nil
}

func (c *nextContext) theFixersPromptSaysWhatItMayNotDo() error {
	f, err := c.theFixer()
	if err != nil {
		return err
	}
	prompt := f.prompt()
	for _, want := range []string{
		"may not push",
		"revert a story's branch",
		"drop a story's change",
		"delete, skip or loosen a test without saying so on that story",
		"comment on each story you touched",
	} {
		if !strings.Contains(prompt, want) {
			return fmt.Errorf("expected the fixer's prompt to say %q, got:\n%s", want, prompt)
		}
	}
	return nil
}

func (c *nextContext) theFixersPromptNamesTheConflict(file, id string) error {
	f, err := c.theFixer()
	if err != nil {
		return err
	}
	prompt := f.prompt()
	branch := application.StartPoint("origin", application.StoryBranch(id))
	for _, line := range strings.Split(prompt, "\n") {
		if strings.Contains(line, branch) && strings.Contains(line, "CONFLICT") && strings.Contains(line, file) {
			return nil
		}
	}
	return fmt.Errorf("expected a line of the fixer's prompt to name %s and git's conflict in %s, got:\n%s", branch, file, prompt)
}

func (c *nextContext) theTestsRanTwiceOnTheBatch() error {
	if ran := c.checksRun() - c.batch.checksBefore; ran != 2 {
		return fmt.Errorf("expected the rig's tests to run twice on the batch, got %d\n%s", ran, c.said())
	}
	return nil
}

func (c *nextContext) landedKeepingBothSides(id, file, first, second string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunLanded {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q\n%s", id, application.RunState, application.RunLanded, got, c.said())
	}
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if !detail.Closed() {
		return fmt.Errorf("expected %s to be closed, got %q", id, detail.Status)
	}
	held, err := gitSay(c.origin(), "show", "main:"+file)
	if err != nil {
		return err
	}
	for _, story := range []string{first, second} {
		if !strings.Contains(held, story) {
			return fmt.Errorf("expected %s on main to keep the words of %s, got:\n%s", file, story, held)
		}
	}
	return nil
}

// landingLine is the ledger line that lands id.
func (c *nextContext) landingLine(id string) (string, error) {
	lines, err := c.ledgerLines()
	if err != nil {
		return "", err
	}
	for _, line := range lines {
		if application.LedgerLandsStory(line, id) {
			return line, nil
		}
	}
	return "", fmt.Errorf("expected a ledger line landing %s, got:\n%s\n%s", id, strings.Join(lines, "\n"), c.said())
}

func (c *nextContext) theLedgerLineSays(id, said string, others *godog.Table) error {
	line, err := c.landingLine(id)
	if err != nil {
		return err
	}
	if !strings.Contains(line, said) {
		return fmt.Errorf("expected the line landing %s to say %q, got:\n%s", id, said, line)
	}
	for _, other := range tableIDs(others) {
		line, err := c.landingLine(other)
		if err != nil {
			return err
		}
		if strings.Contains(line, said) {
			return fmt.Errorf("expected the line landing %s not to say %q, got:\n%s", other, said, line)
		}
	}
	return nil
}

func (c *nextContext) theMetricsHoldAFixedBatch(number, stories, gateRuns int, outcome string) error {
	lines, err := c.batchLines()
	if err != nil {
		return err
	}
	for _, line := range lines {
		if line.Batch != number {
			continue
		}
		if line.Stories != stories || line.GateRuns != gateRuns || !line.Fixer || line.FixerTokens != 1500 || line.Outcome != outcome {
			return fmt.Errorf("expected %d stories, %d gate runs, a fixer of 1500 tokens and outcome %q, got %+v", stories, gateRuns, outcome, line)
		}
		return nil
	}
	return fmt.Errorf("expected a line for batch %d in metrics/factory.jsonl, got %+v\n%s", number, lines, c.said())
}

func (c *nextContext) theMailSaysTheFixerWasGivenIt() error {
	mail, err := c.batchMail()
	if err != nil {
		return err
	}
	if len(mail) != 1 {
		return fmt.Errorf("expected one mail about the batch, got %+v", mail)
	}
	for _, want := range []string{application.BatchFixerName(c.rigKey(), c.batch.report.Number), "no second fixer"} {
		if !strings.Contains(mail[0].Body, want) {
			return fmt.Errorf("expected the mail to say %q, got:\n%s", want, mail[0].Body)
		}
	}
	return nil
}

// aFixerTheHarnessStops is a fixer the harness stops at once for a limit of the
// factory's, saying said, before it has done anything (mw-gq6.472).
func (c *nextContext) aFixerTheHarnessStops(said string) error {
	c.newFixer("", func(string) error { return nil })
	c.batch.fixer.stopped = said
	return nil
}

// theBatchIsHeldForTheHarness reads the batch's hold: the harness's stop and
// the time it lasts until, never the rig's tests judged on what a stopped
// fixer left.
func (c *nextContext) theBatchIsHeldForTheHarness(until string) error {
	held := c.batch.report.Held
	if held == "" || !strings.Contains(held, "harness") || !strings.Contains(held, until) {
		return fmt.Errorf("expected the batch held for the harness's stop until %s, got %q\n%s", until, held, c.said())
	}
	if strings.Contains(held, "no second fixer") {
		return fmt.Errorf("expected a hold for the harness, not a batch judged red after its fixer, got %q", held)
	}
	return nil
}

func (c *nextContext) thisHostClaimsNothingNewUntil(until string) error {
	note, err := c.tracker.Note(context.Background(), application.HarnessHoldKey(nextHost))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(note, until+" ") {
		return fmt.Errorf("expected %s held until %s, the note says %q", nextHost, until, note)
	}
	return nil
}

func (c *nextContext) theFixerWasStartedOnlyOnce() error {
	f, err := c.theFixer()
	if err != nil {
		return err
	}
	if len(f.started) != 1 {
		return fmt.Errorf("expected the fixer started once, before the limit, got %d: %+v\n%s", len(f.started), f.started, c.said())
	}
	return nil
}
