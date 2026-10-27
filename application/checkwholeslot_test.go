package application_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// wholeCheckOnAHeldSlot is mw check with no --part on a rig whose gate is not
// split, while a landing holds the rig's merge slot (mw-gq6.817).
func wholeCheckOnAHeldSlot(t *testing.T, wait time.Duration) (check func() (application.CheckReport, error), checks *scriptedChecks, told *lockedBuffer, release func()) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	told = &lockedBuffer{}
	slots := rig.NewSlots(rig.WithSlotPoll(5*time.Millisecond), rig.WithSlotNoticeEvery(40*time.Millisecond),
		rig.WithSlotNotice(func(said string) { told.WriteString(said) }))
	landing, err := slots.Take(context.Background(), rigDir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatalf("taking the slot as a landing: %v", err)
	}
	checks = &scriptedChecks{passes: []bool{true}}
	records := &mapRecords{}
	check = func() (application.CheckReport, error) {
		return application.Check{
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
			Slot: slots, Records: records, PartSlotWait: wait,
			Host: "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": rigDir},
			Out:  io.Discard,
		}.Run(context.Background(), "mw-x.1")
	}
	return check, checks, told, func() { landing.Release(context.Background()) }
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) WriteString(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b.WriteString(s)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func slotRefusalOf(report application.CheckReport) *application.Refusal {
	for i := range report.Refusals {
		if report.Refusals[i].Reason == application.ReasonTestsNotRun {
			return &report.Refusals[i]
		}
	}
	return nil
}

func TestACheckWithNoPartThatCannotGetTheSlotInItsWaitEndsWithOneLineNamingTheHolderAndTheNextCommand(t *testing.T) {
	check, checks, told, release := wholeCheckOnAHeldSlot(t, 150*time.Millisecond)
	defer release()

	started := time.Now()
	type outcome struct {
		report application.CheckReport
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		report, err := check()
		done <- outcome{report, err}
	}()
	var got outcome
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the check to give up at its wait, it was still waiting for the slot after 5 seconds")
	}
	if time.Since(started) > 4*time.Second {
		t.Errorf("expected the check to give up at its wait, it took %s", time.Since(started))
	}
	if got.err == nil {
		t.Fatal("expected the check not to pass while a landing held the slot")
	}
	refusal := slotRefusalOf(got.report)
	if refusal == nil {
		t.Fatalf("expected a refusal that the tests were not run, got %+v", got.report.Refusals)
	}
	for _, want := range []string{"did not start", "mw land on laptop, landing a batch of millwright", "since ", "nothing was run", "`mw check mw-x.1`"} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("expected the line to say %q, got %q", want, refusal.Why)
		}
	}
	if strings.Contains(refusal.Why, "\n") {
		t.Errorf("expected one line, got %q", refusal.Why)
	}
	if checks.runs != 0 {
		t.Errorf("expected the tests not to have run, they ran %d time(s)", checks.runs)
	}
	// The wait said, as --part's does, who held the slot while it went on.
	if !strings.Contains(told.String(), "mw land on laptop, landing a batch of millwright") || !strings.Contains(told.String(), "still waiting for the merge slot") {
		t.Errorf("expected the wait to say again who held the slot, got %q", told.String())
	}
}

func TestACheckWithNoPartHandedBackOnTheSlotTwiceInOneAttemptCountsTheSecondOnceAndSaysSo(t *testing.T) {
	check, _, _, release := wholeCheckOnAHeldSlot(t, 100*time.Millisecond)
	defer release()

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

func TestACheckWithNoPartRunsAtOnceWhenTheSlotIsFree(t *testing.T) {
	check, checks, _, release := wholeCheckOnAHeldSlot(t, 100*time.Millisecond)
	release()
	report, err := check()
	if err != nil {
		t.Fatalf("expected the check to pass with the slot free, got %v\n%s", err, report)
	}
	if checks.runs != 1 {
		t.Errorf("expected the tests to run once, they ran %d time(s)", checks.runs)
	}
}
