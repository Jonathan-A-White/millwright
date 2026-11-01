package application_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.879: a hold and un-hold made under a live session leaves the story
// 'open' (or held) with its claim lease still renewed by the heartbeat. Every
// reader of what runs on a host counts it, as mw status's RUNNING does since
// mw-gq6.878, and none counts one whose lease has run out or one queued for its
// batch, which holds no session.

// aLiveShape is a story that took a claim and was then set to another status.
type aLiveShape struct {
	name   string
	status string
	lease  time.Duration // from the reader's clock
	queued bool
	counts bool
}

var liveShapes = []aLiveShape{
	{"set back to open, lease live", application.StatusOpen, time.Hour, false, true},
	{"held, lease live", application.StatusHeld, time.Hour, false, true},
	{"set back to open, lease run out", application.StatusOpen, -time.Hour, false, false},
	{"held, lease run out", application.StatusHeld, -time.Hour, false, false},
	{"set back to open, lease live, queued for its batch", application.StatusOpen, time.Hour, true, false},
}

// claimedThenSet claims id as holder with a lease of lease from now, then sets
// its status, the way a hold and un-hold under a running session leaves it.
func claimedThenSet(t *testing.T, tracker *apptest.FakeTracker, id, holder string, now time.Time, shape aLiveShape) {
	t.Helper()
	if err := tracker.ClaimAs(id, holder, now.Add(shape.lease)); err != nil {
		t.Fatalf("claiming %s: %v", id, err)
	}
	if shape.queued {
		if err := tracker.SetStoryState(context.Background(), id, application.RunState, application.RunQueued, "the test"); err != nil {
			t.Fatalf("queueing %s: %v", id, err)
		}
	}
	if err := tracker.SetStatus(id, shape.status); err != nil {
		t.Fatalf("setting the status of %s: %v", id, err)
	}
}

func TestADispatchCountsAStoryWithALiveLeaseThatIsNotInProgressAgainstItsCap(t *testing.T) {
	for _, shape := range liveShapes {
		t.Run(shape.name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Runs"})
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "Waits"})
			claimedThenSet(t, tracker, "mw-gq6.1", "mw@vps", time.Now(), shape)

			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			started := slices.Contains(startedIDs(report), "mw-gq6.2")
			if shape.counts && (started || report.Running != 1) {
				t.Errorf("expected the cap of 1 held by mw-gq6.1 (running %d), but started %v", report.Running, startedIDs(report))
			}
			if !shape.counts && !started {
				t.Errorf("expected mw-gq6.2 started: mw-gq6.1 holds no session, got running %d, started %v", report.Running, startedIDs(report))
			}
		})
	}
}

func TestACloudBoxIsBusyWhileAStoryWithALiveLeaseThatIsNotInProgressRunsOnIt(t *testing.T) {
	for _, shape := range liveShapes {
		t.Run(shape.name, func(t *testing.T) {
			now := time.Date(2026, 10, 30, 23, 30, 0, 0, time.UTC)
			tracker := apptest.NewFakeTracker()
			tracker.AddEpic("mw-q", domain.Path{Rig: "millwright", Branch: "main", Host: "cloud1"})
			tracker.AddStory("mw-q", domain.Story{ID: "mw-q.1", Title: "Runs"})
			claimedThenSet(t, tracker, "mw-q.1", "mw@cloud1", now, shape)
			book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-2 * time.Hour), IdleSince: now.Add(-time.Hour)}}}}
			_, err := application.CloudCheck{
				Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
				Plan: cloudPlan, Now: func() time.Time { return now },
			}.Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			// Idle for an hour, past the plan's half hour, a box that runs no session
			// is destroyed; one that runs a session is marked busy and kept.
			up := len(book.State.Boxes) == 1 && book.State.Boxes[0].IdleSince.IsZero()
			if up != shape.counts {
				t.Errorf("expected cloud1 kept busy: %v, got %v (%+v)", shape.counts, up, book.State.Boxes)
			}
		})
	}
}

func TestStatusCountsATesterWithALiveLeaseThatIsNotInProgressAgainstTestersPerRig(t *testing.T) {
	for _, shape := range liveShapes {
		t.Run(shape.name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			_ = dispatch
			aTesterOn(t, tracker, "mw-t.1", "millwright", "vps", false)
			aTesterOn(t, tracker, "mw-t.2", "millwright", "vps", false)
			claimedThenSet(t, tracker, "mw-t.1", "mw@vps", statusNow, shape)

			report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder", TestersPerRig: 1,
				Now: func() time.Time { return statusNow }}.Run(context.Background())
			if err != nil {
				t.Fatalf("reading status: %v", err)
			}
			_, held := report.TesterHolds["mw-t.2"]
			if held != shape.counts {
				t.Errorf("expected mw-t.2 held back by the Tester running: %v, got %v (holds %+v)", shape.counts, held, report.TesterHolds)
			}
		})
	}
}

func TestDampersCountATesterWithALiveLeaseThatIsNotInProgressAgainstTestersPerRig(t *testing.T) {
	for _, shape := range liveShapes {
		t.Run(shape.name, func(t *testing.T) {
			facts := aCleanDamperFixture()
			facts.Work.Running = nil
			one := damperStory("mw-t.1", 2, time.Hour, application.LabelTester)
			one.Status, one.Assignee, one.LeaseExpires = shape.status, "mw@vps", facts.Now.Add(shape.lease)
			if shape.queued {
				one.Labels = append(one.Labels, application.RunState+":"+application.RunQueued)
			}
			two := damperStory("mw-t.2", 2, time.Hour, application.LabelTester)
			two.Status, two.Assignee, two.LeaseExpires = application.StatusOpen, "mw@vps", facts.Now.Add(time.Hour)
			list := &facts.Work.OpenClaimed
			if shape.status == application.StatusHeld {
				list = &facts.Work.Held
			}
			*list = append(*list, one)
			facts.Work.OpenClaimed = append(facts.Work.OpenClaimed, two)

			report := application.ReadDampers(facts, application.DamperLimits{})
			var over bool
			for _, alarm := range report.Alarms {
				over = over || strings.Contains(alarm.Text, "Tester stories running on millwright")
			}
			if over != shape.counts {
				t.Errorf("expected the testers_per_rig alarm: %v, got %v (%+v)", shape.counts, over, report.Alarms)
			}
		})
	}
}

func TestNudgeFindsAStoryWithALiveLeaseThatIsNotInProgressQuiet(t *testing.T) {
	for _, shape := range liveShapes {
		t.Run(shape.name, func(t *testing.T) {
			tracker := aTrackerPathedToVPS(t)
			storyOn(t, tracker, "mw-gq6.30", "A story that hangs", "vps")
			claim(t, tracker, "mw-gq6.30", 73*time.Minute)
			claimedThenSet(t, tracker, "mw-gq6.30", "mw@vps", statusNow, shape)
			if err := tracker.SetStarted("mw-gq6.30", statusNow.Add(-73*time.Minute)); err != nil {
				t.Fatal(err)
			}

			clauses := nudgeReport(t, tracker, application.Nudge{})
			if found := len(clauses) == 1; found != shape.counts {
				t.Errorf("expected the quiet alarm to name mw-gq6.30: %v, got %+v", shape.counts, clauses)
			}
		})
	}
}
