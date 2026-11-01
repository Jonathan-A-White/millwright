package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// The readers that ACT on what runs read a story with a live claim lease
// whatever its status says, as the counts do (mw-gq6.879): a hold and un-hold
// under a session leaves it open, assigned to the host, its lease renewed by a
// heartbeat.

// claimShape is how a claim stands at a site: a live lease on a story set back
// to open, one whose lease ran out, and one queued for its batch.
type claimShape struct {
	name   string
	lease  time.Duration
	queued bool
	live   bool
}

var claimShapes = []claimShape{
	{name: "live open claim", lease: time.Hour, live: true},
	{name: "lapsed lease", lease: -time.Hour},
	{name: "queued for its batch", lease: time.Hour, queued: true},
}

// setBackToOpen claims id for host and then leaves it open, as a hold and
// un-hold under its session does.
func setBackToOpen(t *testing.T, tracker *apptest.FakeTracker, id, host string, shape claimShape) {
	t.Helper()
	setBackToOpenAt(t, tracker, id, host, shape, time.Now())
}

// setBackToOpenAt is setBackToOpen for a site whose clock is the test's.
func setBackToOpenAt(t *testing.T, tracker *apptest.FakeTracker, id, host string, shape claimShape, now time.Time) {
	t.Helper()
	mustDo(t, tracker.ClaimAs(id, "mw@"+host, now.Add(shape.lease)))
	mustDo(t, tracker.SetStatus(id, apptest.StatusOpen))
	if shape.queued {
		mustDo(t, tracker.SetStoryState(context.Background(), id, application.RunState, application.RunQueued, ""))
	}
}

func TestPauseTypesThePauseLineToALiveClaimSetBackToOpen(t *testing.T) {
	for _, shape := range claimShapes {
		t.Run(shape.name, func(t *testing.T) {
			tracker := apptest.NewFakeTracker()
			tracker.AddEpic("mw-e", domain.Path{Rig: "rig", Branch: "main", Host: "laptop"})
			tracker.AddStory("mw-e", domain.Story{ID: "mw-e.1", Title: "A story"})
			setBackToOpen(t, tracker, "mw-e.1", "laptop", shape)
			runner := apptest.NewFakeRunner()
			mustDo(t, runner.Start(context.Background(), application.SessionSpec{Name: application.SessionName("mw-e.1"), Command: []string{"claude"}}))
			host := application.FleetHostPause{
				Host: "laptop", Notes: tracker, Stories: tracker, Runner: runner, Git: apptest.NewFakePauseGit(),
				Rigs: map[string]string{"rig": t.TempDir()},
			}

			paused, err := host.Pause(context.Background(), application.HostPauseOrder{Reason: "limit"})
			if err != nil {
				t.Fatalf("pausing: %v", err)
			}
			typed := runner.Input(application.SessionName("mw-e.1"))
			if shape.live {
				if len(typed) != 1 || !strings.HasPrefix(typed[0], "PAUSE") {
					t.Errorf("typed %q, want the PAUSE line", typed)
				}
				if len(paused.Stories) != 1 || paused.Stories[0].Story != "mw-e.1" {
					t.Errorf("paused %+v, want mw-e.1", paused.Stories)
				}
				return
			}
			if len(typed) != 0 || len(paused.Stories) != 0 {
				t.Errorf("typed %q and paused %+v, want neither for a story that holds no session", typed, paused.Stories)
			}
		})
	}
}

func TestPathSetRefusesAStoryWithALiveClaimSetBackToOpen(t *testing.T) {
	for _, shape := range claimShapes {
		t.Run(shape.name, func(t *testing.T) {
			tracker := aVaultWithTwoFormulas()
			setBackToOpen(t, tracker, "mw-gq6.2", "laptop", shape)

			_, err := application.PathSet{Tracker: tracker}.Run(context.Background(), "mw-gq6.2", "", []string{"model=sonnet"})
			detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2")
			if shape.live {
				if err == nil || !strings.Contains(err.Error(), "claim") || !strings.Contains(err.Error(), "nothing was written") {
					t.Fatalf("expected a refusal naming the live claim, got %v", err)
				}
				if detail.Story.Overrides.Model != "" {
					t.Errorf("expected the path unchanged, got %+v", detail.Story.Overrides)
				}
				return
			}
			if err != nil || detail.Story.Overrides.Model != "sonnet" {
				t.Errorf("expected the path set as before, got %v / %+v", err, detail.Story.Overrides)
			}
		})
	}
}

func TestCancelEndsTheSessionOfALiveClaimSetBackToOpen(t *testing.T) {
	for _, shape := range claimShapes {
		t.Run(shape.name, func(t *testing.T) {
			w := newControlWorld(t)
			setBackToOpen(t, w.tracker, "mw-e.2", "laptop", shape)
			mustDo(t, w.runner.Start(context.Background(), application.SessionSpec{Name: application.SessionName("mw-e.2"), Command: []string{"claude"}}))
			w.pass(t)
			appendAll(t, w.log, cancelOf("mw-e.2", "governor@postern"))
			w.pass(t)

			closed := false
			for _, name := range w.runner.Closed() {
				closed = closed || name == application.SessionName("mw-e.2")
			}
			if shape.live {
				if !closed {
					t.Errorf("the session of mw-e.2 was not closed (closed %v); errors: %s", w.runner.Closed(), w.err.String())
				}
				if d := w.detail(t, "mw-e.2"); d.Status != apptest.StatusDeferred {
					t.Errorf("the story is %s, want held", d.Status)
				}
				return
			}
			if closed || !strings.Contains(w.err.String(), "not claimed") {
				t.Errorf("closed %v, said %q; want the cancel left as not claimed", w.runner.Closed(), w.err.String())
			}
		})
	}
}

func TestSyncCommitsTheRunRecordOfALiveClaimSetBackToOpen(t *testing.T) {
	for _, shape := range claimShapes {
		t.Run(shape.name, func(t *testing.T) {
			sync, files, tracker := syncing(t)
			tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Host: "vps"})
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.739", Title: "Held and un-held"})
			setBackToOpenAt(t, tracker, "mw-gq6.739", "vps", shape, sync.Now())
			files.Dirty = []string{"runs/mw-gq6.739/result.json"}

			_, err := sync.Run(context.Background())
			if shape.live {
				if err != nil {
					t.Fatalf("expected the run record not to stall the sync, got %v", err)
				}
				if commits := files.Commits(); len(commits) != 1 || commits[0].Paths[0] != "runs/mw-gq6.739/result.json" {
					t.Errorf("expected the run record committed, got %+v", commits)
				}
				return
			}
			if _, stopped := application.Blocked(err); !stopped || len(files.Commits()) != 0 {
				t.Errorf("expected the run record to block the sync as before, got %v / %+v", err, files.Commits())
			}
		})
	}
}
