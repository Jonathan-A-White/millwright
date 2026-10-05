package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// The steps of features/backend_swap.feature (mw-gq6.270): the home's tick swapping
// a staged backend itself. Everything is a fake: the build leaves nothing, the
// runner runs nothing and answers as the scenario says.

const swapCommit = "4c9db71e0f7a5b3c2d1e8f9a0b1c2d3e4f5a6b7c"

var swapNow = time.Date(2026, 10, 5, 21, 0, 0, 0, time.UTC)

type swapBuilds struct{}

func (swapBuilds) Changed(context.Context, string, string, string, string) (bool, error) {
	return true, nil
}
func (swapBuilds) Build(context.Context, string, string, string, string, string) error { return nil }

type swapLock struct{ held bool }

func (l *swapLock) TryTake(context.Context) (func(), bool, error) {
	if l.held {
		return nil, false, nil
	}
	l.held = true
	return func() { l.held = false }, true, nil
}

type backendSwapContext struct {
	tracker *apptest.FakeTracker
	runner  *apptest.FakeHandsRunner
	said    *apptest.FakePosternSender
	stage   application.BackendStage
	bead    string
}

// InitializeBackendSwapScenario registers the steps of features/backend_swap.feature.
func InitializeBackendSwapScenario(ctx *godog.ScenarioContext) {
	c := &backendSwapContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = backendSwapContext{}
		return ctx, nil
	})

	ctx.Given(`^a home that has staged the swap of a Postern landing$`, c.aHomeThatHasStaged)
	ctx.Given(`^the Governor is in a Talk$`, c.theGovernorIsInATalk)
	ctx.Given(`^the swap's health check will fail and put the old backend back$`, c.theHealthCheckWillFail)
	ctx.Given(`^the rig keeps the tap$`, c.theRigKeepsTheTap)
	ctx.When(`^the Talk ends$`, c.theTalkEnds)
	ctx.When(`^the home's tick runs$`, c.theHomesTickRuns)
	ctx.Then(`^the swap ran once$`, c.theSwapRanOnce)
	ctx.Then(`^the swap did not run$`, c.theSwapDidNotRun)
	ctx.Then(`^the Governor was told once, on the swap bead's channel, "([^"]*)"$`, c.toldOnce)
	ctx.Then(`^the Governor was told once, on the swap bead's channel, that the swap failed and the old backend was put back$`, c.toldOfTheFailure)
	ctx.Then(`^the swap bead is closed$`, c.theSwapBeadIsClosed)
	ctx.Then(`^the swap bead is open, hitl, and carries the output as a comment$`, c.openHitlWithOutput)
	ctx.Then(`^the swap bead is open, hitl, and carries no output$`, c.openHitlWithoutOutput)
}

func (c *backendSwapContext) aHomeThatHasStaged() error {
	c.tracker = apptest.NewFakeTracker()
	c.tracker.AddEpic("mw-j0f2d", domain.Path{})
	c.tracker.AddStory("mw-j0f2d", domain.Story{ID: "mw-j0f2d.28", Title: "Show the Mayor's presence"})
	c.runner = &apptest.FakeHandsRunner{Outcome: application.HandsOutcome{Exit: 0, Output: "backend 4c9db71 is live and answering\n"}}
	c.said = &apptest.FakePosternSender{}
	c.stage = application.BackendStage{
		Rigs: map[string]string{"postern": "/rigs/postern"},
		Settings: map[string]application.BackendRig{"postern": {
			Dir: "server", Build: "go build -o {out} ./cmd/postern", Stage: "/stage", Live: "/live/postern",
			Service: "postern-backend", Health: "https://postern.example.org/api/healthz",
		}},
		Builds:  swapBuilds{},
		Home:    &apptest.FakeHomeFile{Text: "laptop 2026-10-01T05:00:00Z mayor\n"},
		Host:    "laptop",
		Tracker: c.tracker,
		Notes:   c.tracker,
		Hands:   application.HandsAdd{Tracker: c.tracker, Notes: c.tracker, Now: func() time.Time { return swapNow }},
		Runner:  c.runner,
		Say:     c.said,
		Lock:    &swapLock{},
		Now:     func() time.Time { return swapNow },
	}
	return nil
}

// landed stages the landing, once the scenario's settings are all in.
func (c *backendSwapContext) landed() error {
	if c.bead != "" {
		return nil
	}
	c.stage.Landed(context.Background(), application.BackendLanding{
		Rig: "postern", Story: "mw-j0f2d.28", Title: "Show the Mayor's presence", Epic: "mw-j0f2d", Commit: swapCommit,
	})
	detail, err := c.tracker.ShowEpic(context.Background(), "mw-j0f2d")
	if err != nil {
		return err
	}
	for _, s := range detail.Stories {
		if s.Story.ID != "mw-j0f2d.28" {
			c.bead = s.Story.ID
		}
	}
	if c.bead == "" {
		return fmt.Errorf("expected the landing to file a swap bead under the epic")
	}
	return nil
}

func (c *backendSwapContext) talk(role string) error {
	return c.tracker.SetNote(context.Background(), application.TalkLastKey,
		fmt.Sprintf(`{"id":"talk-1","role":%q,"at":%d}`, role, swapNow.Add(-time.Minute).Unix()))
}

func (c *backendSwapContext) theGovernorIsInATalk() error { return c.talk(application.TalkRoleTurn) }
func (c *backendSwapContext) theTalkEnds() error          { return c.talk(application.TalkRoleEnd) }

func (c *backendSwapContext) theHealthCheckWillFail() error {
	c.runner.Outcome = application.HandsOutcome{Exit: 1, Output: "curl: (22) the health url said 502\nFAILED after 4 tries: putting the old backend back\n"}
	return nil
}

func (c *backendSwapContext) theRigKeepsTheTap() error {
	cfg := c.stage.Settings["postern"]
	cfg.Swap = application.BackendSwapHands
	c.stage.Settings["postern"] = cfg
	return nil
}

func (c *backendSwapContext) theHomesTickRuns() error {
	if err := c.landed(); err != nil {
		return err
	}
	c.stage.Pending(context.Background())
	return nil
}

func (c *backendSwapContext) theSwapRanOnce() error {
	if n := len(c.runner.Jobs()); n != 1 {
		return fmt.Errorf("expected the swap to run once, it ran %d times", n)
	}
	return nil
}

func (c *backendSwapContext) theSwapDidNotRun() error {
	if err := c.landed(); err != nil {
		return err
	}
	if n := len(c.runner.Jobs()); n != 0 {
		return fmt.Errorf("expected the swap not to run, it ran %d times", n)
	}
	return nil
}

func (c *backendSwapContext) toldOnce(text string) error {
	sent := c.said.Sent()
	if len(sent) != 1 || sent[0].Thread != c.bead || sent[0].Text != text {
		return fmt.Errorf("expected %q said once on %s's channel, got %+v", text, c.bead, sent)
	}
	return nil
}

func (c *backendSwapContext) toldOfTheFailure() error {
	sent := c.said.Sent()
	if len(sent) != 1 || sent[0].Thread != c.bead || !strings.Contains(sent[0].Text, "failed") || !strings.Contains(sent[0].Text, "old backend was put back") {
		return fmt.Errorf("expected the failure said once on %s's channel, got %+v", c.bead, sent)
	}
	return nil
}

func (c *backendSwapContext) detail() (application.StoryDetail, error) {
	return c.tracker.ShowStory(context.Background(), c.bead)
}

func (c *backendSwapContext) theSwapBeadIsClosed() error {
	d, err := c.detail()
	if err != nil {
		return err
	}
	if d.Status != application.StatusClosed {
		return fmt.Errorf("expected %s closed, it is %q", c.bead, d.Status)
	}
	return nil
}

func (c *backendSwapContext) openHitl(output bool) error {
	d, err := c.detail()
	if err != nil {
		return err
	}
	if d.Status == application.StatusClosed || !d.Hitl() {
		return fmt.Errorf("expected %s open and hitl, got %q %v", c.bead, d.Status, d.Labels)
	}
	var kept bool
	for _, comment := range c.tracker.Comments(c.bead) {
		kept = kept || strings.Contains(comment, "health url said 502")
	}
	if kept != output {
		return fmt.Errorf("expected the output on the bead to be %v, it is %v: %q", output, kept, c.tracker.Comments(c.bead))
	}
	return nil
}

func (c *backendSwapContext) openHitlWithOutput() error    { return c.openHitl(true) }
func (c *backendSwapContext) openHitlWithoutOutput() error { return c.openHitl(false) }
