package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// verifyMatchNow is the clock of these scenarios.
var verifyMatchNow = time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)

// verifyMatchContext holds the fake tracker, the stand-in agent, the mailbox and
// the inbox and run directories mw verify match runs against.
type verifyMatchContext struct {
	beads   *apptest.FakeVerifyBeads
	grinder *apptest.FakeGrinder
	mail    *apptest.FakeMailbox
	inbox   string
	runs    string
	answers []map[string]any
	out     bytes.Buffer
	err     error
}

// InitializeVerifyMatchScenario registers the steps of features/verify_match.feature.
func InitializeVerifyMatchScenario(ctx *godog.ScenarioContext) {
	c := &verifyMatchContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = verifyMatchContext{
			beads:   &apptest.FakeVerifyBeads{Comments: map[string][]application.Comment{}, Now: verifyMatchNow},
			grinder: &apptest.FakeGrinder{},
			mail:    apptest.NewFakeMailbox(),
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		os.RemoveAll(c.inbox)
		os.RemoveAll(c.runs)
		return ctx, nil
	})

	ctx.Given(`^a verify fixture with the demos "([^"]*)" and "([^"]*)"$`, c.demos)
	ctx.Given(`^a verify landing "([^"]*)" closed a day ago whose closing comment says how to check it$`, c.landing)
	ctx.Given(`^a verify landing "([^"]*)" closed (\d+) minutes ago whose closing comment says how to check it$`, c.landingMinutesAgo)
	ctx.Given(`^a saved message of his "([^"]*)" and a saved picture "([^"]*)"$`, c.evidence)
	ctx.Given(`^the verify stand-in answers "([^"]*)" from "([^"]*)" showing "([^"]*)" at (\d+\.\d+)$`, c.standInAnswers)
	ctx.When(`^mw verify match runs$`, func() error { return c.run(false) })
	ctx.When(`^mw verify match runs as a dry run$`, func() error { return c.run(true) })
	ctx.Then(`^the verify bead "([^"]*)" has one comment opening PROPOSED that names "([^"]*)" and "([^"]*)"$`, c.hasOneProposal)
	ctx.Then(`^the verify bead "([^"]*)" has no new comment$`, c.hasNoComment)
	ctx.Then(`^the Mayor has one verify mail saying "([^"]*)" with the line "([^"]*)"$`, c.mailSays)
	ctx.Then(`^the verify run's record keeps the answer for "([^"]*)" as left out$`, c.recordKeeps)
	ctx.Then(`^the verify run's record keeps the answer for "([^"]*)" from "([^"]*)" as dropped for being older than the landing$`, c.recordDropped)
	ctx.Then(`^the verify output says "([^"]*)"$`, func(want string) error {
		if !strings.Contains(c.out.String(), want) {
			return fmt.Errorf("the output does not say %q:\n%s", want, c.out.String())
		}
		return nil
	})
	ctx.Then(`^the verify stand-in was called once$`, func() error { return c.calls(1) })
	ctx.Then(`^the verify stand-in was not called$`, func() error { return c.calls(0) })
	ctx.Then(`^no verify bead was closed$`, c.noneClosed)
	ctx.Then(`^the verify output names "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)" and "([^"]*)" and the dir's size$`, c.outputNames)
	ctx.Then(`^no verify comment was written$`, func() error {
		if len(c.beads.Written) != 0 {
			return fmt.Errorf("expected no comment, got %+v", c.beads.Written)
		}
		return nil
	})
	ctx.Then(`^the Mayor has no verify mail$`, func() error {
		if c.mail.Writes() != 0 {
			return fmt.Errorf("expected no mail, got %d", c.mail.Writes())
		}
		return nil
	})
}

func (c *verifyMatchContext) demos(first, second string) error {
	for i, id := range []string{first, second} {
		c.beads.Work.Ready = append(c.beads.Work.Ready, application.StoryDetail{
			Story: domain.Story{ID: id, Title: "Demo " + id}, Status: application.StatusOpen, EpicID: "e-" + strconv.Itoa(i),
			Labels: []string{application.LabelDemo, "hitl"}, Description: "Open the app and look for " + id,
		})
	}
	return nil
}

func (c *verifyMatchContext) landing(id string) error { return c.landingAgo(id, 24*time.Hour) }

func (c *verifyMatchContext) landingMinutesAgo(id string, minutes int) error {
	return c.landingAgo(id, time.Duration(minutes)*time.Minute)
}

func (c *verifyMatchContext) landingAgo(id string, ago time.Duration) error {
	c.beads.Closed = append(c.beads.Closed, application.ClosedStory{
		ID: id, Title: "Story " + id, Type: "task", CloseReason: "landed on main", ClosedAt: verifyMatchNow.Add(-ago),
	})
	c.beads.Comments[id] = []application.Comment{{Author: "mw", Created: verifyMatchNow, Text: "Done.\n\nHOW TO CHECK IT, for the Governor:\n1. Tap Save.\n\nFor the rig memory:\nnothing"}}
	return nil
}

func (c *verifyMatchContext) evidence(message, picture string) error {
	var err error
	if c.inbox, err = os.MkdirTemp("", "mw-verify-inbox-"); err != nil {
		return err
	}
	if c.runs, err = os.MkdirTemp("", "mw-verify-runs-"); err != nil {
		return err
	}
	for name, age := range map[string]time.Duration{message + ".txt": time.Hour, picture + ".jpg": 2 * time.Hour} {
		path := filepath.Join(c.inbox, name)
		if err := os.WriteFile(path, []byte("what he sent: "+name), 0o600); err != nil {
			return err
		}
		at := verifyMatchNow.Add(-age)
		if err := os.Chtimes(path, at, at); err != nil {
			return err
		}
	}
	return nil
}

func (c *verifyMatchContext) standInAnswers(bead, txid, shows, confidence string) error {
	level, err := strconv.ParseFloat(confidence, 64)
	if err != nil {
		return err
	}
	c.answers = append(c.answers, map[string]any{"bead": bead, "txid": txid, "shows": shows, "confidence": level})
	return nil
}

func (c *verifyMatchContext) run(dry bool) error {
	raw, err := json.Marshal(map[string]any{"matches": c.answers})
	if err != nil {
		return err
	}
	c.grinder.Result = application.SessionResult{Answer: raw}
	_, c.err = application.VerifyMatch{
		Beads: c.beads, Grinder: c.grinder, Mailbox: c.mail,
		Settings: application.VerifySettings{Model: "haiku", Effort: "medium", Confidence: 0.8},
		InboxDir: c.inbox, RunDir: c.runs, Host: "laptop", Now: func() time.Time { return verifyMatchNow }, Out: &c.out,
	}.Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour, DryRun: dry})
	return c.err
}

// written are the comments written on bead.
func (c *verifyMatchContext) written(bead string) []string {
	var texts []string
	for _, w := range c.beads.Written {
		if w.ID == bead {
			texts = append(texts, w.Text)
		}
	}
	return texts
}

func (c *verifyMatchContext) hasOneProposal(bead, txid, shows string) error {
	texts := c.written(bead)
	if len(texts) != 1 {
		return fmt.Errorf("expected one comment on %s, got %q", bead, texts)
	}
	text := texts[0]
	if !strings.HasPrefix(text, "PROPOSED ") || !strings.Contains(text, txid) || !strings.Contains(text, shows) {
		return fmt.Errorf("the comment on %s is not the proposal asked for: %q", bead, text)
	}
	return nil
}

func (c *verifyMatchContext) hasNoComment(bead string) error {
	if texts := c.written(bead); len(texts) != 0 {
		return fmt.Errorf("expected no comment on %s, got %q", bead, texts)
	}
	return nil
}

func (c *verifyMatchContext) mailSays(subject, line string) error {
	inbox, err := c.mail.Inbox(context.Background(), "mayor")
	if err != nil {
		return err
	}
	if len(inbox) != 1 {
		return fmt.Errorf("expected one mail to mayor, got %d", len(inbox))
	}
	if inbox[0].Subject != subject || !strings.Contains(inbox[0].Body, line) {
		return fmt.Errorf("the mail does not say %q with %q: %+v", subject, line, inbox[0])
	}
	return nil
}

func (c *verifyMatchContext) recordKeeps(bead string) error {
	names, err := filepath.Glob(filepath.Join(c.runs, "*.json"))
	if err != nil || len(names) != 1 {
		return fmt.Errorf("expected one run record, got %v (%v)", names, err)
	}
	raw, err := os.ReadFile(names[0])
	if err != nil {
		return err
	}
	var record struct {
		Matches []struct{ Bead, Outcome string } `json:"matches"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return err
	}
	for _, m := range record.Matches {
		if m.Bead == bead {
			if !strings.HasPrefix(m.Outcome, "left out") {
				return fmt.Errorf("the record's outcome for %s is %q", bead, m.Outcome)
			}
			return nil
		}
	}
	return fmt.Errorf("the record has no answer for %s: %s", bead, raw)
}

func (c *verifyMatchContext) recordDropped(bead, txid string) error {
	names, err := filepath.Glob(filepath.Join(c.runs, "*.json"))
	if err != nil || len(names) != 1 {
		return fmt.Errorf("expected one run record, got %v (%v)", names, err)
	}
	raw, err := os.ReadFile(names[0])
	if err != nil {
		return err
	}
	var record struct {
		Dropped int                                    `json:"dropped"`
		Matches []struct{ Bead, Txid, Outcome string } `json:"matches"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return err
	}
	for _, m := range record.Matches {
		if m.Bead == bead && m.Txid == txid {
			if !strings.Contains(m.Outcome, "older than the landing") || record.Dropped != 1 {
				return fmt.Errorf("the record's outcome for %s from %s is %q, dropped %d", bead, txid, m.Outcome, record.Dropped)
			}
			return nil
		}
	}
	return fmt.Errorf("the record has no answer for %s from %s: %s", bead, txid, raw)
}

func (c *verifyMatchContext) calls(want int) error {
	if got := len(c.grinder.Calls()); got != want {
		return fmt.Errorf("expected the stand-in called %d times, got %d", want, got)
	}
	return nil
}

// noneClosed: the run's port cannot close a bead; it must have written only
// proposals, and every demo is still open.
func (c *verifyMatchContext) noneClosed() error {
	for _, w := range c.beads.Written {
		if !strings.HasPrefix(w.Text, "PROPOSED ") {
			return fmt.Errorf("a comment that is not a proposal: %q", w.Text)
		}
	}
	for _, d := range c.beads.Work.Ready {
		if d.Status != application.StatusOpen {
			return fmt.Errorf("%s is %s", d.Story.ID, d.Status)
		}
	}
	return nil
}

func (c *verifyMatchContext) outputNames(a, b, d, e, f string) error {
	said := c.out.String()
	for _, want := range []string{a, b, d, e, f, "bytes"} {
		if !strings.Contains(said, want) {
			return fmt.Errorf("the output does not say %q:\n%s", want, said)
		}
	}
	return nil
}
