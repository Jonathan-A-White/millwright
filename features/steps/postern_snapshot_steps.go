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

// posternSnapshotFeatureNow is the clock features/postern_snapshot.feature
// builds its snapshot with, so that "landed yesterday" and "landed three
// days ago" are unambiguous relative to it.
var posternSnapshotFeatureNow = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

// posternSnapshotFeatureDescription and posternSnapshotFeatureComments are
// the fixed text a working bead's description and comments carry, for the
// Then steps to check the snapshot reports them back unchanged.
const posternSnapshotFeatureDescription = "This bead ships the thing the epic is for."

var posternSnapshotFeatureComments = []string{"first update", "second update", "third update", "fourth update"}

// posternSnapshotContext holds the fake tracker a scenario files beads
// against, and the snapshot the last build produced.
type posternSnapshotContext struct {
	tracker *apptest.FakeTracker
	doc     application.PosternSnapshotDoc
	err     error
}

// InitializePosternSnapshotScenario registers the steps of
// features/postern_snapshot.feature.
func InitializePosternSnapshotScenario(ctx *godog.ScenarioContext) {
	c := &posternSnapshotContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = posternSnapshotContext{tracker: apptest.NewFakeTracker()}
		return ctx, nil
	})

	ctx.Given(`^a live epic "([^"]*)"$`, c.aLiveEpic)
	ctx.Given(`^the bead "([^"]*)" under "([^"]*)" is being worked, with a description and four comments$`, c.theBeadIsBeingWorkedWithADescriptionAndFourComments)
	ctx.Given(`^the bead "([^"]*)" under "([^"]*)" landed yesterday$`, c.theBeadLandedYesterday)
	ctx.Given(`^the bead "([^"]*)" under "([^"]*)" landed three days ago$`, c.theBeadLandedThreeDaysAgo)

	ctx.When(`^the postern snapshot is built$`, c.thePosternSnapshotIsBuilt)

	ctx.Then(`^the working bead "([^"]*)" in the snapshot shows its description and its newest three comments, newest first$`, c.theWorkingBeadShowsItsDescriptionAndNewestThreeComments)
	ctx.Then(`^the snapshot's landed beads under "([^"]*)" are exactly "([^"]*)"$`, c.theSnapshotsLandedBeadsAreExactly)
	ctx.Then(`^the snapshot's closed_count under "([^"]*)" is (\d+)$`, c.theSnapshotsClosedCountIs)
}

func (c *posternSnapshotContext) aLiveEpic(id string) error {
	c.tracker.AddEpic(id, domain.Path{})
	c.tracker.DescribeEpic(id, id, apptest.StatusOpen, application.DefaultPriority)
	return nil
}

func (c *posternSnapshotContext) theBeadIsBeingWorkedWithADescriptionAndFourComments(beadID, epicID string) error {
	c.tracker.AddStory(epicID, domain.Story{ID: beadID, Title: beadID})
	if err := c.tracker.SetStatus(beadID, apptest.StatusInProgress); err != nil {
		return fmt.Errorf("claiming %s: %w", beadID, err)
	}
	if err := c.tracker.SetDescription(beadID, posternSnapshotFeatureDescription); err != nil {
		return fmt.Errorf("describing %s: %w", beadID, err)
	}
	for i, text := range posternSnapshotFeatureComments {
		at := posternSnapshotFeatureNow.Add(-time.Duration(len(posternSnapshotFeatureComments)-i) * time.Hour)
		if err := c.tracker.CommentOnStoryAt(beadID, text, at); err != nil {
			return fmt.Errorf("commenting on %s: %w", beadID, err)
		}
	}
	return nil
}

func (c *posternSnapshotContext) theBeadLandedYesterday(beadID, epicID string) error {
	return c.closeBeadAt(beadID, epicID, posternSnapshotFeatureNow.Add(-20*time.Hour))
}

func (c *posternSnapshotContext) theBeadLandedThreeDaysAgo(beadID, epicID string) error {
	return c.closeBeadAt(beadID, epicID, posternSnapshotFeatureNow.Add(-3*24*time.Hour))
}

func (c *posternSnapshotContext) closeBeadAt(beadID, epicID string, closedAt time.Time) error {
	c.tracker.AddStory(epicID, domain.Story{ID: beadID, Title: beadID})
	if err := c.tracker.SetStatus(beadID, apptest.StatusClosed); err != nil {
		return fmt.Errorf("closing %s: %w", beadID, err)
	}
	if err := c.tracker.SetClosedAt(beadID, closedAt); err != nil {
		return fmt.Errorf("dating %s's close: %w", beadID, err)
	}
	return nil
}

func (c *posternSnapshotContext) thePosternSnapshotIsBuilt() error {
	doc, err := application.PosternSnapshot{
		Tracker: c.tracker,
		Notes:   c.tracker,
		Now:     func() time.Time { return posternSnapshotFeatureNow },
	}.Build(context.Background())
	c.doc, c.err = doc, err
	return err
}

func (c *posternSnapshotContext) epic(id string) (application.PosternSnapshotEpic, error) {
	for _, e := range c.doc.Epics {
		if e.ID == id {
			return e, nil
		}
	}
	return application.PosternSnapshotEpic{}, fmt.Errorf("the snapshot has no epic %q; got %+v", id, c.doc.Epics)
}

func (c *posternSnapshotContext) theWorkingBeadShowsItsDescriptionAndNewestThreeComments(beadID string) error {
	var found *application.PosternSnapshotWorking
	for _, e := range c.doc.Epics {
		for i := range e.Working {
			if e.Working[i].ID == beadID {
				found = &e.Working[i]
			}
		}
	}
	if found == nil {
		return fmt.Errorf("the snapshot has no working bead %q", beadID)
	}
	if found.Description != posternSnapshotFeatureDescription {
		return fmt.Errorf("expected description %q, got %q", posternSnapshotFeatureDescription, found.Description)
	}
	wantNewestFirst := []string{"fourth update", "third update", "second update"}
	if len(found.Comments) != len(wantNewestFirst) {
		return fmt.Errorf("expected %d comments, got %d: %+v", len(wantNewestFirst), len(found.Comments), found.Comments)
	}
	for i, want := range wantNewestFirst {
		if found.Comments[i].Text != want {
			return fmt.Errorf("comment %d: expected %q, got %q", i, want, found.Comments[i].Text)
		}
		if found.Comments[i].At == "" {
			return fmt.Errorf("comment %d (%q) carries no timestamp", i, want)
		}
	}
	return nil
}

func (c *posternSnapshotContext) theSnapshotsLandedBeadsAreExactly(epicID, wantCSV string) error {
	e, err := c.epic(epicID)
	if err != nil {
		return err
	}
	var got []string
	for _, l := range e.Landed {
		got = append(got, l.ID)
	}
	want := strings.Split(wantCSV, ",")
	for i := range want {
		want[i] = strings.TrimSpace(want[i])
	}
	if len(got) != len(want) {
		return fmt.Errorf("expected landed beads %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			return fmt.Errorf("expected landed beads %v, got %v", want, got)
		}
	}
	return nil
}

func (c *posternSnapshotContext) theSnapshotsClosedCountIs(epicID string, want int) error {
	e, err := c.epic(epicID)
	if err != nil {
		return err
	}
	if e.ClosedCount != want {
		return fmt.Errorf("expected closed_count %d, got %d", want, e.ClosedCount)
	}
	return nil
}
