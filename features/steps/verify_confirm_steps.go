package steps

import (
	"bytes"
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

// verifyConfirmContext holds the fake tracker, the notes and the push sender
// mw verify confirm and mw verify list run against.
type verifyConfirmContext struct {
	beads  *apptest.FakeVerifyBeads
	notes  *apptest.FakeTracker
	sender *apptest.FakePosternSender
	out    bytes.Buffer
	err    error
}

// InitializeVerifyConfirmScenario registers the steps of features/verify_confirm.feature.
func InitializeVerifyConfirmScenario(ctx *godog.ScenarioContext) {
	c := &verifyConfirmContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = verifyConfirmContext{
			beads:  &apptest.FakeVerifyBeads{Comments: map[string][]application.Comment{}, Now: verifyMatchNow},
			notes:  apptest.NewFakeTracker(),
			sender: &apptest.FakePosternSender{},
		}
		return ctx, nil
	})

	ctx.Given(`^a confirm fixture with the demo "([^"]*)" proposed from "([^"]*)" showing "([^"]*)"$`, c.fixture)
	ctx.Given(`^the demo "([^"]*)" has a card asked under "([^"]*)"$`, c.card)
	ctx.When(`^mw verify confirm runs on "([^"]*)" with "([^"]*)" as the Mayor$`, c.confirm)
	ctx.When(`^mw verify list runs$`, c.list)
	ctx.Then(`^the confirm bead "([^"]*)" has a first comment opening VERIFIED in the standing form naming "([^"]*)" and "([^"]*)"$`, c.firstComment)
	ctx.Then(`^the confirm bead "([^"]*)" was closed$`, func(id string) error { return c.closed(id, true) })
	ctx.Then(`^the confirm bead "([^"]*)" was not closed$`, func(id string) error { return c.closed(id, false) })
	ctx.Then(`^the card of "([^"]*)" is cleared and "([^"]*)" got one reply$`, c.cardCleared)
	ctx.Then(`^the confirm is refused saying "([^"]*)"$`, c.refused)
	ctx.Then(`^no confirm comment was written$`, func() error {
		if len(c.beads.Written) != 0 {
			return fmt.Errorf("expected no comment, got %+v", c.beads.Written)
		}
		return nil
	})
	ctx.Then(`^the confirm output says "([^"]*)"$`, func(want string) error {
		if !strings.Contains(c.out.String(), want) {
			return fmt.Errorf("the output does not say %q:\n%s", want, c.out.String())
		}
		return nil
	})
}

func (c *verifyConfirmContext) fixture(id, txid, shows string) error {
	c.beads.Work.Ready = append(c.beads.Work.Ready, application.StoryDetail{
		Story: domain.Story{ID: id, Title: "Demo " + id}, Status: application.StatusOpen, EpicID: "e-c",
		Labels: []string{application.LabelDemo, "hitl"}, Description: "Open the app and look for " + id,
	})
	c.beads.Comments[id] = []application.Comment{{Author: "mw", Created: verifyMatchNow, Text: fmt.Sprintf(
		"PROPOSED VERIFIED by the Governor's use, from %s (2026-10-21 17:00Z): %s (confidence 0.90, haiku, run run-7)", txid, shows)}}
	return nil
}

func (c *verifyConfirmContext) card(id, txid string) error {
	note, err := json.Marshal(map[string]any{"txid": txid, "q": "Seen it?", "options": []string{"A: Yes"}})
	if err != nil {
		return err
	}
	return c.notes.SetNote(context.Background(), application.PosternQuestionKey(id), string(note))
}

func (c *verifyConfirmContext) confirm(id, txid string) error {
	c.err = application.VerifyConfirm{Beads: c.beads, Notes: c.notes, Sender: c.sender, Out: &c.out}.Run(
		context.Background(), application.VerifyConfirmRequest{Bead: id, Txid: txid, As: "mayor"})
	return nil
}

func (c *verifyConfirmContext) list() error {
	c.out.Reset()
	_, err := application.VerifyList{Beads: c.beads, Now: func() time.Time { return verifyMatchNow }, Out: &c.out}.Run(context.Background())
	return err
}

func (c *verifyConfirmContext) firstComment(id, txid, shows string) error {
	if c.err != nil {
		return c.err
	}
	var texts []string
	for _, w := range c.beads.Written {
		if w.ID == id {
			texts = append(texts, w.Text)
		}
	}
	want := fmt.Sprintf("VERIFIED by the Governor's use, inferred by the Mayor from %s: %s (proposed by mw verify match, run run-7)", txid, shows)
	if len(texts) == 0 || texts[0] != want {
		return fmt.Errorf("the first comment on %s is not the standing form:\n got %q\nwant %q", id, texts, want)
	}
	return nil
}

func (c *verifyConfirmContext) closed(id string, want bool) error {
	got := false
	for _, s := range c.beads.Shut {
		got = got || s.ID == id
	}
	if got != want {
		return fmt.Errorf("closed %s = %v, want %v (%+v)", id, got, want, c.beads.Shut)
	}
	return nil
}

func (c *verifyConfirmContext) cardCleared(id, txid string) error {
	if note, _ := c.notes.Note(context.Background(), application.PosternQuestionKey(id)); note != "" {
		return fmt.Errorf("the card's note is still there: %q", note)
	}
	sent := c.sender.Sent()
	if len(sent) != 1 || sent[0].Re != txid || sent[0].Thread != id {
		return fmt.Errorf("expected one reply under %s in %s's thread, got %+v", txid, id, sent)
	}
	return nil
}

func (c *verifyConfirmContext) refused(want string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected a refusal saying %q, got %v", want, c.err)
	}
	return nil
}
