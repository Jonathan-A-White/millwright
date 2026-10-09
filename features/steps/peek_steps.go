package steps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// peekContext holds one mw peek scenario: the fake tracker, runner, harness
// transcripts and remote the use case runs against, and what it printed.
type peekContext struct {
	tracker     *apptest.FakeTracker
	runner      *apptest.FakeRunner
	transcripts *apptest.FakeTranscriptTail
	remote      *apptest.FakePeekRemote

	now          time.Time
	host         string
	rigDir       string
	storyID      string
	writesBefore int

	printed bytes.Buffer
	err     error
}

// peekStory is the story every scenario of the feature peeks at.
const peekStory = "mw-3evcnk.1"

// InitializePeekScenario registers the steps of features/peek.feature.
func InitializePeekScenario(ctx *godog.ScenarioContext) {
	c := &peekContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = peekContext{
			tracker:     apptest.NewFakeTracker(),
			runner:      apptest.NewFakeRunner(),
			transcripts: apptest.NewFakeTranscriptTail(),
			remote:      apptest.NewFakePeekRemote(),
			now:         time.Date(2026, 10, 2, 22, 3, 0, 0, time.UTC),
			storyID:     peekStory,
		}
		c.tracker.Clock = func() time.Time { return c.now }
		return ctx, nil
	})

	ctx.Given(`^the peeking host is "([^"]*)", with the rig "([^"]*)" checked out$`, c.thePeekingHostIs)
	ctx.Given(`^the peeked story "([^"]*)" is pathed to "([^"]*)"$`, c.thePeekedStoryIsPathedTo)
	ctx.Given(`^the peeked story was claimed (\d+) minutes ago and its session is running$`, c.theStoryWasClaimedAndItsSessionRuns)
	ctx.Given(`^the peeked story was claimed (\d+) minutes ago and it has no session$`, c.theStoryWasClaimedAndHasNoSession)
	ctx.Given(`^the peeked story's formula is poured with the steps "([^"]*)", "([^"]*)", "([^"]*)" and the first is closed$`, c.theFormulaIsPoured)
	ctx.Given(`^the harness has recorded (\d+) lines of transcript for the story$`, c.theHarnessHasRecorded)
	ctx.Given(`^the session's pane has printed (\d+) lines$`, c.thePaneHasPrinted)
	ctx.Given(`^the session has exited with status (\d+)$`, c.theSessionHasExited)
	ctx.Given(`^the peeked story was closed (\d+) minutes ago$`, c.theStoryWasClosed)
	ctx.Given(`^the host "([^"]*)" prints for the story:$`, c.theHostPrints)
	ctx.Given(`^the host "([^"]*)" cannot be reached, saying "([^"]*)"$`, c.theHostCannotBeReached)

	ctx.When(`^the Mayor peeks at "([^"]*)"$`, c.theMayorPeeks)

	ctx.Then(`^the peek succeeds$`, c.thePeekSucceeds)
	ctx.Then(`^the peek is refused, saying: (.+)$`, c.thePeekIsRefused)
	ctx.Then(`^the peek says "([^"]*)"$`, c.thePeekSays)
	ctx.Then(`^the peek shows the last (\d+) of the (\d+) transcript lines$`, c.thePeekShowsTranscriptLines)
	ctx.Then(`^the peek shows the last (\d+) of the (\d+) pane lines$`, c.thePeekShowsPaneLines)
	ctx.Then(`^the peek wrote nothing to the tracker$`, c.thePeekWroteNothing)
	ctx.Then(`^the peek asked the host "([^"]*)" and not this one's session$`, c.thePeekAskedOnlyTheHost)
}

func (c *peekContext) session() string { return application.SessionName(c.storyID) }

func (c *peekContext) worktree() string { return application.WorktreeDir(c.rigDir, c.storyID) }

func (c *peekContext) thePeekingHostIs(host, rig string) error {
	c.host, c.rigDir = host, "/rigs/"+rig
	c.tracker.AddEpic("peek-epic", domain.Path{Rig: rig, Branch: "main", Harness: domain.HarnessClaude})
	return nil
}

func (c *peekContext) thePeekedStoryIsPathedTo(id, host string) error {
	c.storyID = id
	c.tracker.AddStory("peek-epic", domain.Story{ID: id, Title: "Peek at a Builder", Overrides: domain.Path{Host: host}})
	return nil
}

func (c *peekContext) minutesAgo(minutes string) time.Time {
	var n int
	fmt.Sscan(minutes, &n)
	return c.now.Add(-time.Duration(n) * time.Minute)
}

func (c *peekContext) theStoryWasClaimedAndItsSessionRuns(minutes string) error {
	if err := c.claimed(minutes); err != nil {
		return err
	}
	return c.runner.Start(context.Background(), application.SessionSpec{Name: c.session(), Command: []string{"claude"}})
}

func (c *peekContext) theStoryWasClaimedAndHasNoSession(minutes string) error {
	return c.claimed(minutes)
}

func (c *peekContext) claimed(minutes string) error {
	if err := c.tracker.SetStatus(c.storyID, application.StatusInProgress); err != nil {
		return err
	}
	return c.tracker.SetStarted(c.storyID, c.minutesAgo(minutes))
}

func (c *peekContext) theFormulaIsPoured(a, b, d string) error {
	var steps []application.FormulaStep
	for _, title := range []string{a, b, d} {
		steps = append(steps, application.FormulaStep{Title: title})
	}
	c.tracker.AddFormula("tdd-feature", steps...)
	molecule, err := c.tracker.PourFormula(context.Background(), "tdd-feature", c.storyID, "Peek at a Builder", false)
	if err != nil {
		return err
	}
	if err := c.tracker.SetStoryMetadata(context.Background(), c.storyID, map[string]string{application.MoleculeField: molecule.RootID}); err != nil {
		return err
	}
	c.tracker.CloseStep(molecule.Steps[0].ID)
	return nil
}

func numberedLines(prefix string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s %d\n", prefix, i)
	}
	return b.String()
}

func (c *peekContext) theHarnessHasRecorded(n int) error {
	c.transcripts.Record(c.worktree(), numberedLines("transcript line", n))
	return nil
}

func (c *peekContext) thePaneHasPrinted(n int) error {
	c.runner.Write(c.session(), numberedLines("pane line", n))
	return nil
}

func (c *peekContext) theSessionHasExited(code int) error {
	c.runner.Exit(c.session(), code)
	return nil
}

func (c *peekContext) theStoryWasClosed(minutes string) error {
	if err := c.tracker.SetStatus(c.storyID, application.StatusClosed); err != nil {
		return err
	}
	return c.tracker.SetClosedAt(c.storyID, c.minutesAgo(minutes))
}

func (c *peekContext) theHostPrints(host string, text *godog.DocString) error {
	c.remote.Prints(host, text.Content)
	return nil
}

func (c *peekContext) theHostCannotBeReached(host, why string) error {
	c.remote.Unreachable(host, errors.New(why))
	return nil
}

func (c *peekContext) theMayorPeeks(id string) error {
	c.printed.Reset()
	c.writesBefore = c.tracker.Writes()
	c.err = application.Peek{
		Tracker:     c.tracker,
		Runner:      c.runner,
		Transcripts: c.transcripts,
		Remote:      c.remote,
		Rigs:        map[string]string{"millwright": c.rigDir},
		Host:        c.host,
		Now:         func() time.Time { return c.now },
		Out:         &c.printed,
	}.Run(context.Background(), id)
	return nil
}

func (c *peekContext) thePeekSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("expected the peek to succeed, it was refused: %v", c.err)
	}
	return nil
}

func (c *peekContext) thePeekIsRefused(saying string) error {
	if c.err == nil {
		return fmt.Errorf("expected the peek to be refused, it printed:\n%s", c.printed.String())
	}
	if !strings.Contains(c.err.Error(), saying) {
		return fmt.Errorf("expected the refusal to say %q, it said: %v", saying, c.err)
	}
	return nil
}

func (c *peekContext) thePeekSays(line string) error {
	for _, printed := range strings.Split(c.printed.String(), "\n") {
		if strings.TrimSpace(printed) == line {
			return nil
		}
	}
	return fmt.Errorf("expected a line %q in the peek, it printed:\n%s", line, c.printed.String())
}

// thePeekShowsTranscriptLines checks that exactly the last shown of total
// numbered lines are printed, and the one before them is not.
func (c *peekContext) thePeekShowsTranscriptLines(shown, total int) error {
	return c.shows("transcript line", shown, total)
}

func (c *peekContext) thePeekShowsPaneLines(shown, total int) error {
	return c.shows("pane line", shown, total)
}

func (c *peekContext) shows(prefix string, shown, total int) error {
	out := c.printed.String()
	for i := 1; i <= total; i++ {
		want := i > total-shown
		got := strings.Contains(out, fmt.Sprintf("%s %d\n", prefix, i))
		if want != got {
			return fmt.Errorf("expected %q line %d shown=%v, shown=%v; the peek printed:\n%s", prefix, i, want, got, out)
		}
	}
	return nil
}

func (c *peekContext) thePeekWroteNothing() error {
	if got := c.tracker.Writes(); got != c.writesBefore {
		return fmt.Errorf("expected the peek to write nothing, the tracker took %d writes", got-c.writesBefore)
	}
	return nil
}

func (c *peekContext) thePeekAskedOnlyTheHost(host string) error {
	if got := c.remote.Asked(); len(got) != 1 || got[0] != host {
		return fmt.Errorf("expected one ask, of %s, got %v", host, got)
	}
	if strings.Contains(c.printed.String(), "session:") && !strings.Contains(c.printed.String(), host) {
		return fmt.Errorf("the peek printed this host's session:\n%s", c.printed.String())
	}
	return nil
}
