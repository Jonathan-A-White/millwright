package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// registerPosternCloseSteps adds the steps of the keep and close scenarios of
// features/postern_inbox_apply.feature to the inbox's world.
func (c *posternInboxContext) registerPosternCloseSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the postern inbox clock reads "([^"]*)"$`, c.thePosternInboxClockReads)
	ctx.Given(`^the stale epic "([^"]*)" has a held story filed (\d+) days? ago$`, c.theStaleEpicHasAHeldStory)
	ctx.Given(`^a postern keep action on bead "([^"]*)" for (\d+) days from the Governor with txid "([^"]*)"$`, c.aPosternKeepActionFromTheGovernor)
	ctx.Given(`^the postern view raises a stale need on "([^"]*)"$`, c.thePosternViewRaisesAStaleNeed)
	ctx.Then(`^the postern view raises no stale need on "([^"]*)"$`, c.thePosternViewRaisesNoStaleNeed)
	ctx.Then(`^the note "([^"]*)" reads "([^"]*)"$`, c.theNoteReads)
	ctx.Then(`^the note "([^"]*)" is not set$`, c.theNoteIsNotSet)
	ctx.Given(`^epic "([^"]*)" has an open, unclaimed story "([^"]*)"$`, c.epicHasAnOpenStory)
	ctx.Then(`^nothing under epic "([^"]*)" was closed with the reason "([^"]*)"$`, c.nothingUnderEpicWasClosed)
	ctx.Then(`^bead "([^"]*)" was closed with the reason "([^"]*)"$`, c.beadWasClosedWithTheReason)
	ctx.Then(`^bead "([^"]*)" was not closed with the reason "([^"]*)"$`, c.beadWasNotClosedWithTheReason)
	ctx.Given(`^a demo story "([^"]*)" is known to the tracker$`, c.aDemoStoryIsKnown)
	ctx.Given(`^a story "([^"]*)" titled "([^"]*)" is known to the tracker$`, c.aStoryTitledIsKnown)
	ctx.Given(`^bead "([^"]*)" is already closed$`, c.beadIsAlreadyClosed)
	ctx.Then(`^mail "([^"]*)" was sent to mayor saying "([^"]*)"$`, c.mailWasSentToMayorSaying)
}

func (c *posternInboxContext) thePosternInboxClockReads(stamp string) error {
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return err
	}
	c.clock = at
	return nil
}

// theStaleEpicHasAHeldStory files a live epic id with one held story, id.1,
// filed days before the inbox's clock: old enough for the view to call it
// stale.
func (c *posternInboxContext) theStaleEpicHasAHeldStory(id string, days int) error {
	c.memory.AddEpic(id, domain.Path{})
	c.memory.DescribeEpic(id, "Epic "+id, apptest.StatusOpen, 1)
	story := id + ".1"
	c.memory.AddStory(id, domain.Story{ID: story, Title: "Story " + story})
	if err := c.memory.SetStatus(story, apptest.StatusDeferred); err != nil {
		return err
	}
	return c.memory.SetCreated(story, c.clock.Add(-time.Duration(days)*24*time.Hour))
}

func (c *posternInboxContext) aPosternKeepActionFromTheGovernor(bead string, days int, txid string) error {
	return c.addAction(c.governorKey, txid, map[string]any{"action": "keep", "bead": bead, "days": days})
}

func (c *posternInboxContext) staleNeedsOn(bead string) (int, error) {
	doc, err := application.PosternView{Tracker: c.memory, Notes: c.memory, Now: func() time.Time { return c.clock }}.Build(context.Background())
	if err != nil {
		return 0, err
	}
	stale := 0
	for _, n := range doc.Needs {
		if n.Bead == bead && n.Kind == "stale" {
			stale++
		}
	}
	return stale, nil
}

func (c *posternInboxContext) thePosternViewRaisesAStaleNeed(bead string) error {
	stale, err := c.staleNeedsOn(bead)
	if err != nil {
		return err
	}
	if stale == 0 {
		return fmt.Errorf("expected the view to raise a stale need on %s, it raises none", bead)
	}
	return nil
}

func (c *posternInboxContext) thePosternViewRaisesNoStaleNeed(bead string) error {
	stale, err := c.staleNeedsOn(bead)
	if err != nil {
		return err
	}
	if stale != 0 {
		return fmt.Errorf("expected the view to raise no stale need on %s, it raises %d", bead, stale)
	}
	return nil
}

func (c *posternInboxContext) theNoteReads(key, want string) error {
	got, err := c.memory.Note(context.Background(), key)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("expected note %s to read %q, got %q", key, want, got)
	}
	return nil
}

func (c *posternInboxContext) theNoteIsNotSet(key string) error {
	return c.theNoteReads(key, "")
}

func (c *posternInboxContext) beadWasClosedWithTheReason(bead, want string) error {
	if got := c.memory.CloseReason(bead); got != want {
		return fmt.Errorf("expected %s closed with the reason %q, got %q", bead, want, got)
	}
	return nil
}

func (c *posternInboxContext) mailWasSentToMayorSaying(subject, words string) error {
	unread, err := c.mailbox.Inbox(context.Background(), "mayor")
	if err != nil {
		return err
	}
	for _, message := range unread {
		if message.Subject == subject && strings.Contains(message.Body, words) {
			return nil
		}
	}
	return fmt.Errorf("expected mail %q to mayor mentioning %q, got: %+v", subject, words, unread)
}

// epicHasAnOpenStory files an open, unclaimed story under epic: released, or
// waiting for dispatch.
func (c *posternInboxContext) epicHasAnOpenStory(epic, id string) error {
	c.memory.AddStory(epic, domain.Story{ID: id, Title: "Story " + id})
	return nil
}

// nothingUnderEpicWasClosed checks the epic and every story filed under it
// against a close carrying reason.
func (c *posternInboxContext) nothingUnderEpicWasClosed(epic, reason string) error {
	detail, err := c.memory.ShowEpic(context.Background(), epic)
	if err != nil {
		return err
	}
	ids := []string{epic}
	for _, story := range detail.Stories {
		ids = append(ids, story.Story.ID)
	}
	for _, id := range ids {
		if c.memory.CloseReason(id) == reason {
			return fmt.Errorf("expected nothing under %s closed with the reason %q, but %s was", epic, reason, id)
		}
	}
	return nil
}

func (c *posternInboxContext) beadWasNotClosedWithTheReason(bead, reason string) error {
	if got := c.memory.CloseReason(bead); got == reason {
		return fmt.Errorf("expected %s not closed with the reason %q, but it was", bead, reason)
	}
	return nil
}

// aDemoStoryIsKnown files a story carrying the demo label, as the Governor's
// demo cards are.
func (c *posternInboxContext) aDemoStoryIsKnown(id string) error {
	c.memory.AddStory("epic", domain.Story{ID: id, Title: "Show it working"})
	return c.memory.SetLabels(id, application.LabelDemo)
}

// aStoryTitledIsKnown files a story with title and no labels.
func (c *posternInboxContext) aStoryTitledIsKnown(id, title string) error {
	c.memory.AddStory("epic", domain.Story{ID: id, Title: title})
	return nil
}

func (c *posternInboxContext) beadIsAlreadyClosed(id string) error {
	return c.memory.SetStatus(id, apptest.StatusClosed)
}
