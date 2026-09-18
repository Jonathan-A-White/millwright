package steps

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// What a sweep scenario's fixtures hold.
const sweepHost = "vps"

// sweepContext holds a fake work tracker and a fake runner: the only two
// ports mw sweep reads and writes through. Nothing here reaches the factory's
// own vault, beads database or a real tmux server.
type sweepContext struct {
	tracker *apptest.FakeTracker
	runner  *apptest.FakeRunner

	lastEpic string
	now      time.Time

	// askedBefore and namesBefore are the tracker's and runner's own logs just
	// before mw sweep last ran, so a scenario can say what changed and nothing
	// more.
	askedBefore int
	namesBefore []string

	report application.SweepReport
	err    error
}

// InitializeSweepScenario registers the steps of features/sweep.feature.
func InitializeSweepScenario(ctx *godog.ScenarioContext) {
	c := &sweepContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = sweepContext{
			tracker: apptest.NewFakeTracker(),
			runner:  apptest.NewFakeRunner(),
			now:     time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
		}
		return ctx, nil
	})

	ctx.Given(`^the sweep epic "([^"]*)" on the default path:$`, c.theSweepEpicOnTheDefaultPath)
	ctx.Given(`^a sweep story "([^"]*)" filed under it$`, c.aSweepStoryFiledUnderIt)
	ctx.Given(`^a sweep story "([^"]*)" filed under it, overriding "([^"]*)" with "([^"]*)"$`, c.aSweepStoryOverriding)
	ctx.Given(`^the sweep story "([^"]*)" is claimed with no session behind it$`, c.theSweepStoryIsClaimedWithNoSession)
	ctx.Given(`^the sweep story "([^"]*)" is claimed with its session running$`, c.theSweepStoryIsClaimedWithSessionRunning)
	ctx.Given(`^the sweep story "([^"]*)" is marked run=(\S+)$`, c.theSweepStoryIsMarkedRun)
	ctx.Given(`^the session of "([^"]*)" has printed "([^"]*)"$`, c.theSessionHasPrinted)

	ctx.When(`^mw sweep reads the host$`, c.mwSweepReadsTheHost)
	ctx.When(`^the clock advances (\d+) hours?$`, c.theClockAdvances)

	ctx.Then(`^sweeping succeeds$`, c.sweepingSucceeds)
	ctx.Then(`^the sweep story "([^"]*)" is recorded as stuck$`, c.theSweepStoryIsRecordedAsStuck)
	ctx.Then(`^the sweep story "([^"]*)" is not recorded as stuck$`, c.theSweepStoryIsNotRecordedAsStuck)
	ctx.Then(`^the sweep story "([^"]*)" carries a comment quoting: (.+)$`, c.theSweepStoryCarriesACommentQuoting)
	ctx.Then(`^the sweep story "([^"]*)" carries no comment$`, c.theSweepStoryCarriesNoComment)
	ctx.Then(`^the sweep story "([^"]*)" carries exactly (\d+) comment$`, c.theSweepStoryCarriesExactlyNComments)
	ctx.Then(`^the sweep story "([^"]*)" is still recorded run=(\S+), not stuck$`, c.theSweepStoryIsStillRecordedRun)
	ctx.Then(`^the sweep report shows "([^"]*)" on the rig "([^"]*)"$`, c.theSweepReportShowsOnTheRig)
	ctx.Then(`^nothing was written through the sweep tracker but state and comments$`, c.nothingButStateAndComments)
	ctx.Then(`^nothing was started, sent to or closed through the sweep runner$`, c.nothingStartedSentOrClosed)
}

func (c *sweepContext) theSweepEpicOnTheDefaultPath(id string, table *godog.Table) error {
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

func (c *sweepContext) aSweepStoryFiledUnderIt(id string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: id})
	return nil
}

func (c *sweepContext) aSweepStoryOverriding(id, field, value string) error {
	story := domain.Story{ID: id, Title: id}
	if err := story.Overrides.Set(field, value); err != nil {
		return err
	}
	c.tracker.AddStory(c.lastEpic, story)
	return nil
}

func (c *sweepContext) theSweepStoryIsClaimedWithNoSession(id string) error {
	return c.tracker.ClaimStory(context.Background(), id)
}

func (c *sweepContext) theSweepStoryIsClaimedWithSessionRunning(id string) error {
	ctx := context.Background()
	if err := c.tracker.ClaimStory(ctx, id); err != nil {
		return err
	}
	return c.runner.Start(ctx, application.SessionSpec{
		Name:    application.SessionName(id),
		Command: []string{"true"},
	})
}

func (c *sweepContext) theSweepStoryIsMarkedRun(id, run string) error {
	return c.tracker.SetStoryState(context.Background(), id, application.RunState, run, "recorded by the test")
}

func (c *sweepContext) theSessionHasPrinted(id, text string) error {
	c.runner.Write(application.SessionName(id), text+"\n")
	return nil
}

func (c *sweepContext) mwSweepReadsTheHost() error {
	c.askedBefore = len(c.tracker.Asked())
	c.namesBefore = c.runner.Names()
	c.report, c.err = application.Sweep{
		Tracker: c.tracker,
		Runner:  c.runner,
		Host:    sweepHost,
		Now:     func() time.Time { return c.now },
	}.Run(context.Background())
	return nil
}

func (c *sweepContext) theClockAdvances(hoursText string) error {
	hours, err := strconv.Atoi(hoursText)
	if err != nil {
		return fmt.Errorf("parsing %q as a number of hours: %w", hoursText, err)
	}
	c.now = c.now.Add(time.Duration(hours) * time.Hour)
	return nil
}

func (c *sweepContext) sweepingSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("sweeping failed: %w", c.err)
	}
	return nil
}

func (c *sweepContext) theSweepStoryIsRecordedAsStuck(id string) error {
	if err := c.sweepingSucceeds(); err != nil {
		return err
	}
	if got := c.tracker.State(id, application.RunState); got != application.RunStuck {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q", id, application.RunState, application.RunStuck, got)
	}
	for _, d := range c.report.Stuck {
		if d.Story.ID == id {
			return nil
		}
	}
	return fmt.Errorf("expected the report to name %s as newly stuck, got %+v", id, c.report.Stuck)
}

func (c *sweepContext) theSweepStoryIsNotRecordedAsStuck(id string) error {
	if err := c.sweepingSucceeds(); err != nil {
		return err
	}
	if got := c.tracker.State(id, application.RunState); got == application.RunStuck {
		return fmt.Errorf("expected %s not to be recorded stuck, but it was", id)
	}
	for _, d := range c.report.Stuck {
		if d.Story.ID == id {
			return fmt.Errorf("expected the report not to name %s as stuck, got %+v", id, c.report.Stuck)
		}
	}
	return nil
}

func (c *sweepContext) theSweepStoryCarriesACommentQuoting(id, words string) error {
	if err := c.sweepingSucceeds(); err != nil {
		return err
	}
	want := strings.TrimSpace(words)
	for _, comment := range c.tracker.Comments(id) {
		if strings.Contains(comment, want) {
			return nil
		}
	}
	return fmt.Errorf("expected %s to carry a comment quoting %q, got %v", id, want, c.tracker.Comments(id))
}

func (c *sweepContext) theSweepStoryCarriesNoComment(id string) error {
	if err := c.sweepingSucceeds(); err != nil {
		return err
	}
	if n := len(c.tracker.Comments(id)); n != 0 {
		return fmt.Errorf("expected %s to carry no comment, got %v", id, c.tracker.Comments(id))
	}
	return nil
}

func (c *sweepContext) theSweepStoryCarriesExactlyNComments(id string, wantText string) error {
	if err := c.sweepingSucceeds(); err != nil {
		return err
	}
	want, err := strconv.Atoi(wantText)
	if err != nil {
		return fmt.Errorf("parsing %q as a number of comments: %w", wantText, err)
	}
	if got := len(c.tracker.Comments(id)); got != want {
		return fmt.Errorf("expected %s to carry exactly %d comment(s), got %d: %v", id, want, got, c.tracker.Comments(id))
	}
	return nil
}

func (c *sweepContext) theSweepStoryIsStillRecordedRun(id, run string) error {
	if err := c.sweepingSucceeds(); err != nil {
		return err
	}
	if got := c.tracker.State(id, application.RunState); got != run {
		return fmt.Errorf("expected %s to still be recorded %s=%s, got %q", id, application.RunState, run, got)
	}
	return nil
}

func (c *sweepContext) theSweepReportShowsOnTheRig(id, rig string) error {
	if err := c.sweepingSucceeds(); err != nil {
		return err
	}
	for _, d := range c.report.Stuck {
		if d.Story.ID != id {
			continue
		}
		if got := d.Merged().Rig; got != rig {
			return fmt.Errorf("expected %s on the rig %q, got %q", id, rig, got)
		}
		if !strings.Contains(c.report.String(), rig) {
			return fmt.Errorf("expected the printed report to name the rig %q, got:\n%s", rig, c.report.String())
		}
		return nil
	}
	return fmt.Errorf("%s is not in the report:\n%s", id, c.report.String())
}

// nothingButStateAndComments checks that no claim was given back, no story
// was closed, and no dispatch-facing call beyond reading what is claimed and
// setting state was made.
func (c *sweepContext) nothingButStateAndComments() error {
	if err := c.sweepingSucceeds(); err != nil {
		return err
	}
	for _, id := range c.tracker.Stories() {
		detail, err := c.tracker.ShowStory(context.Background(), id)
		if err != nil {
			return err
		}
		if detail.Closed() {
			return fmt.Errorf("expected %s not to be closed, but it was", id)
		}
		if detail.Status == apptest.StatusInProgress && detail.Assignee == "" {
			return fmt.Errorf("expected %s's claim not to be given back, but its assignee is empty", id)
		}
	}
	for _, call := range c.tracker.Asked()[c.askedBefore:] {
		switch call {
		case "RunningStories", "SetStoryState":
		default:
			return fmt.Errorf("expected sweep to only read what is claimed and set state, but it called %s", call)
		}
	}
	return nil
}

func (c *sweepContext) nothingStartedSentOrClosed() error {
	if err := c.sweepingSucceeds(); err != nil {
		return err
	}
	after := c.runner.Names()
	if len(after) != len(c.namesBefore) {
		return fmt.Errorf("expected the runner's sessions to be unchanged, had %v now have %v", c.namesBefore, after)
	}
	for _, name := range after {
		if sent := c.runner.Input(name); len(sent) != 0 {
			return fmt.Errorf("expected nothing sent to session %s, got %v", name, sent)
		}
	}
	return nil
}
