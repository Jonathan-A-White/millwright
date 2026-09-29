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

// tidyContext holds a fake mailbox, a fake tracker (beads and the notes) and a
// fake tick log: the three things mw tidy reads and writes through. Nothing
// here reaches the factory's own vault or beads database.
type tidyContext struct {
	mail    *apptest.FakeMailbox
	tracker *apptest.FakeTracker
	log     *apptest.FakeTickLog

	now time.Time
	ids map[string]string // subject -> mail id

	// written is what the tracker had been written to when tidy ran: the
	// fixtures are writes too, and what a run adds is what a scenario counts.
	written int

	report application.TidyReport
	err    error
}

// InitializeTidyScenario registers the steps of features/tidy.feature.
func InitializeTidyScenario(ctx *godog.ScenarioContext) {
	c := &tidyContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = tidyContext{
			mail:    apptest.NewFakeMailbox(),
			tracker: apptest.NewFakeTracker(),
			log:     &apptest.FakeTickLog{},
			ids:     map[string]string{},
		}
		return ctx, nil
	})

	ctx.Given(`^a tidy world with the clock at "([^"]*)"$`, c.aTidyWorld)
	ctx.Given(`^the mail "([^"]*)" was sent (\d+) (hours?|days?) ago$`, c.mailSentAgo)
	ctx.Given(`^the mail "([^"]*)" was sent (\d+) (hours?|days?) ago and read$`, c.mailSentAgoAndRead)
	ctx.Given(`^a tidy story "([^"]*)" that is (open|closed)$`, c.aTidyStory)
	ctx.Given(`^a tidy story "([^"]*)" that is open and (\d+) days old$`, c.anOldTidyStory)
	ctx.Given(`^a tidy story "([^"]*)" labelled "([^"]*)" that is open and (\d+) days old$`, c.anOldLabelledTidyStory)
	ctx.Given(`^a tidy epic "([^"]*)" that is open and (\d+) days old$`, c.anOldTidyEpic)
	ctx.Given(`^a postern question note for "([^"]*)" and for "([^"]*)"$`, c.questionNotes)

	ctx.When(`^mw tidy runs$`, func() error { return c.run(false) })
	ctx.When(`^mw tidy runs with --dry-run$`, func() error { return c.run(true) })

	ctx.Then(`^tidying succeeds$`, c.tidyingSucceeds)
	ctx.Then(`^the tidy mail "([^"]*)" is closed, saying "([^"]*)"$`, c.mailClosedSaying)
	ctx.Then(`^the tidy mail "([^"]*)" is still open$`, c.mailStillOpen)
	ctx.Then(`^the question note for "([^"]*)" is cleared$`, func(bead string) error { return c.noteIs(bead, false) })
	ctx.Then(`^the question note for "([^"]*)" is kept$`, func(bead string) error { return c.noteIs(bead, true) })
	ctx.Then(`^the tidy story "([^"]*)" carries the comment "([^"]*)"$`, c.storyCarries)
	ctx.Then(`^the tidy story "([^"]*)" carries no comment$`, c.storyCarriesNone)
	ctx.Then(`^the tidy story "([^"]*)" is still (open|closed)$`, c.storyIsStill)
	ctx.Then(`^nothing was written to the tidy tracker$`, c.nothingWritten)
	ctx.Then(`^the tick log has (\d+) tidy lines$`, c.tickLogHas)
	ctx.Then(`^a tidy line says "([^"]*)"$`, c.aTidyLineSays)
	ctx.Then(`^the tidy report says "([^"]*)"$`, c.reportSays)
}

func (c *tidyContext) aTidyWorld(at string) error {
	now, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return fmt.Errorf("parsing %q as a time: %w", at, err)
	}
	c.now = now
	return nil
}

// ago is how long ago a scenario says something was.
func ago(count, unit string) (time.Duration, error) {
	n, err := strconv.Atoi(count)
	if err != nil {
		return 0, fmt.Errorf("parsing %q as a number: %w", count, err)
	}
	if strings.HasPrefix(unit, "hour") {
		return time.Duration(n) * time.Hour, nil
	}
	return time.Duration(n) * 24 * time.Hour, nil
}

func (c *tidyContext) send(subject, count, unit string) (string, error) {
	age, err := ago(count, unit)
	if err != nil {
		return "", err
	}
	id, err := c.mail.Send(context.Background(), application.NewMessage{From: "mw@laptop", To: "mayor", Subject: subject, Body: "body"})
	if err != nil {
		return "", err
	}
	c.ids[subject] = id
	return id, c.mail.SetSent(id, c.now.Add(-age))
}

func (c *tidyContext) mailSentAgo(subject, count, unit string) error {
	_, err := c.send(subject, count, unit)
	return err
}

func (c *tidyContext) mailSentAgoAndRead(subject, count, unit string) error {
	id, err := c.send(subject, count, unit)
	if err != nil {
		return err
	}
	_, err = c.mail.Read(context.Background(), id, "mayor")
	return err
}

func (c *tidyContext) aTidyStory(id, status string) error {
	c.tracker.AddEpic("mw-tidy", domain.Path{})
	c.tracker.AddStory("mw-tidy", domain.Story{ID: id, Title: id})
	if status == "closed" {
		return c.tracker.SetStatus(id, apptest.StatusClosed)
	}
	return nil
}

func (c *tidyContext) anOldTidyStory(id, days string) error {
	return c.anOldLabelledTidyStory(id, "", days)
}

func (c *tidyContext) anOldLabelledTidyStory(id, label, days string) error {
	if err := c.aTidyStory(id, "open"); err != nil {
		return err
	}
	age, err := ago(days, "days")
	if err != nil {
		return err
	}
	if err := c.tracker.SetCreated(id, c.now.Add(-age)); err != nil {
		return err
	}
	if label != "" {
		return c.tracker.SetLabels(id, label)
	}
	return nil
}

func (c *tidyContext) anOldTidyEpic(id, _ string) error {
	c.tracker.AddEpic(id, domain.Path{})
	c.tracker.DescribeEpic(id, id, apptest.StatusOpen, application.DefaultPriority)
	return nil
}

func (c *tidyContext) questionNotes(first, second string) error {
	for _, bead := range []string{first, second} {
		if err := c.tracker.SetNote(context.Background(), application.PosternQuestionKey(bead), "{}"); err != nil {
			return err
		}
	}
	return nil
}

func (c *tidyContext) run(dry bool) error {
	c.written = c.tracker.Writes()
	c.report, c.err = application.Tidy{
		Mail: c.mail, Notes: c.tracker, Beads: c.tracker, Log: c.log,
		DryRun: dry,
		Now:    func() time.Time { return c.now },
	}.Run(context.Background())
	return nil
}

func (c *tidyContext) tidyingSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("tidying failed: %w", c.err)
	}
	return nil
}

func (c *tidyContext) mailID(subject string) (string, error) {
	id, ok := c.ids[subject]
	if !ok {
		return "", fmt.Errorf("no mail %q was sent", subject)
	}
	return id, nil
}

func (c *tidyContext) mailClosedSaying(subject, saying string) error {
	id, err := c.mailID(subject)
	if err != nil {
		return err
	}
	closed, reason := c.mail.Closed(id)
	if !closed {
		return fmt.Errorf("mail %q was not closed", subject)
	}
	if reason != saying {
		return fmt.Errorf("mail %q was closed saying %q, want %q", subject, reason, saying)
	}
	return nil
}

func (c *tidyContext) mailStillOpen(subject string) error {
	id, err := c.mailID(subject)
	if err != nil {
		return err
	}
	if closed, _ := c.mail.Closed(id); closed {
		return fmt.Errorf("mail %q was closed", subject)
	}
	return nil
}

func (c *tidyContext) noteIs(bead string, kept bool) error {
	notes, err := c.tracker.NotesWithPrefix(context.Background(), application.PosternQuestionKey(""))
	if err != nil {
		return err
	}
	_, there := notes[application.PosternQuestionKey(bead)]
	if there != kept {
		return fmt.Errorf("the question note for %s: kept is %t, want %t", bead, there, kept)
	}
	return nil
}

func (c *tidyContext) storyCarries(id, comment string) error {
	for _, said := range c.tracker.Comments(id) {
		if said == comment {
			return nil
		}
	}
	return fmt.Errorf("%s carries %q, not %q", id, c.tracker.Comments(id), comment)
}

func (c *tidyContext) storyCarriesNone(id string) error {
	if said := c.tracker.Comments(id); len(said) != 0 {
		return fmt.Errorf("%s carries %q, want no comment", id, said)
	}
	return nil
}

func (c *tidyContext) storyIsStill(id, status string) error {
	found, err := c.tracker.ShowBeads(context.Background(), []string{id})
	if err != nil {
		return err
	}
	if len(found) != 1 {
		return fmt.Errorf("the tracker did not return %s", id)
	}
	if got := found[0].Closed(); got != (status == "closed") {
		return fmt.Errorf("%s: closed is %t, want it %s", id, got, status)
	}
	return nil
}

func (c *tidyContext) nothingWritten() error {
	if writes := c.tracker.Writes() - c.written; writes != 0 {
		return fmt.Errorf("tidy wrote to the tracker %d times, want none", writes)
	}
	return nil
}

func (c *tidyContext) tidyLines() []string {
	var lines []string
	for _, line := range c.log.Lines() {
		if _, words, ok := strings.Cut(line, " "); ok && strings.HasPrefix(words, application.TidyLogPrefix) {
			lines = append(lines, words)
		}
	}
	return lines
}

func (c *tidyContext) tickLogHas(count string) error {
	n, err := strconv.Atoi(count)
	if err != nil {
		return err
	}
	if got := len(c.tidyLines()); got != n {
		return fmt.Errorf("the tick log has %d tidy lines, want %d: %q", got, n, c.log.Lines())
	}
	return nil
}

func (c *tidyContext) aTidyLineSays(text string) error {
	for _, line := range c.tidyLines() {
		if strings.Contains(line, text) {
			return nil
		}
	}
	return fmt.Errorf("no tidy line says %q: %q", text, c.tidyLines())
}

func (c *tidyContext) reportSays(text string) error {
	if !strings.Contains(c.report.String(), text) {
		return fmt.Errorf("the report does not say %q:\n%s", text, c.report.String())
	}
	return nil
}
