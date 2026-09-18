package steps

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// What a status scenario's fixtures hold.
const (
	statusHost = "vps"
	statusSeat = "builder"
)

// statusContext holds a fake work tracker and a fake runner standing in for
// the two ports mw status must never write through, and a real vault
// directory in a temp dir for the one thing it does read: the seat's ledger.
// Nothing here reaches the factory's own vault, beads database or terminal.
type statusContext struct {
	root string

	tracker *apptest.FakeTracker
	runner  *apptest.FakeRunner

	lastEpic string
	now      time.Time
	// askedBefore is how many calls the tracker had logged just before mw
	// status ran, so that a scenario can say what it asked and nothing more.
	askedBefore int

	report application.StatusReport
	err    error
}

// InitializeStatusScenario registers the steps of features/status.feature.
func InitializeStatusScenario(ctx *godog.ScenarioContext) {
	c := &statusContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = statusContext{
			tracker: apptest.NewFakeTracker(),
			runner:  apptest.NewFakeRunner(),
			now:     time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^the status epic "([^"]*)" on the default path:$`, c.theStatusEpicOnTheDefaultPath)
	ctx.Given(`^a status story "([^"]*)" filed under it$`, c.aStatusStoryFiledUnderIt)
	ctx.Given(`^a status story "([^"]*)" titled "([^"]*)" filed under it$`, c.aStatusStoryTitledFiledUnderIt)
	ctx.Given(`^a status story "([^"]*)" filed under it, overriding "([^"]*)" with "([^"]*)"$`, c.aStatusStoryOverriding)
	ctx.Given(`^a status story "([^"]*)" filed under it, waiting on "([^"]*)"$`, c.aStatusStoryWaitingOn)
	ctx.Given(`^the status story "([^"]*)" is claimed with its session running$`, c.theStatusStoryIsClaimedAndRunning)
	ctx.Given(`^the status story "([^"]*)" is marked run=(\S+)$`, c.theStatusStoryIsMarkedRun)
	ctx.Given(`^the formula poured for "([^"]*)" has a step still open$`, c.theFormulaPouredHasAStepStillOpen)
	ctx.Given(`^the builder's ledger holds a line from (today|\d{4}-\d{2}-\d{2}) burning (\d+) tokens$`,
		c.theBuildersLedgerHoldsALineBurning)

	ctx.When(`^mw status reads the host$`, c.mwStatusReadsTheHost)

	ctx.Then(`^reading status succeeds$`, c.readingStatusSucceeds)
	ctx.Then(`^the report shows "([^"]*)" running with session "([^"]*)"$`, c.theReportShowsRunningWithSession)
	ctx.Then(`^the report lists "([^"]*)" as ready$`, c.theReportListsAsReady)
	ctx.Then(`^the report lists "([^"]*)" as blocked$`, c.theReportListsAsBlocked)
	ctx.Then(`^the report shows "([^"]*)" on the rig "([^"]*)"$`, c.theReportShowsOnTheRig)
	ctx.Then(`^the report says the close-out of "([^"]*)" is blocked by an open formula step$`,
		c.theReportSaysCloseOutBlockedByFormula)
	ctx.Then(`^the report shows "([^"]*)" as run=(\S+), not running$`, c.theReportShowsRunState)
	ctx.Then(`^the report says today's fuel is (.+)$`, c.theReportSaysTodaysFuelIs)
	ctx.Then(`^every line of the report is at most 60 columns wide$`, c.everyLineIsAtMost60ColumnsWide)
	ctx.Then(`^nothing was written through the tracker, the ledger or the runner$`, c.nothingWasWritten)
}

// workspace makes the temp directory this scenario keeps its vault in, once.
func (c *statusContext) workspace() (string, error) {
	if c.root != "" {
		return c.root, nil
	}
	root, err := os.MkdirTemp("", "mw-status-")
	if err != nil {
		return "", fmt.Errorf("making a workspace: %w", err)
	}
	c.root = root
	return root, nil
}

func (c *statusContext) theStatusEpicOnTheDefaultPath(id string, table *godog.Table) error {
	defaults := domain.Path{}
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("a default path row needs a field and a value, got %d cells", len(row.Cells))
		}
		if err := defaults.Set(row.Cells[0].Value, row.Cells[1].Value); err != nil {
			return err
		}
	}
	c.tracker.AddEpic(id, defaults)
	c.lastEpic = id
	return nil
}

func (c *statusContext) aStatusStoryFiledUnderIt(id string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: id})
	return nil
}

func (c *statusContext) aStatusStoryTitledFiledUnderIt(id, title string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: title})
	return nil
}

func (c *statusContext) aStatusStoryOverriding(id, field, value string) error {
	story := domain.Story{ID: id, Title: id}
	if err := story.Overrides.Set(field, value); err != nil {
		return err
	}
	c.tracker.AddStory(c.lastEpic, story)
	return nil
}

func (c *statusContext) aStatusStoryWaitingOn(id, need string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: id})
	c.tracker.Needs(id, need)
	return nil
}

func (c *statusContext) theStatusStoryIsClaimedAndRunning(id string) error {
	ctx := context.Background()
	if err := c.tracker.ClaimStory(ctx, id); err != nil {
		return err
	}
	return c.tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "dispatched by the test")
}

func (c *statusContext) theStatusStoryIsMarkedRun(id, run string) error {
	return c.tracker.SetStoryState(context.Background(), id, application.RunState, run, "recorded by the test")
}

// theFormulaPouredHasAStepStillOpen pours a formula for a story the way a
// dispatch does, and closes every step but the last: a session that stopped
// short of finishing it.
func (c *statusContext) theFormulaPouredHasAStepStillOpen(id string) error {
	ctx := context.Background()
	c.tracker.AddFormula("tdd-feature",
		application.FormulaStep{Title: "Understand the story"},
		application.FormulaStep{Title: "Write the failing feature"},
		application.FormulaStep{Title: "Implement until green"},
	)
	molecule, err := c.tracker.PourFormula(ctx, "tdd-feature", id, "The story "+id)
	if err != nil {
		return err
	}
	if err := c.tracker.SetStoryMetadata(ctx, id, map[string]string{application.MoleculeField: molecule.RootID}); err != nil {
		return err
	}
	for _, step := range molecule.Steps[:len(molecule.Steps)-1] {
		c.tracker.CloseStep(step.ID)
	}
	return nil
}

// theBuildersLedgerHoldsALineBurning appends one real ledger line to a real
// vault directory in this scenario's own temp workspace, dated either today
// (the clock mw status is given) or a literal date, burning the tokens given.
func (c *statusContext) theBuildersLedgerHoldsALineBurning(when, tokensText string) error {
	tokens, err := strconv.Atoi(tokensText)
	if err != nil {
		return fmt.Errorf("the token count %q is not a number: %w", tokensText, err)
	}
	date := c.now
	if when != "today" {
		parsed, err := time.Parse(application.LedgerDate, when)
		if err != nil {
			return fmt.Errorf("parsing the date %q: %w", when, err)
		}
		date = parsed
	}
	line := application.LedgerLine{
		When:    date,
		StoryID: "mw-old.1",
		Title:   "an earlier story",
		Outcome: "landed on main",
		Path:    domain.Path{Model: domain.ModelOpus, Effort: domain.EffortHigh},
		Result:  application.SessionResult{Fuel: application.Fuel{Input: tokens}},
	}.String()

	dir, err := c.workspace()
	if err != nil {
		return err
	}
	return vault.New(dir).AppendToLedger(context.Background(), statusSeat, line)
}

func (c *statusContext) mwStatusReadsTheHost() error {
	dir, err := c.workspace()
	if err != nil {
		return err
	}
	c.askedBefore = len(c.tracker.Asked())
	c.report, c.err = application.Status{
		Tracker: c.tracker,
		Vault:   vault.New(dir),
		Host:    statusHost,
		Seat:    statusSeat,
		Now:     func() time.Time { return c.now },
	}.Run(context.Background())
	return nil
}

func (c *statusContext) readingStatusSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("reading status failed: %w", c.err)
	}
	return nil
}

// storyIn is the detail the report holds for a story, wherever it is listed.
func (c *statusContext) storyIn(id string) (application.StoryDetail, bool) {
	for _, rs := range c.report.Running {
		if rs.Detail.Story.ID == id {
			return rs.Detail, true
		}
	}
	for _, d := range c.report.Ready {
		if d.Story.ID == id {
			return d, true
		}
	}
	for _, d := range c.report.Blocked {
		if d.Story.ID == id {
			return d, true
		}
	}
	return application.StoryDetail{}, false
}

func (c *statusContext) runningIn(id string) (application.RunningStory, bool) {
	for _, rs := range c.report.Running {
		if rs.Detail.Story.ID == id {
			return rs, true
		}
	}
	return application.RunningStory{}, false
}

func (c *statusContext) theReportShowsRunningWithSession(id, session string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	rs, ok := c.runningIn(id)
	if !ok {
		return fmt.Errorf("%s is not listed as running:\n%s", id, c.report.String())
	}
	if rs.Session != session {
		return fmt.Errorf("expected %s to run under session %q, got %q", id, session, rs.Session)
	}
	if !strings.Contains(c.report.String(), session) {
		return fmt.Errorf("expected the printed report to name the session %q, got:\n%s", session, c.report.String())
	}
	return nil
}

func (c *statusContext) theReportListsAsReady(id string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, d := range c.report.Ready {
		if d.Story.ID == id {
			return nil
		}
	}
	return fmt.Errorf("%s is not listed as ready:\n%s", id, c.report.String())
}

func (c *statusContext) theReportListsAsBlocked(id string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, d := range c.report.Blocked {
		if d.Story.ID == id {
			return nil
		}
	}
	return fmt.Errorf("%s is not listed as blocked:\n%s", id, c.report.String())
}

func (c *statusContext) theReportShowsOnTheRig(id, rig string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	detail, ok := c.storyIn(id)
	if !ok {
		return fmt.Errorf("%s is not in the report:\n%s", id, c.report.String())
	}
	if got := detail.Merged().Rig; got != rig {
		return fmt.Errorf("expected %s on the rig %q, got %q", id, rig, got)
	}
	if !strings.Contains(c.report.String(), rig) {
		return fmt.Errorf("expected the printed report to name the rig %q, got:\n%s", rig, c.report.String())
	}
	return nil
}

func (c *statusContext) theReportSaysCloseOutBlockedByFormula(id string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	rs, ok := c.runningIn(id)
	if !ok {
		return fmt.Errorf("%s is not listed as running:\n%s", id, c.report.String())
	}
	if !rs.FormulaOpen() {
		return fmt.Errorf("expected %s to show an open formula step, got %+v", id, rs)
	}
	if !strings.Contains(c.report.String(), "formula") {
		return fmt.Errorf("expected the printed report to say the formula is open, got:\n%s", c.report.String())
	}
	return nil
}

func (c *statusContext) theReportShowsRunState(id, run string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	rs, ok := c.runningIn(id)
	if !ok {
		return fmt.Errorf("%s is not listed as running:\n%s", id, c.report.String())
	}
	if rs.Run != run {
		return fmt.Errorf("expected %s to be recorded run=%s, got %q", id, run, rs.Run)
	}
	if !rs.Stopped() {
		return fmt.Errorf("expected %s to be shown as not running, got %+v", id, rs)
	}
	if !strings.Contains(c.report.String(), run) {
		return fmt.Errorf("expected the printed report to say run=%s, got:\n%s", run, c.report.String())
	}
	return nil
}

func (c *statusContext) theReportSaysTodaysFuelIs(want string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	got := fmt.Sprintf("%s tokens", application.Thousands(c.report.FuelToday))
	if got != want {
		return fmt.Errorf("expected today's fuel to be %q, got %q", want, got)
	}
	if !strings.Contains(c.report.String(), got) {
		return fmt.Errorf("expected the printed report to say %q, got:\n%s", got, c.report.String())
	}
	return nil
}

func (c *statusContext) everyLineIsAtMost60ColumnsWide() error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, line := range strings.Split(c.report.String(), "\n") {
		if n := utf8.RuneCountInString(line); n > application.Width {
			return fmt.Errorf("expected every line at most %d columns, got %d in %q", application.Width, n, line)
		}
	}
	return nil
}

func (c *statusContext) nothingWasWritten() error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, call := range c.tracker.Asked()[c.askedBefore:] {
		if call != "RunningStories" && call != "ReadyForHost" {
			return fmt.Errorf("expected mw status to only read the tracker, but it called %s", call)
		}
	}
	if names := c.runner.Names(); len(names) != 0 {
		return fmt.Errorf("expected nothing started through the runner, got %v", names)
	}
	return nil
}
