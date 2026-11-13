package application_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// mw check runs detached (checkStarter, specsProcess in cmd/mw/check.go) and
// takes minutes, waiting for the slot, the gate lock and a calm host. It checks
// nothing about the home at its start, so it must write nothing the home owns
// at its end: only its own log and gate records. This keeps it so
// (mw-gq6.1235, the sweep of mw-gq6.1207).
func TestADetachedCheckWritesNothingTheHomeOwns(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	slots := rig.NewSlots(rig.WithSlotPoll(5 * time.Millisecond))
	checks := &scriptedChecks{passes: []bool{true}}

	statusBefore := tracker.State("mw-x.1", "run")
	metaBefore := tracker.Metadata("mw-x.1")
	if _, err := (application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
		Slot: slots, Records: &mapRecords{}, PartSlotWait: time.Second,
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": rigDir},
		Out:  io.Discard,
	}).Run(context.Background(), "mw-x.1"); err != nil {
		t.Fatalf("the check: %v", err)
	}
	if got := tracker.Comments("mw-x.1"); len(got) != 0 {
		t.Errorf("a check left comments on the story: %v", got)
	}
	if got := tracker.State("mw-x.1", "run"); got != statusBefore {
		t.Errorf("a check changed the story's run state from %q to %q", statusBefore, got)
	}
	if got := tracker.Metadata("mw-x.1"); !reflect.DeepEqual(got, metaBefore) {
		t.Errorf("a check changed the story's metadata: before %v, after %v", metaBefore, got)
	}
}
