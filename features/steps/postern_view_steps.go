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
	ctx.Given(`^the view's bead "([^"]*)" under "([^"]*)" landed a month ago$`, c.theViewsBeadLandedAMonthAgo)
	ctx.Given(`^the view's bead "([^"]*)" under "([^"]*)" landed two hours ago with the Mayor's comment "([^"]*)"$`, c.theViewsBeadLandedTwoHoursAgoChecked)
	ctx.Given(`^the view's bead "([^"]*)" under "([^"]*)" has a postern question open since two hours ago$`, c.theViewsBeadHasAQuestionOpen)

	ctx.Given(`^the view's bead "([^"]*)" has the hands step "([^"]*)" on "([^"]*)" as "([^"]*)" running "([^"]*)"$`, c.theViewsBeadHasTheHandsStep)
	ctx.Given(`^the view's hands step "([^"]*)" on "([^"]*)" ran with exit (\d+)$`, c.theViewsHandsStepRan)
	ctx.Then(`^the view's hands need on "([^"]*)" carries the step "([^"]*)" with its sha256, run with exit (\d+)$`, c.theViewsHandsNeedCarriesTheStep)
	ctx.When(`^the live view is built$`, c.theLiveViewIsBuilt)
	ctx.When(`^the live view is run and written$`, c.theLiveViewIsRunAndWritten)

	ctx.Then(`^the view's needs are "([^"]*)"$`, c.theViewsNeedsAre)
	ctx.Then(`^the view's verify need on "([^"]*)" says "([^"]*)"$`, c.theViewsVerifyNeedSays)
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
	if err := c.tracker.SetStatus(id, apptest.StatusClosed); err != nil {
		return err
	}
	return c.tracker.SetClosedAt(id, at)
}

func (c *posternViewContext) theViewsBeadLandedAnHourAgo(id, epic string) error {
	return c.closeAt(id, epic, posternViewFeatureNow.Add(-time.Hour))
}

func (c *posternViewContext) theViewsBeadLandedTwoHoursAgoChecked(id, epic, comment string) error {
	if err := c.closeAt(id, epic, posternViewFeatureNow.Add(-2*time.Hour)); err != nil {
		return err
	}
	return c.tracker.CommentOnStory(context.Background(), id, comment)
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
	steps, err := json.Marshal([]application.HandsStepRecord{{HandsStep: domain.HandsStep{ID: id, Host: host, As: as, Run: run}}})
	if err != nil {
		return err
	}
	return c.tracker.SetNote(context.Background(), application.HandsStepsKey(bead), string(steps))
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
