package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// wholeCheckOnABusyHost is mw check with no --part on a rig whose gate is not
// split, on a host whose load reads as readings (the last again for every read
// after), with a wait for a calm host that is counted and not slept (mw-gq6.819).
func wholeCheckOnABusyHost(t *testing.T, readings []application.LoadReading, wait time.Duration) (check func() (application.CheckReport, error), checks *scriptedChecks, said *strings.Builder, waited *time.Duration) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	checks = &scriptedChecks{passes: []bool{true}}
	records := &mapRecords{}
	said, waited = &strings.Builder{}, new(time.Duration)
	check = func() (application.CheckReport, error) {
		return application.Check{
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
			Slot: aSlot{}, Records: records, PartSlotWait: wait,
			Load:     &apptest.FakeHostLoad{Readings: readings},
			LoadPoll: 30 * time.Second, // LoadBound is left at its default, 15 minutes
			LoadWait: func(_ context.Context, d time.Duration) error { *waited += d; return nil },
			Host:     "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": rigDir},
			Out:  said,
		}.Run(context.Background(), "mw-x.1")
	}
	return check, checks, said, waited
}

func TestACheckWithNoPartOnAHostThatStaysBusyWaitsNoLongerThanTheSlotWaitAndEndsInOneLine(t *testing.T) {
	check, checks, said, waited := wholeCheckOnABusyHost(t, []application.LoadReading{busy()}, 3*time.Minute)

	report, err := check()

	if err == nil {
		t.Fatal("expected the check not to pass while the host stayed busy")
	}
	if *waited > 3*time.Minute {
		t.Errorf("expected a wait of no more than the slot wait, 3m, it waited %s", *waited)
	}
	if checks.runs != 0 {
		t.Errorf("expected the tests not to have run, they ran %d time(s)", checks.runs)
	}
	refusal := slotRefusalOf(report)
	if refusal == nil {
		t.Fatalf("expected a refusal that the tests were not run, got %+v", report.Refusals)
	}
	for _, want := range []string{"did not start", "load 39.0 of 20 cores", "since ", "nothing was run", "`mw check mw-x.1`"} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("expected the line to say %q, got %q", want, refusal.Why)
		}
	}
	if strings.Contains(refusal.Why, "\n") {
		t.Errorf("expected one line, got %q", refusal.Why)
	}
	// It said the load and since when, every minute, as the slot wait does.
	for _, want := range []string{"still waiting for laptop to calm", "since ", "(1m so far", "(2m so far", "(3m so far"} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("expected the wait to say %q, got %q", want, said.String())
		}
	}
	if n := strings.Count(said.String(), "still waiting for laptop to calm"); n != 3 {
		t.Errorf("expected a notice every minute of a 3m wait (3 of them), got %d in %q", n, said.String())
	}
	if !strings.Contains(said.String(), "load 39.0 of 20 cores") {
		t.Errorf("expected the notices to say the load, got %q", said.String())
	}
}

func TestACheckWithNoPartHandedBackOnTheHostsLoadTwiceInOneAttemptCountsTheSecondOnceAndSaysSo(t *testing.T) {
	check, _, _, _ := wholeCheckOnABusyHost(t, []application.LoadReading{busy()}, 2*time.Minute)

	first, _ := check()
	one := slotRefusalOf(first)
	if one == nil {
		t.Fatalf("expected a refusal on the first hand-back, got %+v", first.Refusals)
	}
	if strings.Contains(one.Why, "counted once") {
		t.Errorf("expected the first hand-back to be a plain wait, got %q", one.Why)
	}
	second, _ := check()
	two := slotRefusalOf(second)
	if two == nil {
		t.Fatalf("expected a refusal on the second hand-back, got %+v", second.Refusals)
	}
	for _, want := range []string{"counted once for this attempt", "not a failure", "`mw check mw-x.1`", "comment on the story"} {
		if !strings.Contains(two.Why, want) {
			t.Errorf("expected the repeat to say %q, got %q", want, two.Why)
		}
	}
	if strings.Contains(two.Why, "\n") {
		t.Errorf("expected one line, got %q", two.Why)
	}
}

func TestACheckWithNoPartRunsTheTestsOnceTheHostCalmsWithinItsWait(t *testing.T) {
	check, checks, _, waited := wholeCheckOnABusyHost(t, []application.LoadReading{busy(), busy(), calm()}, 5*time.Minute)

	report, err := check()

	if err != nil {
		t.Fatalf("expected the check to pass once the host calmed, got %v\n%s", err, report)
	}
	if checks.runs != 1 {
		t.Errorf("expected the tests to run once, they ran %d time(s)", checks.runs)
	}
	if *waited != time.Minute {
		t.Errorf("expected two polls of 30s before the host calmed, it waited %s", *waited)
	}
}
