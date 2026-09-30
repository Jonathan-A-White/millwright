package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// posternViewFeatureNow is the clock features/postern_view.feature builds its
// view with.
var posternViewFeatureNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// posternViewContext holds the fake tracker a scenario files beads against,
// the view the last build produced, and where a run wrote it.
type posternViewContext struct {
	tracker *apptest.FakeTracker
	cipher  *apptest.FakeCipher
	file    *apptest.FakeSnapshotFile
	doc     application.PosternViewDoc
	err     error
	// readsCounted is the comment reads of a bead a scenario counted.
	readsCounted int
}

// InitializePosternViewScenario registers the steps of
// features/postern_view.feature.
func InitializePosternViewScenario(ctx *godog.ScenarioContext) {
	c := &posternViewContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = posternViewContext{
			tracker: apptest.NewFakeTracker(),
			cipher:  apptest.NewFakeCipher(),
			file:    apptest.NewFakeSnapshotFile("/state/postern/view.b64"),
		}
		return ctx, nil
	})

	ctx.Given(`^the view's epic "([^"]*)" is live$`, c.theViewsEpicIsLive)
	ctx.Given(`^the view's bead "([^"]*)" under "([^"]*)" is labelled "([^"]*)"$`, c.theViewsBeadIsLabelled)
	ctx.Given(`^the view's bead "([^"]*)" under "([^"]*)" waits on "([^"]*)"$`, c.theViewsBeadWaitsOn)
	ctx.Given(`^the view's bead "([^"]*)" under "([^"]*)" landed an hour ago$`, c.theViewsBeadLandedAnHourAgo)
	ctx.Given(`^the view's hands bead "([^"]*)" under "([^"]*)" closed an hour ago$`, c.theViewsHandsBeadClosedAnHourAgo)
	ctx.Given(`^the view's bead "([^"]*)" under "([^"]*)" landed a month ago$`, c.theViewsBeadLandedAMonthAgo)
	ctx.Given(`^the view's bead "([^"]*)" under "([^"]*)" landed two hours ago with the Mayor's comment "([^"]*)"$`, c.theViewsBeadLandedTwoHoursAgoChecked)
	ctx.Given(`^the view's bead "([^"]*)" under "([^"]*)" has a postern question open since two hours ago$`, c.theViewsBeadHasAQuestionOpen)

	ctx.Given(`^the view's bead "([^"]*)" has the hands step "([^"]*)" on "([^"]*)" as "([^"]*)" running "([^"]*)"$`, c.theViewsBeadHasTheHandsStep)
	ctx.Given(`^the view's hands step "([^"]*)" on "([^"]*)" ran with exit (\d+)$`, c.theViewsHandsStepRan)
	ctx.Then(`^the view's hands need on "([^"]*)" carries the step "([^"]*)" with its sha256, run with exit (\d+)$`, c.theViewsHandsNeedCarriesTheStep)
	ctx.Given(`^the view's held story "([^"]*)" under "([^"]*)" was filed (\d+) days? ago$`, c.theViewsHeldStoryWasFiled)
	ctx.Given(`^the view's held story "([^"]*)" under "([^"]*)" waits on "([^"]*)"$`, c.theViewsHeldStoryWaitsOn)
	ctx.Then(`^the view's approve need on "([^"]*)" says "([^"]*)"$`, c.theViewsApproveNeedSays)
	ctx.Then(`^the view's approve need on "([^"]*)" offers "([^"]*)"$`, c.theViewsApproveNeedOffers)
	ctx.Then(`^the view's approve need on "([^"]*)" offers nothing$`, c.theViewsApproveNeedOffersNothing)
	ctx.Given(`^the view's hitl bead "([^"]*)" under "([^"]*)" was filed (\d+) days? ago$`, c.theViewsHitlBeadWasFiled)
	ctx.Given(`^the view's bead "([^"]*)" has the comment "([^"]*)"$`, c.theViewsBeadHasTheComment)
	ctx.Given(`^the view's bead "([^"]*)" has the comment "([^"]*)" dated (\d+) days? ago$`, c.theViewsBeadHasTheCommentDated)
	ctx.Given(`^the view's bead "([^"]*)" has a comment of (\d+) letters$`, c.theViewsBeadHasALongComment)
	ctx.Given(`^the view's bead "([^"]*)" is kept until (\d+) days? ahead$`, c.theViewsBeadIsKeptAhead)
	ctx.Given(`^the view's bead "([^"]*)" is kept until (\d+) days? ago$`, c.theViewsBeadIsKeptAgo)
	ctx.Then(`^the view's needs on "([^"]*)" are "([^"]*)"$`, c.theViewsNeedsOnAre)
	ctx.Then(`^the view's stale need on "([^"]*)" says "([^"]*)"$`, c.theViewsStaleNeedSays)
	ctx.Then(`^the view's stale need on "([^"]*)" has the options "([^"]*)" and has been stale since "([^"]*)"$`, c.theViewsStaleNeedOptions)
	ctx.Then(`^the view's stale need on "([^"]*)" quotes (\d+) letters of its newest comment$`, c.theViewsStaleNeedQuotes)
	ctx.Given(`^the view's story "([^"]*)" under "([^"]*)" is open$`, c.theViewsStoryIsOpen)
	ctx.Given(`^the view's hitl bead "([^"]*)" under "([^"]*)" waits on "([^"]*)"$`, c.theViewsHitlBeadWaitsOn)
	ctx.Given(`^the view's host "([^"]*)" last synced (\d+) minutes ago with work pathed to it$`, c.theViewsHostLastSynced)
	ctx.Given(`^the view's story "([^"]*)" under "([^"]*)" has used all (\d+) attempts$`, c.theViewsStoryUsedAllAttempts)
	ctx.Then(`^the view's hands need on "([^"]*)" waits for "([^"]*)"$`, c.theViewsHandsNeedWaitsFor)
	ctx.Then(`^the view's need "([^"]*)" on "([^"]*)" waits for "([^"]*)"$`, c.theViewsNeedWaitsFor)
	ctx.Then(`^the view's alarm for "([^"]*)" waits for "([^"]*)"$`, c.theViewsAlarmWaitsFor)
	ctx.Then(`^the view's hands need on "([^"]*)" is not ready, waiting on "([^"]*)"$`, c.theViewsHandsNeedIsNotReady)
	ctx.Then(`^the view's hands need on "([^"]*)" is ready, saying "([^"]*)"$`, c.theViewsHandsNeedIsReadySaying)
	ctx.Then(`^the view's hands need on "([^"]*)" is ready$`, c.theViewsHandsNeedIsReady)
	ctx.When(`^the live view is built$`, c.theLiveViewIsBuilt)
	ctx.When(`^the live view is run and written$`, c.theLiveViewIsRunAndWritten)

	ctx.Then(`^the view's needs are "([^"]*)"$`, c.theViewsNeedsAre)
	ctx.Then(`^the view's verify need on "([^"]*)" says "([^"]*)"$`, c.theViewsVerifyNeedSays)
	ctx.Then(`^the view's verify need on "([^"]*)" offers "([^"]*)"$`, c.theViewsVerifyNeedOffers)
	ctx.Then(`^the view's verify need on "([^"]*)" offers nothing$`, c.theViewsVerifyNeedOffersNothing)
	ctx.Then(`^the view's verify need on "([^"]*)" is waiting on "([^"]*)"$`, c.theViewsVerifyNeedIsWaitingOn)
	ctx.When(`^the view's reads of the comments of "([^"]*)" are counted$`, c.theViewsReadsAreCounted)
	ctx.Then(`^the view has read the comments of "([^"]*)" no more than counted$`, c.theViewReadNoMoreThanCounted)
	ctx.Then(`^the view's need on "([^"]*)" blocks (\d+) beads?$`, c.theViewsNeedBlocks)
	ctx.Then(`^the view's bead "([^"]*)" waits on "([^"]*)"$`, c.theViewsBeadWaitsOnInTheView)
	ctx.Then(`^the view has no bead "([^"]*)"$`, c.theViewHasNoBead)
	ctx.Then(`^the view's epic "([^"]*)" has (\d+) done earlier$`, c.theViewsEpicHasDoneEarlier)
	ctx.Then(`^the written view opens, from gzip, to a v (\d+) view holding "([^"]*)"$`, c.theWrittenViewOpens)
	ctx.Then(`^the view read no comments of "([^"]*)" or "([^"]*)"$`, c.theViewReadNoCommentsOf)
}

func (c *posternViewContext) theViewsEpicIsLive(id string) error {
	c.tracker.AddEpic(id, domain.Path{})
	c.tracker.DescribeEpic(id, "Epic "+id, apptest.StatusOpen, 1)
	return nil
}

func (c *posternViewContext) story(id, epic string) {
	c.tracker.AddStory(epic, domain.Story{ID: id, Title: "Story " + id})
	_ = c.tracker.CommentOnStory(context.Background(), id, "a word on "+id)
}

func (c *posternViewContext) theViewsBeadIsLabelled(id, epic, label string) error {
	c.story(id, epic)
	return c.tracker.SetLabels(id, label)
}

func (c *posternViewContext) theViewsBeadWaitsOn(id, epic, on string) error {
	c.story(id, epic)
	c.tracker.Needs(id, on)
	return nil
}

func (c *posternViewContext) closeAt(id, epic string, at time.Time) error {
	c.tracker.AddStory(epic, domain.Story{ID: id, Title: "Story " + id})
	// Closed as mw next closes a landing: run:landed is recorded first.
	if err := c.tracker.SetStoryState(context.Background(), id, application.RunState, application.RunLanded, "landed"); err != nil {
		return err
	}
	if err := c.tracker.SetStatus(id, apptest.StatusClosed); err != nil {
		return err
	}
	return c.tracker.SetClosedAt(id, at)
}

func (c *posternViewContext) theViewsBeadLandedAnHourAgo(id, epic string) error {
	return c.closeAt(id, epic, posternViewFeatureNow.Add(-time.Hour))
}

func (c *posternViewContext) theViewsHandsBeadClosedAnHourAgo(id, epic string) error {
	if err := c.closeAt(id, epic, posternViewFeatureNow.Add(-time.Hour)); err != nil {
		return err
	}
	return c.tracker.SetLabels(id, "hitl")
}

func (c *posternViewContext) theViewsBeadLandedTwoHoursAgoChecked(id, epic, comment string) error {
	if err := c.closeAt(id, epic, posternViewFeatureNow.Add(-2*time.Hour)); err != nil {
		return err
	}
	return c.tracker.CommentOnStory(context.Background(), id, strings.ReplaceAll(comment, `\n`, "\n"))
}

func (c *posternViewContext) theViewsVerifyNeedSays(bead, want string) error {
	for _, n := range c.doc.Needs {
		if n.Kind == application.PosternNeedVerify && n.Bead == bead {
			if n.Text != want {
				return fmt.Errorf("expected the verify need on %s to say %q, got %q", bead, want, n.Text)
			}
			return nil
		}
	}
	return fmt.Errorf("the view has no verify need on %s", bead)
}

func (c *posternViewContext) verifyNeed(bead string) (application.PosternViewNeed, error) {
	for _, n := range c.doc.Needs {
		if n.Kind == application.PosternNeedVerify && n.Bead == bead {
			return n, nil
		}
	}
	return application.PosternViewNeed{}, fmt.Errorf("the view has no verify need on %s", bead)
}

func (c *posternViewContext) theViewsVerifyNeedOffers(bead, option string) error {
	n, err := c.verifyNeed(bead)
	if err != nil {
		return err
	}
	if strings.Join(n.Options, ", ") != option {
		return fmt.Errorf("expected the verify need on %s to offer %q, got %q", bead, option, n.Options)
	}
	return nil
}

func (c *posternViewContext) theViewsVerifyNeedOffersNothing(bead string) error {
	n, err := c.verifyNeed(bead)
	if err != nil {
		return err
	}
	if len(n.Options) != 0 {
		return fmt.Errorf("expected the verify need on %s to offer nothing, got %q", bead, n.Options)
	}
	return nil
}

func (c *posternViewContext) theViewsVerifyNeedIsWaitingOn(bead, waitingOn string) error {
	n, err := c.verifyNeed(bead)
	if err != nil {
		return err
	}
	if !n.NotReady || strings.Join(n.WaitingOn, ", ") != waitingOn {
		return fmt.Errorf("expected the verify need on %s not ready, waiting on %q, got not_ready %v waiting on %q", bead, waitingOn, n.NotReady, n.WaitingOn)
	}
	return nil
}

func (c *posternViewContext) theViewsReadsAreCounted(bead string) error {
	c.readsCounted = c.tracker.CommentReads(bead)
	return nil
}

func (c *posternViewContext) theViewReadNoMoreThanCounted(bead string) error {
	if reads := c.tracker.CommentReads(bead); reads != c.readsCounted {
		return fmt.Errorf("expected %s's comments read %d times, got %d", bead, c.readsCounted, reads)
	}
	return nil
}

func (c *posternViewContext) theViewsBeadLandedAMonthAgo(id, epic string) error {
	return c.closeAt(id, epic, posternViewFeatureNow.Add(-30*24*time.Hour))
}

func (c *posternViewContext) theViewsBeadHasAQuestionOpen(id, epic string) error {
	c.tracker.AddStory(epic, domain.Story{ID: id, Title: "Which way?"})
	note, err := json.Marshal(map[string]any{
		"txid": "q-" + id, "options": []string{"left", "right"}, "q": "Which way?", "rec": "left",
		"asked": posternViewFeatureNow.Add(-2 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return c.tracker.SetNote(context.Background(), application.PosternQuestionKey(id), string(note))
}

func (c *posternViewContext) view() application.PosternView {
	return application.PosternView{
		Tracker: c.tracker, Notes: c.tracker, Cipher: c.cipher, File: c.file,
		GovernorKey: "governor-pubkey-hex", Host: "desktop",
		Now: func() time.Time { return posternViewFeatureNow },
	}
}

func (c *posternViewContext) theLiveViewIsBuilt() error {
	c.doc, c.err = c.view().Build(context.Background())
	return c.err
}

func (c *posternViewContext) theLiveViewIsRunAndWritten() error {
	c.doc, c.err = c.view().Run(context.Background())
	return c.err
}

func (c *posternViewContext) theViewsNeedsAre(wantCSV string) error {
	var got []string
	for _, n := range c.doc.Needs {
		got = append(got, n.Kind+":"+n.Bead)
	}
	if strings.Join(got, ", ") != wantCSV {
		return fmt.Errorf("expected needs %q, got %q", wantCSV, strings.Join(got, ", "))
	}
	return nil
}

func (c *posternViewContext) theViewsNeedBlocks(bead string, want int) error {
	for _, n := range c.doc.Needs {
		if n.Bead == bead {
			if n.Blocks != want {
				return fmt.Errorf("expected the need on %s to block %d, got %d", bead, want, n.Blocks)
			}
			return nil
		}
	}
	return fmt.Errorf("the view has no need on %s", bead)
}

func (c *posternViewContext) bead(id string) (application.PosternViewBead, bool) {
	for _, b := range c.doc.Beads {
		if b.ID == id {
			return b, true
		}
	}
	return application.PosternViewBead{}, false
}

func (c *posternViewContext) theViewsBeadWaitsOnInTheView(id, on string) error {
	b, ok := c.bead(id)
	if !ok {
		return fmt.Errorf("the view has no bead %s", id)
	}
	if strings.Join(b.Waits, ",") != on {
		return fmt.Errorf("expected %s to wait on %s, got %v", id, on, b.Waits)
	}
	return nil
}

func (c *posternViewContext) theViewHasNoBead(id string) error {
	if _, ok := c.bead(id); ok {
		return fmt.Errorf("expected no bead %s in the view", id)
	}
	return nil
}

func (c *posternViewContext) theViewsEpicHasDoneEarlier(id string, want int) error {
	b, ok := c.bead(id)
	if !ok {
		return fmt.Errorf("the view has no epic %s", id)
	}
	if b.DoneEarlier != want {
		return fmt.Errorf("expected %s to have %d done earlier, got %d", id, want, b.DoneEarlier)
	}
	return nil
}

func (c *posternViewContext) theWrittenViewOpens(version int, id string) error {
	text, _, err := c.cipher.Decrypt("any", string(c.file.Written()))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(text, "\x1f\x8b") {
		return fmt.Errorf("expected the sealed plaintext to be gzip")
	}
	var opened application.PosternViewDoc
	if err := application.OpenPosternDoc(c.cipher, "any", string(c.file.Written()), &opened); err != nil {
		return err
	}
	if opened.V != version {
		return fmt.Errorf("expected a v %d view, got v %d", version, opened.V)
	}
	for _, b := range opened.Beads {
		if b.ID == id {
			return nil
		}
	}
	return fmt.Errorf("expected the written view to hold %s", id)
}

func (c *posternViewContext) theViewReadNoCommentsOf(a, b string) error {
	for _, id := range []string{a, b} {
		if reads := c.tracker.CommentReads(id); reads != 0 {
			return fmt.Errorf("expected %s's comments never read, got %d reads", id, reads)
		}
	}
	return nil
}

func (c *posternViewContext) theViewsBeadHasTheHandsStep(bead, id, host, as, run string) error {
	ctx := context.Background()
	var records []application.HandsStepRecord
	if raw, err := c.tracker.Note(ctx, application.HandsStepsKey(bead)); err != nil {
		return err
	} else if raw != "" {
		if err := json.Unmarshal([]byte(raw), &records); err != nil {
			return err
		}
	}
	records = append(records, application.HandsStepRecord{HandsStep: domain.HandsStep{ID: id, Host: host, As: as, Run: run}})
	steps, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return c.tracker.SetNote(ctx, application.HandsStepsKey(bead), string(steps))
}

func (c *posternViewContext) theViewsHandsStepRan(id, bead string, exit int) error {
	return c.tracker.SetNote(context.Background(), application.HandsRanKey(bead, id),
		fmt.Sprintf(`{"at":"2026-09-28T11:50:00Z","exit":%d,"host":"desktop"}`, exit))
}

func (c *posternViewContext) theViewsHandsNeedCarriesTheStep(bead, id string, exit int) error {
	for _, n := range c.doc.Needs {
		if n.Kind != application.PosternNeedHands || n.Bead != bead {
			continue
		}
		for _, step := range n.Steps {
			if step.ID != id {
				continue
			}
			if step.SHA256 != domain.HandsSHA256(bead, step.HandsStep) {
				return fmt.Errorf("expected %s's sha256 over its canonical bytes, got %s", id, step.SHA256)
			}
			if step.Ran == nil || step.Ran.Exit != exit {
				return fmt.Errorf("expected %s run with exit %d, got %+v", id, exit, step.Ran)
			}
			return nil
		}
		return fmt.Errorf("the hands need on %s carries no step %s: %+v", bead, id, n.Steps)
	}
	return fmt.Errorf("the view has no hands need on %s", bead)
}

func posternViewDaysAgo(days int) time.Time {
	return posternViewFeatureNow.Add(-time.Duration(days) * 24 * time.Hour)
}

func (c *posternViewContext) theViewsHeldStoryWasFiled(id, epic string, days int) error {
	c.tracker.AddStory(epic, domain.Story{ID: id, Title: "Story " + id})
	if err := c.tracker.SetStatus(id, apptest.StatusDeferred); err != nil {
		return err
	}
	return c.tracker.SetCreated(id, posternViewDaysAgo(days))
}

func (c *posternViewContext) theViewsHeldStoryWaitsOn(id, epic, on string) error {
	c.tracker.AddStory(epic, domain.Story{ID: id, Title: "Story " + id})
	c.tracker.Needs(id, on)
	return c.tracker.SetStatus(id, apptest.StatusDeferred)
}

func (c *posternViewContext) theViewsApproveNeedSays(bead, want string) error {
	n, err := c.needOf(application.PosternNeedApprove, bead)
	if err != nil {
		return err
	}
	if n.Text != want {
		return fmt.Errorf("expected the approve need on %s to say %q, got %q", bead, want, n.Text)
	}
	return nil
}

func (c *posternViewContext) theViewsApproveNeedOffers(bead, want string) error {
	n, err := c.needOf(application.PosternNeedApprove, bead)
	if err != nil {
		return err
	}
	if strings.Join(n.Options, ", ") != want {
		return fmt.Errorf("expected the approve need on %s to offer %q, got %v", bead, want, n.Options)
	}
	return nil
}

func (c *posternViewContext) theViewsApproveNeedOffersNothing(bead string) error {
	n, err := c.needOf(application.PosternNeedApprove, bead)
	if err != nil {
		return err
	}
	if len(n.Options) != 0 {
		return fmt.Errorf("expected the approve need on %s to offer nothing, got %v", bead, n.Options)
	}
	return nil
}

func (c *posternViewContext) theViewsHitlBeadWasFiled(id, epic string, days int) error {
	c.tracker.AddStory(epic, domain.Story{ID: id, Title: "Story " + id})
	if err := c.tracker.SetLabels(id, "hitl"); err != nil {
		return err
	}
	return c.tracker.SetCreated(id, posternViewDaysAgo(days))
}

func (c *posternViewContext) theViewsBeadHasTheComment(id, text string) error {
	return c.tracker.CommentOnStory(context.Background(), id, text)
}

func (c *posternViewContext) theViewsBeadHasTheCommentDated(id, text string, days int) error {
	return c.tracker.CommentOnStoryAt(id, text, posternViewDaysAgo(days))
}

func (c *posternViewContext) theViewsBeadHasALongComment(id string, letters int) error {
	return c.tracker.CommentOnStory(context.Background(), id, strings.Repeat("x", letters))
}

func (c *posternViewContext) keep(id string, until time.Time) error {
	return c.tracker.SetNote(context.Background(), application.PosternKeepKey(id), until.Format(time.RFC3339))
}

func (c *posternViewContext) theViewsBeadIsKeptAhead(id string, days int) error {
	return c.keep(id, posternViewFeatureNow.Add(time.Duration(days)*24*time.Hour))
}

func (c *posternViewContext) theViewsBeadIsKeptAgo(id string, days int) error {
	return c.keep(id, posternViewDaysAgo(days))
}

func (c *posternViewContext) theViewsNeedsOnAre(bead, want string) error {
	var got []string
	for _, n := range c.doc.Needs {
		if n.Bead == bead {
			got = append(got, n.Kind+":"+n.Bead)
		}
	}
	if want == "none" {
		want = ""
	}
	if strings.Join(got, ", ") != want {
		return fmt.Errorf("expected the needs on %s to be %q, got %q", bead, want, strings.Join(got, ", "))
	}
	return nil
}

func (c *posternViewContext) staleNeed(bead string) (application.PosternViewNeed, error) {
	for _, n := range c.doc.Needs {
		if n.Kind == application.PosternNeedStale && n.Bead == bead {
			return n, nil
		}
	}
	return application.PosternViewNeed{}, fmt.Errorf("the view has no stale need on %s", bead)
}

func (c *posternViewContext) theViewsStaleNeedSays(bead, want string) error {
	n, err := c.staleNeed(bead)
	if err != nil {
		return err
	}
	if n.Text != want {
		return fmt.Errorf("expected the stale need on %s to say %q, got %q", bead, want, n.Text)
	}
	return nil
}

func (c *posternViewContext) theViewsStaleNeedOptions(bead, options, since string) error {
	n, err := c.staleNeed(bead)
	if err != nil {
		return err
	}
	if strings.Join(n.Options, ", ") != options {
		return fmt.Errorf("expected the options %q, got %v", options, n.Options)
	}
	if n.Since != since {
		return fmt.Errorf("expected the stale need stale since %s, got %s", since, n.Since)
	}
	return nil
}

func (c *posternViewContext) theViewsStaleNeedQuotes(bead string, letters int) error {
	n, err := c.staleNeed(bead)
	if err != nil {
		return err
	}
	if got := strings.Count(n.Text, "x"); got != letters {
		return fmt.Errorf("expected %d letters of the comment in %q, got %d", letters, n.Text, got)
	}
	return nil
}

func (c *posternViewContext) theViewsStoryIsOpen(id, epic string) error {
	c.story(id, epic)
	return nil
}

func (c *posternViewContext) theViewsHitlBeadWaitsOn(id, epic, on string) error {
	c.story(id, epic)
	c.tracker.Needs(id, on)
	return c.tracker.SetLabels(id, "hitl")
}

func (c *posternViewContext) theViewsHostLastSynced(host string, minutes int) error {
	c.tracker.AddStory("mw-v", domain.Story{ID: "mw-" + host + ".1", Title: "On the " + host, Overrides: domain.Path{Host: host}})
	at := posternViewFeatureNow.Add(-time.Duration(minutes) * time.Minute)
	return c.tracker.SetNote(context.Background(), application.LastSyncKey(host), at.Format(time.RFC3339))
}

func (c *posternViewContext) theViewsStoryUsedAllAttempts(id, epic string, attempts int) error {
	c.tracker.AddStory(epic, domain.Story{ID: id, Title: "Story " + id})
	n := fmt.Sprint(attempts)
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{
		application.AttemptsField: n, application.AttemptsExhaustedField: n})
}

func (c *posternViewContext) needOf(kind, bead string) (application.PosternViewNeed, error) {
	for _, n := range c.doc.Needs {
		if n.Kind == kind && n.Bead == bead {
			return n, nil
		}
	}
	return application.PosternViewNeed{}, fmt.Errorf("the view has no %s need on %q", kind, bead)
}

func (c *posternViewContext) needWaitsFor(kind, bead, want string) error {
	n, err := c.needOf(kind, bead)
	if err != nil {
		return err
	}
	if n.WaitsFor != want {
		return fmt.Errorf("expected the %s need on %s to wait for %q, got %q", kind, bead, want, n.WaitsFor)
	}
	return nil
}

func (c *posternViewContext) theViewsHandsNeedWaitsFor(bead, want string) error {
	return c.needWaitsFor(application.PosternNeedHands, bead, want)
}

func (c *posternViewContext) theViewsNeedWaitsFor(kind, bead, want string) error {
	return c.needWaitsFor(kind, bead, want)
}

func (c *posternViewContext) theViewsAlarmWaitsFor(host, want string) error {
	for _, n := range c.doc.Needs {
		if n.Kind == application.PosternNeedAlarm && n.Bead == "" && strings.Contains(n.Title, host) {
			if n.WaitsFor != want {
				return fmt.Errorf("expected the alarm for %s to wait for %q, got %q", host, want, n.WaitsFor)
			}
			return nil
		}
	}
	return fmt.Errorf("the view has no alarm for host %s", host)
}

func (c *posternViewContext) theViewsHandsNeedIsNotReady(bead, waitingOn string) error {
	n, err := c.needOf(application.PosternNeedHands, bead)
	if err != nil {
		return err
	}
	if !n.NotReady || strings.Join(n.WaitingOn, ", ") != waitingOn {
		return fmt.Errorf("expected the hands need on %s not ready, waiting on %q, got not_ready %v waiting on %q", bead, waitingOn, n.NotReady, n.WaitingOn)
	}
	return nil
}

func (c *posternViewContext) theViewsHandsNeedIsReadySaying(bead, text string) error {
	n, err := c.needOf(application.PosternNeedHands, bead)
	if err != nil {
		return err
	}
	if n.NotReady || len(n.WaitingOn) != 0 || n.Text != text {
		return fmt.Errorf("expected the hands need on %s ready, saying %q, got not_ready %v waiting on %q saying %q", bead, text, n.NotReady, n.WaitingOn, n.Text)
	}
	return nil
}

func (c *posternViewContext) theViewsHandsNeedIsReady(bead string) error {
	n, err := c.needOf(application.PosternNeedHands, bead)
	if err != nil {
		return err
	}
	if n.NotReady || len(n.WaitingOn) != 0 {
		return fmt.Errorf("expected the hands need on %s ready, got not_ready %v waiting on %q", bead, n.NotReady, n.WaitingOn)
	}
	return nil
}
