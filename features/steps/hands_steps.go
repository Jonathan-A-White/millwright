package steps

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// handsContext holds the fake tracker a scenario writes steps against, and
// what the last add or list said.
type handsContext struct {
	tracker *apptest.FakeTracker
	push    *apptest.FakePosternSender
	out     bytes.Buffer
	errOut  bytes.Buffer
	err     error
}

// InitializeHandsScenario registers the steps of features/hands.feature.
func InitializeHandsScenario(ctx *godog.ScenarioContext) {
	c := &handsContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = handsContext{tracker: apptest.NewFakeTracker()}
		return ctx, nil
	})

	ctx.Given(`^a bead "([^"]*)" for the Governor's hands$`, c.aBeadForTheGovernorsHands)
	ctx.Given(`^the Mayor added the step "([^"]*)" to "([^"]*)" on "([^"]*)" as "([^"]*)" running "([^"]*)"$`, c.theMayorAddedTheStep)
	ctx.Given(`^the step "([^"]*)" on "([^"]*)" ran on "([^"]*)" with exit (\d+)$`, c.theStepRan)
	ctx.Given(`^a working push to the Governor$`, c.aWorkingPush)
	ctx.Given(`^a failing push to the Governor$`, c.aFailingPush)
	ctx.When(`^the Mayor adds the step "([^"]*)" to "([^"]*)" on "([^"]*)" as "([^"]*)" running "([^"]*)"$`, c.theMayorAddsTheStep)
	ctx.When(`^the Mayor adds the step "([^"]*)" to "([^"]*)" on "([^"]*)" as "([^"]*)" running "([^"]*)" with --no-push$`, c.theMayorAddsTheStepWithNoPush)
	ctx.Then(`^exactly one push was sent, on the thread of "([^"]*)", saying "([^"]*)"$`, c.exactlyOnePushWasSent)
	ctx.Then(`^that push's class opens Needs you$`, c.thatPushOpensNeeds)
	ctx.Then(`^no push was sent$`, c.noPushWasSent)
	ctx.Then(`^the failed push is reported on stderr$`, c.theFailedPushIsReported)
	ctx.Then(`^bead "([^"]*)"'s last comment says the push failed$`, c.lastCommentSaysPushFailed)
	ctx.When(`^the Mayor replaces the step "([^"]*)" on "([^"]*)" on "([^"]*)" as "([^"]*)" running "([^"]*)"$`, c.theMayorReplacesTheStep)
	ctx.When(`^the Mayor lists the hands steps of "([^"]*)"$`, c.theMayorListsTheHandsSteps)
	ctx.Then(`^adding the step succeeds$`, c.addingTheStepSucceeds)
	ctx.Then(`^adding the step is refused$`, c.addingTheStepIsRefused)
	ctx.Then(`^adding the step is refused, naming --replace$`, c.addingTheStepIsRefusedNamingReplace)
	ctx.Then(`^bead "([^"]*)" keeps the step "([^"]*)" running "([^"]*)"$`, c.beadKeepsTheStep)
	ctx.Then(`^bead "([^"]*)" keeps no steps$`, c.beadKeepsNoSteps)
	ctx.Then(`^bead "([^"]*)"'s last comment is the HANDS STEP "([^"]*)" on "([^"]*)" as "([^"]*)"$`, c.lastCommentIsTheHandsStep)
	ctx.Then(`^bead "([^"]*)" carries the label "([^"]*)"$`, c.beadCarriesTheLabel)
	ctx.Then(`^the list shows "([^"]*)" with its sha256 and "([^"]*)"$`, c.theListShows)
}

func (c *handsContext) aBeadForTheGovernorsHands(bead string) error {
	c.tracker.AddEpic("mw-h", domain.Path{})
	c.tracker.AddStory("mw-h", domain.Story{ID: bead, Title: "For his hands"})
	return nil
}

func (c *handsContext) add(id, bead, host, as, run string, replace, noPush bool) error {
	c.out.Reset()
	c.errOut.Reset()
	adder := application.HandsAdd{
		Tracker: c.tracker, Notes: c.tracker, Out: &c.out, Err: &c.errOut, NoPush: noPush,
		Now: func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) },
	}
	if c.push != nil {
		adder.Push = c.push
	}
	_, err := adder.Run(context.Background(), application.HandsAddRequest{
		Bead: bead, Step: domain.HandsStep{ID: id, Host: host, As: as, Run: run}, Replace: replace,
	})
	return err
}

func (c *handsContext) aWorkingPush() error {
	c.push = &apptest.FakePosternSender{}
	return nil
}

func (c *handsContext) aFailingPush() error {
	c.push = &apptest.FakePosternSender{Err: fmt.Errorf("the backend is down")}
	return nil
}

func (c *handsContext) theMayorAddsTheStepWithNoPush(id, bead, host, as, run string) error {
	c.err = c.add(id, bead, host, as, run, false, true)
	return nil
}

func (c *handsContext) exactlyOnePushWasSent(bead, text string) error {
	sent := c.push.Sent()
	if len(sent) != 1 {
		return fmt.Errorf("expected exactly one push, got %d: %+v", len(sent), sent)
	}
	if sent[0].Thread != bead || sent[0].Text != text {
		return fmt.Errorf("expected a push on the thread of %s saying %q, got %+v", bead, text, sent[0])
	}
	return nil
}

// thatPushOpensNeeds holds the class to the classes postern's
// src/push/classOptions.ts CLASS_URLS sends to /?v=needs.
func (c *handsContext) thatPushOpensNeeds() error {
	sent := c.push.Sent()
	if len(sent) == 0 {
		return fmt.Errorf("no push was sent")
	}
	switch sent[0].Class {
	case "decision-needed", "landing", "alarm":
		return nil
	}
	return fmt.Errorf("class %q does not open Needs you", sent[0].Class)
}

func (c *handsContext) noPushWasSent() error {
	if sent := c.push.Sent(); len(sent) != 0 {
		return fmt.Errorf("expected no push, got %+v", sent)
	}
	return nil
}

func (c *handsContext) theFailedPushIsReported() error {
	if !strings.Contains(c.errOut.String(), "push") || !strings.Contains(c.errOut.String(), "failed") {
		return fmt.Errorf("expected stderr to say the push failed, got %q", c.errOut.String())
	}
	return nil
}

func (c *handsContext) lastCommentSaysPushFailed(bead string) error {
	comments := c.tracker.Comments(bead)
	if len(comments) == 0 || !strings.HasPrefix(comments[len(comments)-1], "PUSH FAILED") {
		return fmt.Errorf("expected the last comment to start PUSH FAILED, got %q", comments)
	}
	return nil
}

func (c *handsContext) theMayorAddedTheStep(id, bead, host, as, run string) error {
	return c.add(id, bead, host, as, run, false, false)
}

func (c *handsContext) theMayorAddsTheStep(id, bead, host, as, run string) error {
	c.err = c.add(id, bead, host, as, run, false, false)
	return nil
}

func (c *handsContext) theMayorReplacesTheStep(id, bead, host, as, run string) error {
	c.err = c.add(id, bead, host, as, run, true, false)
	return nil
}

func (c *handsContext) theStepRan(id, bead, host string, exit int) error {
	return c.tracker.SetNote(context.Background(), application.HandsRanKey(bead, id),
		fmt.Sprintf(`{"at":"2026-09-28T12:03:00Z","exit":%d,"host":%q}`, exit, host))
}

func (c *handsContext) theMayorListsTheHandsSteps(bead string) error {
	c.out.Reset()
	_, c.err = application.HandsList{Notes: c.tracker, Out: &c.out}.Run(context.Background(), bead)
	return c.err
}

func (c *handsContext) addingTheStepSucceeds() error { return c.err }

func (c *handsContext) addingTheStepIsRefused() error {
	if c.err == nil {
		return fmt.Errorf("expected the step refused")
	}
	return nil
}

func (c *handsContext) addingTheStepIsRefusedNamingReplace() error {
	if c.err == nil || !strings.Contains(c.err.Error(), "--replace") {
		return fmt.Errorf("expected a refusal naming --replace, got %v", c.err)
	}
	return nil
}

func (c *handsContext) steps(bead string) (string, error) {
	return c.tracker.Note(context.Background(), application.HandsStepsKey(bead))
}

func (c *handsContext) beadKeepsTheStep(bead, id, run string) error {
	raw, err := c.steps(bead)
	if err != nil {
		return err
	}
	if !strings.Contains(raw, fmt.Sprintf(`"id":%q`, id)) || !strings.Contains(raw, fmt.Sprintf(`"run":%q`, run)) || strings.Count(raw, `"id":`) != 1 {
		return fmt.Errorf("expected %s to keep the one step %s running %q, got %s", bead, id, run, raw)
	}
	return nil
}

func (c *handsContext) beadKeepsNoSteps(bead string) error {
	raw, err := c.steps(bead)
	if err != nil {
		return err
	}
	if raw != "" {
		return fmt.Errorf("expected no steps on %s, got %s", bead, raw)
	}
	return nil
}

func (c *handsContext) lastCommentIsTheHandsStep(bead, id, host, as string) error {
	comments := c.tracker.Comments(bead)
	want := fmt.Sprintf("HANDS STEP %s on %s as %s:\n```sh\n", id, host, as)
	if len(comments) == 0 || !strings.HasPrefix(comments[len(comments)-1], want) {
		return fmt.Errorf("expected the last comment to start %q, got %q", want, comments)
	}
	return nil
}

func (c *handsContext) beadCarriesTheLabel(bead, label string) error {
	detail, err := c.tracker.ShowStory(context.Background(), bead)
	if err != nil {
		return err
	}
	for _, l := range detail.Labels {
		if l == label {
			return nil
		}
	}
	return fmt.Errorf("expected %s labelled %s, got %v", bead, label, detail.Labels)
}

func (c *handsContext) theListShows(id, state string) error {
	text := c.out.String()
	if !strings.Contains(text, id+" on ") || !strings.Contains(text, "sha256 ") || !strings.Contains(text, state) {
		return fmt.Errorf("expected the list to show %s, its sha256 and %q, got:\n%s", id, state, text)
	}
	return nil
}
