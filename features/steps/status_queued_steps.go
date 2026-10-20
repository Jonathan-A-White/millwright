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
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// queuedClock is how the report writes a time of a queued story, as these
// steps expect to read it.
const queuedClock = "2006-01-02 15:04Z"

// registerStatusQueuedSteps registers the steps of the scenarios about stories
// queued for batch landing (mw-gq6.604).
func registerStatusQueuedSteps(ctx *godog.ScenarioContext, c *statusContext) {
	ctx.Given(`^the rig "([^"]*)" lands in batches every (\d+) minutes$`, c.theRigLandsInBatchesEvery)
	ctx.Given(`^the status story "([^"]*)" was queued for batch landing (\d+) minutes ago$`, c.theStoryWasQueuedMinutesAgo)
	ctx.Given(`^a batch-land job for the rig "([^"]*)" began running (\d+) minutes ago$`, c.aBatchLandJobBeganRunning)
	ctx.Given(`^the batch-land job ended$`, c.theBatchLandJobEnded)
	ctx.Given(`^(\d+) other events were written since$`, c.otherEventsWrittenSince)
	ctx.Then(`^the report says "([^"]*)" is queued for batch landing since (\d+) minutes ago$`, c.theReportSaysQueuedSince)
	ctx.Then(`^the report says "([^"]*)" is in the batch running since (\d+) minutes ago$`, c.theReportSaysInTheBatchSince)
	ctx.Then(`^the report says "([^"]*)" is queued too long$`, c.theReportSaysQueuedTooLong)
	ctx.Then(`^the report does not say any story is queued too long$`, c.theReportSaysNoQueuedTooLong)
	ctx.Then(`^the report does not call any story stranded$`, c.theReportCallsNoneStranded)
	ctx.Then(`^the report calls "([^"]*)" stranded$`, c.theReportCallsStranded)
}

func (c *statusContext) theRigLandsInBatchesEvery(rig, minutes string) error {
	var every int
	if _, err := fmt.Sscanf(minutes, "%d", &every); err != nil {
		return fmt.Errorf("the minutes %q are not a number: %w", minutes, err)
	}
	c.epicRules.Require(rig, domain.EpicRequirements{Landing: domain.LandingSettings{
		Mode:       domain.LandingBatch,
		BatchEvery: time.Duration(every) * time.Minute,
	}})
	return nil
}

func (c *statusContext) minutesAgo(minutes string) (time.Time, error) {
	var n int
	if _, err := fmt.Sscanf(minutes, "%d", &n); err != nil {
		return time.Time{}, fmt.Errorf("the minutes %q are not a number: %w", minutes, err)
	}
	return c.now.Add(-time.Duration(n) * time.Minute), nil
}

// theStoryWasQueuedMinutesAgo records a story as mw next does when it queues
// one: run=queued and the time it was queued.
func (c *statusContext) theStoryWasQueuedMinutesAgo(id, minutes string) error {
	at, err := c.minutesAgo(minutes)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := c.tracker.SetStoryState(ctx, id, application.RunState, application.RunQueued, "queued by the test"); err != nil {
		return err
	}
	return c.tracker.SetStoryMetadata(ctx, id, map[string]string{application.QueuedAtField: at.UTC().Format(time.RFC3339)})
}

// aBatchLandJobBeganRunning leaves the two events the follower's batch-land
// job writes when it springs: scheduled, naming the rig, and running.
func (c *statusContext) aBatchLandJobBeganRunning(rig, minutes string) error {
	at, err := c.minutesAgo(minutes)
	if err != nil {
		return err
	}
	if c.log == nil {
		c.log = &apptest.FakeEventLog{}
	}
	actor := application.BatchLandJobName + "@" + statusHost
	_, err = c.log.Append(context.Background(), []events.Event{
		{Ts: at, Kind: events.KindJob, Lane: events.LaneNormal, Actor: actor, To: events.JobScheduled, Detail: fmt.Sprintf("landing of %s (3 queued)", rig)},
		{Ts: at, Kind: events.KindJob, Lane: events.LaneNormal, Actor: actor, From: events.JobScheduled, To: events.JobRunning},
	})
	return err
}

// theBatchLandJobEnded leaves the done event the follower writes when the
// batch-land job's pass ends.
func (c *statusContext) theBatchLandJobEnded() error {
	_, err := c.log.Append(context.Background(), []events.Event{
		{Ts: c.now, Kind: events.KindJob, Lane: events.LaneNormal, Actor: application.BatchLandJobName + "@" + statusHost, From: events.JobRunning, To: events.JobDone},
	})
	return err
}

// otherEventsWrittenSince buries what is in the log under n events of no job.
func (c *statusContext) otherEventsWrittenSince(n int) error {
	evs := make([]events.Event, n)
	for i := range evs {
		evs[i] = events.Event{Ts: c.now, Kind: events.KindBeadChanged, Lane: events.LaneNormal, Actor: "mayor@" + statusHost, Bead: "mw-other", From: "running", To: "running", Detail: "comment"}
	}
	_, err := c.log.Append(context.Background(), evs)
	return err
}

func (c *statusContext) theReportSaysQueuedSince(id, minutes string) error {
	at, err := c.minutesAgo(minutes)
	if err != nil {
		return err
	}
	return c.reportSaysOf(id, "queued for batch landing since "+at.UTC().Format(queuedClock))
}

func (c *statusContext) theReportSaysInTheBatchSince(id, minutes string) error {
	at, err := c.minutesAgo(minutes)
	if err != nil {
		return err
	}
	return c.reportSaysOf(id, "in the batch running since "+at.UTC().Format(queuedClock))
}

func (c *statusContext) theReportSaysQueuedTooLong(id string) error {
	return c.reportSaysOf(id, "QUEUED TOO LONG")
}

// reportSaysOf checks the words are printed in the story's own block of the
// report: from the line that opens with its id to the next blank line.
func (c *statusContext) reportSaysOf(id, words string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	printed := c.report.String()
	from := strings.Index(printed, id)
	if from < 0 {
		return fmt.Errorf("%s is not in the report:\n%s", id, printed)
	}
	block := printed[from:]
	if to := strings.Index(block, "\n\n"); to >= 0 {
		block = block[:to]
	}
	// A line is wrapped to fit a phone: judge the words as one run.
	if !strings.Contains(strings.Join(strings.Fields(block), " "), words) {
		return fmt.Errorf("expected the block of %s to say %q, got:\n%s", id, words, block)
	}
	return nil
}

func (c *statusContext) theReportSaysNoQueuedTooLong() error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	if strings.Contains(c.report.String(), "QUEUED TOO LONG") {
		return fmt.Errorf("expected no story queued too long, got:\n%s", c.report.String())
	}
	return nil
}

func (c *statusContext) theReportCallsNoneStranded() error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	if strings.Contains(c.report.String(), "STRANDED") {
		return fmt.Errorf("expected no story called stranded, got:\n%s", c.report.String())
	}
	return nil
}

func (c *statusContext) theReportCallsStranded(id string) error {
	return c.reportSaysOf(id, "STRANDED: lease ran out")
}
