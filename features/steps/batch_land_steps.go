package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The steps of mw land (features/batch_land.feature, plans/0117 Land Rush 3).
// They hang off nextContext: the stories are queued by the real close-out, and
// mw land is given the same ports mw next is, on the same real rig and origin.

// batchFixture is what one mw land of a scenario did, and the world as it was
// just before it ran, so that a step can say what the landing alone changed.
type batchFixture struct {
	report application.BatchReport
	err    error

	checksBefore int    // the rig's test runs counted before mw land
	gitBefore    int    // the lines of the git log before mw land
	mainBefore   string // main at the origin before mw land
	printedFrom  int    // where in the printed output mw land began

	// fixer is the runner mw land starts a batch's fixer through, when the
	// scenario lets it start one (features/batch_land.feature, Land Rush 5).
	fixer *fakeFixer
}

// registerBatchLandSteps registers the steps of features/batch_land.feature.
func registerBatchLandSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^these stories are queued for batch landing:$`, c.theseStoriesAreQueued)
	ctx.Given(`^"([^"]*)" is queued for batch landing, writing "([^"]*)" with words of its own$`, c.aStoryIsQueuedWriting)
	ctx.Given(`^"([^"]*)" is queued for batch landing, its branch then carrying a commit whose message says "([^"]*)"$`, c.aStoryIsQueuedThenSigned)
	ctx.Then(`^the batch set "([^"]*)" aside, naming its signed commit and the line "([^"]*)"$`, c.theBatchSetAsideSigned)
	ctx.Then(`^the Mayor's mail names "([^"]*)", its signed commit and the line "([^"]*)"$`, c.theMayorsMailNamesTheSignedCommit)
	ctx.Then(`^"([^"]*)" is not on "([^"]*)" at the rig's origin$`, c.isNotOnTheTarget)
	ctx.Given(`^the tracker acts as "([^"]*)", this host's mw$`, c.theTrackerActsAs)
	ctx.Given(`^"([^"]*)" is claimed by "([^"]*)", on another host$`, c.claimedOnAnotherHost)
	ctx.When(`^mw lands the batch of the rig$`, c.mwLandsTheBatch)
	ctx.Then(`^the story "([^"]*)" is recorded as landed and is still open$`, c.landedAndStillOpen)
	ctx.When(`^mw lands the batch of the rig with --dry-run$`, c.mwLandsTheBatchDryRun)
	ctx.Then(`^the batch landed on "([^"]*)" in one push, forcing nothing$`, c.theBatchLandedInOnePush)
	ctx.Then(`^the rig's tests were run once on the batch$`, c.theTestsRanOnceOnTheBatch)
	ctx.Then(`^the rig's tests were not run on the batch$`, c.theTestsDidNotRunOnTheBatch)
	ctx.Then(`^these stories are on "([^"]*)" at the rig's origin, closed and recorded as landed, each with a ledger line naming batch (\d+):$`, c.theseStoriesLandedInTheBatch)
	ctx.Then(`^the batch records of batch (\d+) hold the merge order, the gate log and the outcome "([^"]*)"$`, c.theBatchRecordsHold)
	ctx.Then(`^the Mayor was sent no mail saying the batch was held$`, c.noMailSaysTheBatchWasHeld)
	ctx.Then(`^the batch set "([^"]*)" aside, naming "([^"]*)" in its conflict$`, c.theBatchSetAside)
	ctx.Then(`^the story "([^"]*)" is still queued for batch landing$`, c.theStoryIsStillQueued)
	ctx.Then(`^the Mayor's mail names "([^"]*)" and its conflict in "([^"]*)"$`, c.theMayorsMailNamesTheConflict)
	ctx.Then(`^"([^"]*)" at the rig's origin is where it was before the batch$`, c.theTargetIsWhereItWas)
	ctx.Then(`^these stories are still queued for batch landing, none of them closed:$`, c.theseStoriesAreStillQueued)
	ctx.Then(`^the batch is held, and the Mayor's mail names every story in it and says "([^"]*)"$`, c.theBatchIsHeldAndMailed)
	ctx.Then(`^the dry run names "([^"]*)" then "([^"]*)" in the order it would merge them$`, c.theDryRunNamesInOrder)
	ctx.Then(`^metrics/factory.jsonl holds a line for batch (\d+) of the rig: (\d+) stories, (\d+) gate runs?, no fixer, outcome "([^"]*)"$`, c.theMetricsHoldTheBatch)
	ctx.Then(`^metrics/factory.jsonl holds no line for the rig$`, c.theMetricsHoldNoBatch)
	ctx.Then(`^no batch record was kept$`, c.noBatchRecordWasKept)
	ctx.Then(`^the newest entry of "([^"]*)" at the rig's origin is for "([^"]*)", the next for "([^"]*)"$`, c.theNewestEntriesAreFor)
	registerBatchFixSteps(ctx, c)
}

// tableIDs is the first cell of every row of a one-column table.
func tableIDs(table *godog.Table) []string {
	var ids []string
	for _, row := range table.Rows {
		if len(row.Cells) > 0 && strings.TrimSpace(row.Cells[0].Value) != "" {
			ids = append(ids, strings.TrimSpace(row.Cells[0].Value))
		}
	}
	return ids
}

// theseStoriesAreQueued works each story in its own worktree, each writing a
// file of its own, and closes it out for real: on a batch rig that queues it.
func (c *nextContext) theseStoriesAreQueued(table *godog.Table) error {
	for _, id := range tableIDs(table) {
		if err := c.theStoryHasBeenWorked(id); err != nil {
			return err
		}
		if err := c.queueByCloseOut(id); err != nil {
			return err
		}
	}
	return nil
}

// aStoryIsQueuedWriting is a story whose own commit writes file, which another
// story's work also adds with other words: the two do not merge.
func (c *nextContext) aStoryIsQueuedWriting(id, file string) error {
	if err := c.worked(id); err != nil {
		return err
	}
	dir := application.WorktreeDir(c.rig, id)
	if err := os.WriteFile(filepath.Join(dir, file), []byte("the words of "+id+", not the first story's\n"), 0o644); err != nil {
		return err
	}
	if err := commitIn(dir, "The work of "+id); err != nil {
		return err
	}
	return c.queueByCloseOut(id)
}

func (c *nextContext) queueByCloseOut(id string) error {
	if err := c.theSessionSucceeded(id); err != nil {
		return err
	}
	if err := c.mwClosesOut(id); err != nil {
		return err
	}
	if got := c.tracker.State(id, application.RunState); got != application.RunQueued {
		return fmt.Errorf("expected the close-out to queue %s, got %s=%q (it said: %v)\n%s", id, application.RunState, got, c.err, c.printed.String())
	}
	return nil
}

// theTrackerActsAs makes the tracker act under the name given, as the real
// gateway acts under this host's mw name: it claims as that name, and closes a
// story in progress only when that name holds the claim, as bd does.
func (c *nextContext) theTrackerActsAs(name string) error {
	c.tracker.ActAs(name)
	return nil
}

// claimedOnAnotherHost hands a story's claim to another host's actor, as a
// story dispatched on a cloud host and landed here is held (mw-gq6.475).
func (c *nextContext) claimedOnAnotherHost(id, holder string) error {
	return c.tracker.ClaimAs(id, holder, time.Now().Add(time.Hour))
}

func (c *nextContext) landedAndStillOpen(id string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunLanded {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q\n%s", id, application.RunState, application.RunLanded, got, c.said())
	}
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Closed() {
		return fmt.Errorf("expected %s to be still open, got %q\n%s", id, detail.Status, c.said())
	}
	return nil
}

func (c *nextContext) mwLandsTheBatch() error { return c.land(false) }

func (c *nextContext) mwLandsTheBatchDryRun() error { return c.land(true) }

// land runs mw land on the scenario's rig, built on the close-out's own ports.
func (c *nextContext) land(dryRun bool) error {
	main, err := gitSay(c.origin(), "rev-parse", "main")
	if err != nil {
		return err
	}
	c.batch = batchFixture{
		checksBefore: c.checksRun(),
		gitBefore:    len(c.gitLines()),
		mainBefore:   main,
		printedFrom:  c.printed.Len(),
		fixer:        c.batch.fixer,
	}
	next := c.nextUseCase()
	if c.batch.fixer != nil {
		next.Runner = c.batch.fixer
	}
	c.batch.report, c.batch.err = application.BatchLand{
		Next:    next,
		Records: vault.New(c.vault),
		Metrics: vault.New(c.vault),
		Fix:     c.batch.fixer != nil,
		DryRun:  dryRun,
	}.Run(context.Background(), c.rigKey())
	return nil
}

// checksRun is how many times the rig's tests have been run so far.
func (c *nextContext) checksRun() int {
	held, err := os.ReadFile(c.checkLog)
	if err != nil {
		return 0
	}
	return strings.Count(string(held), "run\n")
}

// gitLines is every git command mw has asked for so far.
func (c *nextContext) gitLines() []string {
	held, err := os.ReadFile(c.gitLog)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(held), "\n"), "\n")
}

func (c *nextContext) said() string {
	return fmt.Sprintf("mw land said: %v\n%s", c.batch.err, c.printed.String()[c.batch.printedFrom:])
}

func (c *nextContext) theBatchLandedInOnePush(branch string) error {
	if c.batch.err != nil || !c.batch.report.Landed {
		return fmt.Errorf("expected the batch to land\n%s", c.said())
	}
	pushes := 0
	for _, line := range c.gitLines()[c.batch.gitBefore:] {
		if !strings.HasPrefix(line, "push ") {
			continue
		}
		for _, forced := range []string{"--force", " -f ", "+refs/", "--mirror"} {
			if strings.Contains(line+" ", forced) {
				return fmt.Errorf("mw land forced a push: %q", line)
			}
		}
		if !strings.HasSuffix(line, "refs/heads/"+branch) {
			return fmt.Errorf("expected mw land to push only %s, got %q", branch, line)
		}
		pushes++
	}
	if pushes != 1 {
		return fmt.Errorf("expected one push of %s, got %d\n%s", branch, pushes, c.said())
	}
	now, err := gitSay(c.origin(), "rev-parse", branch)
	if err != nil {
		return err
	}
	if now != c.batch.report.Commit {
		return fmt.Errorf("expected %s at the origin to be the batch commit %s, got %s", branch, c.batch.report.Commit, now)
	}
	// A fast-forward: what main was is in the history of what it is now.
	if err := gitRun(c.origin(), "git", "merge-base", "--is-ancestor", c.batch.mainBefore, now); err != nil {
		return fmt.Errorf("expected %s to move forward from %s: %w", branch, c.batch.mainBefore, err)
	}
	return nil
}

func (c *nextContext) theTestsRanOnceOnTheBatch() error {
	if ran := c.checksRun() - c.batch.checksBefore; ran != 1 {
		return fmt.Errorf("expected the rig's tests to run once on the batch, got %d\n%s", ran, c.said())
	}
	return nil
}

func (c *nextContext) theTestsDidNotRunOnTheBatch() error {
	if ran := c.checksRun() - c.batch.checksBefore; ran != 0 {
		return fmt.Errorf("expected the rig's tests not to run, got %d runs\n%s", ran, c.said())
	}
	return nil
}

func (c *nextContext) theseStoriesLandedInTheBatch(branch string, number int, table *godog.Table) error {
	lines, err := c.ledgerLines()
	if err != nil {
		return err
	}
	batch := fmt.Sprintf("batch %d of %s", number, c.rigKey())
	for _, id := range tableIDs(table) {
		if _, err := gitSay(c.origin(), "show", branch+":"+id+".md"); err != nil {
			return fmt.Errorf("expected the work of %s on %s at the origin\n%s", id, branch, c.said())
		}
		if got := c.tracker.State(id, application.RunState); got != application.RunLanded {
			return fmt.Errorf("expected %s to be recorded %s=%s, got %q\n%s", id, application.RunState, application.RunLanded, got, c.said())
		}
		detail, err := c.tracker.ShowStory(context.Background(), id)
		if err != nil {
			return err
		}
		if !detail.Closed() {
			return fmt.Errorf("expected %s to be closed, got %q\n%s", id, detail.Status, c.said())
		}
		found := false
		for _, line := range lines {
			if application.LedgerLandsStory(line, id) && strings.Contains(line, batch) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("expected a ledger line landing %s that names %q, got:\n%s", id, batch, strings.Join(lines, "\n"))
		}
	}
	return nil
}

// batchDir is where the records of batch number of the scenario's rig are kept.
func (c *nextContext) batchDir(number int) string {
	return filepath.Join(c.vault, filepath.FromSlash(application.BatchDir), c.rigKey(), fmt.Sprint(number))
}

func (c *nextContext) theBatchRecordsHold(number int, outcome string) error {
	dir := c.batchDir(number)
	order, err := os.ReadFile(filepath.Join(dir, application.BatchOrderFile))
	if err != nil {
		return fmt.Errorf("expected the merge order of batch %d: %w\n%s", number, err, c.said())
	}
	for _, story := range c.batch.report.Queue {
		if !strings.Contains(string(order), story.ID) {
			return fmt.Errorf("expected the merge order to name %s, got:\n%s", story.ID, order)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, application.BatchGateLogFile)); err != nil {
		return fmt.Errorf("expected the gate log of batch %d: %w", number, err)
	}
	said, err := os.ReadFile(filepath.Join(dir, application.BatchOutcomeFile))
	if err != nil {
		return fmt.Errorf("expected the outcome of batch %d: %w", number, err)
	}
	if !strings.HasPrefix(string(said), outcome) {
		return fmt.Errorf("expected the outcome of batch %d to begin %q, got:\n%s", number, outcome, said)
	}
	return nil
}

// batchMail is every mail to the Mayor about a batch that did not land whole.
func (c *nextContext) batchMail() ([]application.Message, error) {
	sent, err := c.mailSent()
	if err != nil {
		return nil, err
	}
	var about []application.Message
	for _, mail := range sent {
		if strings.HasPrefix(mail.Subject, application.MailHeld+":") || strings.HasPrefix(mail.Subject, application.MailSetAside+":") {
			about = append(about, mail)
		}
	}
	return about, nil
}

func (c *nextContext) noMailSaysTheBatchWasHeld() error {
	mail, err := c.batchMail()
	if err != nil {
		return err
	}
	if len(mail) > 0 {
		return fmt.Errorf("expected no mail about a held batch, got %+v", mail)
	}
	return nil
}

func (c *nextContext) theBatchSetAside(id, file string) error {
	for _, story := range c.batch.report.SetAside {
		if story.ID != id {
			continue
		}
		if !strings.Contains(strings.Join(story.Conflicts, "\n"), file) {
			return fmt.Errorf("expected %s set aside naming %s in its conflict, got %+v", id, file, story)
		}
		return nil
	}
	return fmt.Errorf("expected %s to be set aside, got %+v\n%s", id, c.batch.report.SetAside, c.said())
}

func (c *nextContext) theStoryIsStillQueued(id string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunQueued {
		return fmt.Errorf("expected %s still %s=%s, got %q\n%s", id, application.RunState, application.RunQueued, got, c.said())
	}
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Closed() {
		return fmt.Errorf("expected %s to be open still, it is closed", id)
	}
	return nil
}

func (c *nextContext) theMayorsMailNamesTheConflict(id, file string) error {
	mail, err := c.batchMail()
	if err != nil {
		return err
	}
	for _, m := range mail {
		if m.To == "mayor" && strings.Contains(m.Body, id) && strings.Contains(m.Body, file) {
			return nil
		}
	}
	return fmt.Errorf("expected a mail to the Mayor naming %s and %s, got %+v", id, file, mail)
}

func (c *nextContext) theTargetIsWhereItWas(branch string) error {
	now, err := gitSay(c.origin(), "rev-parse", branch)
	if err != nil {
		return err
	}
	if now != c.batch.mainBefore {
		return fmt.Errorf("expected %s at the origin to stay at %s, got %s\n%s", branch, c.batch.mainBefore, now, c.said())
	}
	if c.batch.report.Landed {
		return fmt.Errorf("expected the report to say nothing landed, got %+v", c.batch.report)
	}
	return nil
}

func (c *nextContext) theseStoriesAreStillQueued(table *godog.Table) error {
	for _, id := range tableIDs(table) {
		if err := c.theStoryIsStillQueued(id); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theBatchIsHeldAndMailed(said string) error {
	if c.batch.report.Held == "" {
		return fmt.Errorf("expected the batch to be held, got %+v\n%s", c.batch.report, c.said())
	}
	mail, err := c.batchMail()
	if err != nil {
		return err
	}
	if len(mail) != 1 || mail[0].To != "mayor" || !strings.HasPrefix(mail[0].Subject, application.MailHeld+":") {
		return fmt.Errorf("expected one Held mail to the Mayor, got %+v", mail)
	}
	for _, story := range c.batch.report.Queue {
		if !strings.Contains(mail[0].Body, story.ID) {
			return fmt.Errorf("expected the mail to name %s, got:\n%s", story.ID, mail[0].Body)
		}
	}
	if len(c.batch.report.Queue) == 0 {
		return fmt.Errorf("expected the batch to have stories in it")
	}
	if !strings.Contains(mail[0].Body, said) {
		return fmt.Errorf("expected the mail to say %q, got:\n%s", said, mail[0].Body)
	}
	return nil
}

func (c *nextContext) theDryRunNamesInOrder(first, second string) error {
	if !c.batch.report.DryRun || c.batch.err != nil {
		return fmt.Errorf("expected a dry run\n%s", c.said())
	}
	out := c.printed.String()[c.batch.printedFrom:]
	i, j := strings.Index(out, first), strings.Index(out, second)
	if i < 0 || j < 0 || i > j {
		return fmt.Errorf("expected the dry run to name %s then %s, got:\n%s", first, second, out)
	}
	return nil
}

func (c *nextContext) noBatchRecordWasKept() error {
	if _, err := os.Stat(filepath.Join(c.vault, filepath.FromSlash(application.BatchDir))); !os.IsNotExist(err) {
		return fmt.Errorf("expected no batch record, found %s (%v)", application.BatchDir, err)
	}
	mail, err := c.mailSent()
	if err != nil {
		return err
	}
	if len(mail) > 0 {
		return fmt.Errorf("expected no mail from a dry run, got %+v", mail)
	}
	return nil
}

func (c *nextContext) theNewestEntriesAreFor(file, newest, next string) error {
	entries, err := c.changelogAt(file, "main")
	if err != nil {
		return err
	}
	if len(entries) < 2 || entries[0].Story != newest || entries[1].Story != next {
		return fmt.Errorf("expected the newest entries of %s to be for %s then %s, got %+v", file, newest, next, entries)
	}
	return nil
}

// batchLines are the batch lines of the rig in metrics/factory.jsonl.
func (c *nextContext) batchLines() ([]application.BatchMetric, error) {
	kept, err := vault.New(c.vault).Batches(context.Background())
	if err != nil {
		return nil, err
	}
	var ours []application.BatchMetric
	for _, line := range kept {
		if line.Rig == c.rigKey() {
			ours = append(ours, line)
		}
	}
	return ours, nil
}

func (c *nextContext) theMetricsHoldTheBatch(number, stories, gateRuns int, outcome string) error {
	lines, err := c.batchLines()
	if err != nil {
		return err
	}
	for _, line := range lines {
		if line.Batch != number {
			continue
		}
		if line.Stories != stories || line.GateRuns != gateRuns || line.Fixer || line.FixerTokens != 0 || line.Outcome != outcome {
			return fmt.Errorf("expected %d stories, %d gate runs, no fixer and outcome %q, got %+v", stories, gateRuns, outcome, line)
		}
		return nil
	}
	return fmt.Errorf("expected a line for batch %d in metrics/factory.jsonl, got %+v\n%s", number, lines, c.said())
}

func (c *nextContext) theMetricsHoldNoBatch() error {
	lines, err := c.batchLines()
	if err != nil {
		return err
	}
	if len(lines) > 0 {
		return fmt.Errorf("expected no batch line, got %+v", lines)
	}
	return nil
}

// aStoryIsQueuedThenSigned queues a story by the real close-out, which refuses
// a signed commit, and then lets its branch at the origin take one more commit
// whose message says line: how a branch signed after its close-out, or by a
// Clerk's merge resolution, reaches the queue (mw-gq6.506).
func (c *nextContext) aStoryIsQueuedThenSigned(id, line string) error {
	if err := c.theStoryHasBeenWorked(id); err != nil {
		return err
	}
	if err := c.queueByCloseOut(id); err != nil {
		return err
	}
	clone, err := os.MkdirTemp(c.root, "signer-")
	if err != nil {
		return err
	}
	branch := application.StoryBranch(id)
	if err := gitRun(clone, "git", "clone", "-q", "--branch", branch, c.origin(), clone); err != nil {
		return err
	}
	if err := gitIdentify(clone); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(clone, "signed.md"), []byte("signed\n"), 0o644); err != nil {
		return err
	}
	if err := commitIn(clone, "A later commit\n\n"+line); err != nil {
		return err
	}
	return gitRun(clone, "git", "push", "-q", "origin", branch)
}

// signedCommitOf is the short hash of the newest commit on id's branch at the
// origin.
func (c *nextContext) signedCommitOf(id string) (string, error) {
	return gitSay(c.origin(), "rev-parse", "--short", application.StoryBranch(id))
}

func (c *nextContext) theBatchSetAsideSigned(id, line string) error {
	hash, err := c.signedCommitOf(id)
	if err != nil {
		return err
	}
	for _, story := range c.batch.report.SetAside {
		if story.ID != id {
			continue
		}
		if !strings.Contains(story.Why, hash) || !strings.Contains(story.Why, line) {
			return fmt.Errorf("expected %s to be set aside naming commit %s and %q, got %q", id, hash, line, story.Why)
		}
		return nil
	}
	return fmt.Errorf("expected %s to be set aside, got %+v\n%s", id, c.batch.report.SetAside, c.said())
}

func (c *nextContext) theMayorsMailNamesTheSignedCommit(id, line string) error {
	hash, err := c.signedCommitOf(id)
	if err != nil {
		return err
	}
	mail, err := c.batchMail()
	if err != nil {
		return err
	}
	for _, m := range mail {
		if m.To == "mayor" && strings.Contains(m.Body, id) && strings.Contains(m.Body, hash) && strings.Contains(m.Body, line) {
			return nil
		}
	}
	return fmt.Errorf("expected a mail to the Mayor naming %s, commit %s and %q, got %+v", id, hash, line, mail)
}

func (c *nextContext) isNotOnTheTarget(file, branch string) error {
	if _, err := gitSay(c.origin(), "show", branch+":"+file); err == nil {
		return fmt.Errorf("expected %s not to be on %s at the origin", file, branch)
	}
	return nil
}
