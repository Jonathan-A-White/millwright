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
	// cipher and file are where a view the add publishes is sealed and kept,
	// nil when the scenario publishes none; lock is the notifier's lock.
	cipher *apptest.FakeCipher
	file   *apptest.FakeSnapshotFile
	lock   *fakeViewLock
	out    bytes.Buffer
	errOut bytes.Buffer
	err    error
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
	ctx.Given(`^the step "([^"]*)" on "([^"]*)" is superseded by "([^"]*)"$`, c.theStepIsSupersededBy)
	ctx.Given(`^a working push to the Governor$`, c.aWorkingPush)
	ctx.Given(`^a failing push to the Governor$`, c.aFailingPush)
	ctx.Given(`^a bead "([^"]*)" titled "([^"]*)"$`, c.aBeadTitled)
	ctx.Given(`^a bead "([^"]*)" with no title$`, c.aBeadWithNoTitle)
	ctx.Then(`^the push carries the summary "([^"]*)"$`, c.thePushCarriesTheSummary)
	ctx.Given(`^a view the add publishes$`, c.aViewTheAddPublishes)
	ctx.Given(`^the notifier holds the view lock$`, c.theNotifierHoldsTheViewLock)
	ctx.Given(`^the view cannot be written$`, c.theViewCannotBeWritten)
	ctx.When(`^the Mayor adds the step "([^"]*)" to "([^"]*)" on "([^"]*)" as "([^"]*)" running "([^"]*)" after "([^"]*)"$`, c.theMayorAddsTheStepAfter)
	ctx.When(`^the Mayor adds the step "([^"]*)" to "([^"]*)" on "([^"]*)" as "([^"]*)" running "([^"]*)" with --no-view$`, c.theMayorAddsTheStepWithNoView)
	ctx.Then(`^bead "([^"]*)" waits on "([^"]*)"$`, c.beadWaitsOn)
	ctx.Then(`^the published view's hands need on "([^"]*)" is not ready, waiting on "([^"]*)"$`, c.publishedNeedIsWaitingOn)
	ctx.Then(`^the published view's hands need on "([^"]*)" carries the step "([^"]*)" running "([^"]*)" with its sha256$`, c.publishedNeedCarriesTheStep)
	ctx.Then(`^no view was published$`, c.noViewWasPublished)
	ctx.Then(`^stderr warns the view was skipped$`, c.stderrWarnsViewSkipped)
	ctx.Then(`^stderr warns the view was not published$`, c.stderrWarnsViewNotPublished)
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

// handsAddOpts are the flags of a scenario's add beyond the step itself.
type handsAddOpts struct {
	replace, noPush, noView bool
	after                   []string
}

func (c *handsContext) add(id, bead, host, as, run string, replace, noPush bool) error {
	return c.addWith(id, bead, host, as, run, handsAddOpts{replace: replace, noPush: noPush})
}

func (c *handsContext) addWith(id, bead, host, as, run string, o handsAddOpts) error {
	c.out.Reset()
	c.errOut.Reset()
	adder := application.HandsAdd{
		Tracker: c.tracker, Notes: c.tracker, Out: &c.out, Err: &c.errOut, NoPush: o.noPush, NoView: o.noView,
		Now: func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) },
	}
	if c.push != nil {
		adder.Push = c.push
	}
	if c.file != nil {
		adder.View = c.view()
		adder.ViewLock = c.lock
	}
	_, err := adder.Run(context.Background(), application.HandsAddRequest{
		Bead: bead, Step: domain.HandsStep{ID: id, Host: host, As: as, Run: run}, Replace: o.replace, After: o.after,
	})
	return err
}

// view is the live view the add publishes, sealed into the fake file.
func (c *handsContext) view() application.PosternView {
	return application.PosternView{
		Tracker: c.tracker, Notes: c.tracker, Cipher: c.cipher, File: c.file,
		GovernorKey: "governor-pubkey-hex", Host: "desktop",
		Now: func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) },
	}
}

// fakeViewLock is the notifier's lock: free unless held.
type fakeViewLock struct{ held bool }

func (l *fakeViewLock) TryTake(context.Context) (func(), bool, error) {
	if l.held {
		return nil, false, nil
	}
	return func() {}, true, nil
}

func (c *handsContext) aBeadTitled(bead, title string) error {
	c.tracker.AddStory("mw-h", domain.Story{ID: bead, Title: title})
	return nil
}

func (c *handsContext) aBeadWithNoTitle(bead string) error {
	return c.aBeadTitled(bead, "")
}

func (c *handsContext) thePushCarriesTheSummary(want string) error {
	sent := c.push.Sent()
	if len(sent) != 1 {
		return fmt.Errorf("expected exactly one push, got %d: %+v", len(sent), sent)
	}
	if sent[0].Summary != want {
		return fmt.Errorf("expected the summary %q, got %q", want, sent[0].Summary)
	}
	return nil
}

func (c *handsContext) aViewTheAddPublishes() error {
	c.tracker.DescribeEpic("mw-h", "Epic mw-h", apptest.StatusOpen, 1)
	c.cipher = apptest.NewFakeCipher()
	c.file = apptest.NewFakeSnapshotFile("/state/postern/view.b64")
	c.lock = &fakeViewLock{}
	return nil
}

func (c *handsContext) theNotifierHoldsTheViewLock() error {
	c.lock.held = true
	return nil
}

func (c *handsContext) theViewCannotBeWritten() error {
	c.file.Err = fmt.Errorf("the disk is full")
	return nil
}

func (c *handsContext) theMayorAddsTheStepAfter(id, bead, host, as, run, after string) error {
	c.err = c.addWith(id, bead, host, as, run, handsAddOpts{after: []string{after}})
	return nil
}

func (c *handsContext) theMayorAddsTheStepWithNoView(id, bead, host, as, run string) error {
	c.err = c.addWith(id, bead, host, as, run, handsAddOpts{noView: true})
	return nil
}

func (c *handsContext) beadWaitsOn(bead, on string) error {
	found, err := c.tracker.ShowBeads(context.Background(), []string{bead})
	if err != nil || len(found) != 1 {
		return fmt.Errorf("reading %s: %v (%d found)", bead, err, len(found))
	}
	detail := found[0]
	for _, n := range detail.Needs {
		if n == on {
			return nil
		}
	}
	return fmt.Errorf("expected %s to wait on %s, got %v", bead, on, detail.Needs)
}

// publishedNeed is the hands need on bead in the view the add last wrote.
func (c *handsContext) publishedNeed(bead string) (application.PosternViewNeed, error) {
	if c.file == nil || c.file.Writes() == 0 {
		return application.PosternViewNeed{}, fmt.Errorf("no view was published")
	}
	var opened application.PosternViewDoc
	if err := application.OpenPosternDoc(c.cipher, "any", string(c.file.Written()), &opened); err != nil {
		return application.PosternViewNeed{}, err
	}
	for _, n := range opened.Needs {
		if n.Kind == application.PosternNeedHands && n.Bead == bead {
			return n, nil
		}
	}
	return application.PosternViewNeed{}, fmt.Errorf("the published view has no hands need on %s: %+v", bead, opened.Needs)
}

func (c *handsContext) publishedNeedIsWaitingOn(bead, title string) error {
	need, err := c.publishedNeed(bead)
	if err != nil {
		return err
	}
	if !need.NotReady || len(need.WaitingOn) != 1 || need.WaitingOn[0] != title {
		return fmt.Errorf("expected the need on %s not ready, waiting on %q, got %+v", bead, title, need)
	}
	return nil
}

func (c *handsContext) publishedNeedCarriesTheStep(bead, id, run string) error {
	need, err := c.publishedNeed(bead)
	if err != nil {
		return err
	}
	for _, step := range need.Steps {
		if step.ID != id {
			continue
		}
		if step.Run != run || step.SHA256 != domain.HandsSHA256(bead, step.HandsStep) {
			return fmt.Errorf("expected step %s running %q with its own sha256, got %+v", id, run, step)
		}
		return nil
	}
	return fmt.Errorf("the published need on %s carries no step %s: %+v", bead, id, need.Steps)
}

func (c *handsContext) noViewWasPublished() error {
	if c.file != nil && c.file.Writes() != 0 {
		return fmt.Errorf("expected no view published, got %d write(s)", c.file.Writes())
	}
	return nil
}

func (c *handsContext) stderrWarnsViewSkipped() error {
	if !strings.Contains(c.errOut.String(), "view") || !strings.Contains(c.errOut.String(), "skipped") {
		return fmt.Errorf("expected stderr to say the view was skipped, got %q", c.errOut.String())
	}
	return nil
}

func (c *handsContext) stderrWarnsViewNotPublished() error {
	if !strings.Contains(c.errOut.String(), "view") || !strings.Contains(c.errOut.String(), "not published") {
		return fmt.Errorf("expected stderr to say the view was not published, got %q", c.errOut.String())
	}
	return nil
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

func (c *handsContext) theStepIsSupersededBy(id, bead, newer string) error {
	return c.tracker.SetNote(context.Background(), application.HandsSupersededKey(bead, id), newer)
}
