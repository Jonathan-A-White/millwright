package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// landingsAlive is an application.LandingsInFlight that names the rigs it was
// given, or fails.
type landingsAlive struct {
	rigs []string
	err  error
}

func (l landingsAlive) Pending(context.Context) ([]string, error) { return l.rigs, l.err }

func statusWithBatchDirs(t *testing.T, dirs *apptest.FakeBatchRefs, landings application.LandingsInFlight) application.StatusReport {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("ep", domain.Path{Rig: "lampas", Branch: "main", Host: "desktop"})
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "desktop", Seat: "builder",
		Now: func() time.Time { return statusNow }, BatchDirs: dirs, Landings: landings,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}

// A batch whose mw land died leaves runs/batch/<rig>/<n>/ with no outcome.txt,
// and mw status names it (mw-gq6.1264).
func TestStatusNamesABatchDirectoryWithNoOutcomeWhoseLandIsNotRunning(t *testing.T) {
	dirs := &apptest.FakeBatchRefs{}
	dirs.Leave("lampas", 7)
	report := statusWithBatchDirs(t, dirs, landingsAlive{})
	if len(report.BatchesNoOutcome) != 1 || report.BatchesNoOutcome[0].Rig != "lampas" || report.BatchesNoOutcome[0].Number != 7 {
		t.Fatalf("expected batch 7 of lampas to be named, got %+v", report.BatchesNoOutcome)
	}
	text := report.String()
	for _, want := range []string{application.BatchNoOutcomeHeading, "runs/batch/lampas/7/", "outcome.txt"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in:\n%s", want, text)
		}
	}
}

// A batch whose mw land is running has no outcome yet, and is no fault.
func TestStatusDoesNotNameABatchWhoseLandIsRunning(t *testing.T) {
	dirs := &apptest.FakeBatchRefs{}
	dirs.Leave("lampas", 7)
	dirs.Leave("millwright", 3)
	report := statusWithBatchDirs(t, dirs, landingsAlive{rigs: []string{"lampas"}})
	if len(report.BatchesNoOutcome) != 1 || report.BatchesNoOutcome[0].Rig != "millwright" {
		t.Fatalf("expected only millwright's batch named, got %+v", report.BatchesNoOutcome)
	}
}

// Whether a land runs could not be read: nothing is said rather than a batch
// that may be landing now.
func TestStatusNamesNoBatchWhenTheLandsCannotBeRead(t *testing.T) {
	dirs := &apptest.FakeBatchRefs{}
	dirs.Leave("lampas", 7)
	report := statusWithBatchDirs(t, dirs, landingsAlive{err: errors.New("no state dir")})
	if len(report.BatchesNoOutcome) != 0 {
		t.Fatalf("expected none, got %+v", report.BatchesNoOutcome)
	}
}

// With no batch directory bare of an outcome the section is not printed.
func TestStatusLeavesTheBatchSectionOutWhenAllHaveAnOutcome(t *testing.T) {
	report := statusWithBatchDirs(t, &apptest.FakeBatchRefs{}, landingsAlive{})
	if strings.Contains(report.String(), application.BatchNoOutcomeHeading) {
		t.Fatalf("expected no section, got:\n%s", report.String())
	}
}
