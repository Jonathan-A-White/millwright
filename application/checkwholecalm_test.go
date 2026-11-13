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
	return wholeCheckOnAStalledBusyHost(t, readings, wait, 0)
}

// stallingLoad is a host load that takes stall of real time over its first
// reading, as a host that is slow for a moment does, and says what the readings say.
type stallingLoad struct {
	*apptest.FakeHostLoad
	stall time.Duration
}

func (l *stallingLoad) Load(ctx context.Context) (application.LoadReading, error) {
	if l.Reads == 0 {
		time.Sleep(l.stall)
	}
	return l.FakeHostLoad.Load(ctx)
}

// wholeCheckOnAStalledBusyHost is wholeCheckOnABusyHost on a host that takes stall of
// real time before the wait for a calm host reads its bound. The check's clock
// (Check.CallNow) is moved by hand, by the waits and by nothing else, so the real
// time the stall takes is not time the check has spent (mw-gq6.991).
func wholeCheckOnAStalledBusyHost(t *testing.T, readings []application.LoadReading, wait, stall time.Duration) (check func() (application.CheckReport, error), checks *scriptedChecks, said *strings.Builder, waited *time.Duration) {
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
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	check = func() (application.CheckReport, error) {
		return application.Check{
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
			Slot: aSlot{}, Records: records, PartSlotWait: wait,
			CallNow:  func() time.Time { return now },
			Load:     &stallingLoad{FakeHostLoad: &apptest.FakeHostLoad{Readings: readings}, stall: stall},
			LoadPoll: 30 * time.Second, // LoadBound is left at its default, 15 minutes
			LoadWait: func(_ context.Context, d time.Duration) error { *waited += d; now = now.Add(d); return nil },
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

// The wait says since when the host has been busy by the check's clock, not the
// host's: a check whose clock a test holds at noon says noon (mw-gq6.1221).
func TestACheckWithNoPartOnAHostThatStaysBusySaysSinceWhenByTheChecksClock(t *testing.T) {
	check, _, said, _ := wholeCheckOnABusyHost(t, []application.LoadReading{busy()}, 3*time.Minute)

	if _, err := check(); err == nil {
		t.Fatal("expected the check not to pass while the host stayed busy")
	}
	if n := strings.Count(said.String(), "at load 39.0 of 20 cores since 12:00:00Z"); n != 3 {
		t.Errorf("expected 3 notices that say since 12:00:00Z, the check's clock at the start of the wait, got %d in %q", n, said.String())
	}
}

// A stall of real time before the wait reads its bound (the host was slow for a
// moment) is not time the check has spent: the notices are counted against the
// check's clock, which a test moves by hand (mw-gq6.991, seen in mw-gq6.983's gate).
func TestACheckWithNoPartOnAHostThatStallsBeforeItsWaitCountsTheSameNoticesAsOneThatDoesNot(t *testing.T) {
	check, _, said, waited := wholeCheckOnAStalledBusyHost(t, []application.LoadReading{busy()}, 3*time.Minute, 20*time.Millisecond)

	if _, err := check(); err == nil {
		t.Fatal("expected the check not to pass while the host stayed busy")
	}
	if *waited != 3*time.Minute {
		t.Errorf("expected a wait of the whole 3m, it waited %s", *waited)
	}
	if n := strings.Count(said.String(), "still waiting for laptop to calm"); n != 3 {
		t.Errorf("expected 3 notices in a 3m wait whatever the real time took, got %d in %q", n, said.String())
	}
	if !strings.Contains(said.String(), "(3m so far") {
		t.Errorf("expected the last notice, got %q", said.String())
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

// toolParts is a rig's gate in two parts that only run tools in the worktree, as the
// lampas gate's parts do, so that neither takes the merge slot; it notes which ran.
type toolParts struct{ ran []string }

func (p *toolParts) Parts(string) []application.GatePart {
	return []application.GatePart{{Name: "install", Command: "npm ci"}, {Name: "vitest 1/6", Command: "npx vitest run --shard 1/6"}}
}

func (p *toolParts) RunLine(_ context.Context, _, _, command string) (application.Checked, error) {
	p.ran = append(p.ran, command)
	return application.Checked{Command: command, Passed: true}, nil
}

// partOnABusyHost is mw check --part 2 of a rig whose gate is split, on a host
// whose load reads as readings (the last again for every read after), with a wait
// for a calm host that is counted and not slept (mw-gq6.987). A foreground part
// keeps its place, as the command line gives it.
func partOnABusyHost(t *testing.T, readings []application.LoadReading, wait time.Duration) (check func() (application.CheckReport, error), parts *toolParts, said *strings.Builder, waited *time.Duration, records *mapRecords) {
	t.Helper()
	return partOnAStalledBusyHost(t, readings, wait, 0)
}

// partOnAStalledBusyHost is partOnABusyHost on a host that takes stall of real time
// before the wait for a calm host reads its bound. The check's clock (Check.CallNow)
// is moved by hand, by the waits and by nothing else, so the real time the stall
// takes is not time the part has spent (mw-gq6.1135).
func partOnAStalledBusyHost(t *testing.T, readings []application.LoadReading, wait, stall time.Duration) (check func() (application.CheckReport, error), parts *toolParts, said *strings.Builder, waited *time.Duration, records *mapRecords) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parts, records = &toolParts{}, &mapRecords{}
	said, waited = &strings.Builder{}, new(time.Duration)
	now := time.Now()
	check = func() (application.CheckReport, error) {
		return application.Check{
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
			Slot: aSlot{}, Parts: parts, Records: records, Part: 2, PartSlotWait: wait, KeepPlace: application.CheckKeepPlace,
			CallNow:  func() time.Time { return now },
			Load:     &stallingLoad{FakeHostLoad: &apptest.FakeHostLoad{Readings: readings}, stall: stall},
			LoadPoll: 30 * time.Second,
			LoadWait: func(_ context.Context, d time.Duration) error { *waited += d; now = now.Add(d); return nil },
			Host:     "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": rigDir},
			Out:  said,
		}.Run(context.Background(), "mw-x.1")
	}
	return check, parts, said, waited, records
}

func TestAPartOnAHostThatStaysBusyWaitsForItToCalmThenStartsNothingAndEndsInOneLine(t *testing.T) {
	check, parts, said, waited, records := partOnABusyHost(t, []application.LoadReading{busy()}, 3*time.Minute)

	_, err := check()

	if err == nil {
		t.Fatal("expected the part not to pass while the host stayed busy")
	}
	if len(parts.ran) != 0 {
		t.Errorf("expected the part not to start on a busy host, it ran %v", parts.ran)
	}
	if *waited != 3*time.Minute {
		t.Errorf("expected a wait of the whole 3m, it waited %s", *waited)
	}
	line := err.Error()
	for _, want := range []string{"load 39.0 of 20 cores", "since ", "`mw check mw-x.1 --part 2`", "part 2 of 2 (vitest 1/6)"} {
		if !strings.Contains(line, want) {
			t.Errorf("expected the line to say %q, got %q", want, line)
		}
	}
	if strings.Contains(line, "\n") {
		t.Errorf("expected one line, got %q", line)
	}
	if n := strings.Count(said.String(), "still waiting for laptop to calm"); n != 3 {
		t.Errorf("expected a notice every minute of a 3m wait (3 of them), got %d in %q", n, said.String())
	}
	if _, rerr := records.ReadRunFile(context.Background(), "mw-x.1", application.CheckPartFileName(1, 2)); rerr == nil {
		t.Error("expected a part that did not start to leave no record of a run")
	}
}

// A stall of real time before the wait reads its bound (the host was slow for a
// moment) is not time the part has spent: the notices are counted against the
// check's clock, which a test moves by hand (mw-gq6.1135).
func TestAPartOnAHostThatStallsBeforeItsWaitCountsTheSameNoticesAsOneThatDoesNot(t *testing.T) {
	check, parts, said, waited, _ := partOnAStalledBusyHost(t, []application.LoadReading{busy()}, 3*time.Minute, 20*time.Millisecond)

	if _, err := check(); err == nil {
		t.Fatal("expected the part not to pass while the host stayed busy")
	}
	if len(parts.ran) != 0 {
		t.Errorf("expected the part not to start on a busy host, it ran %v", parts.ran)
	}
	if *waited != 3*time.Minute {
		t.Errorf("expected a wait of the whole 3m, it waited %s", *waited)
	}
	if n := strings.Count(said.String(), "still waiting for laptop to calm"); n != 3 {
		t.Errorf("expected 3 notices in a 3m wait whatever the real time took, got %d in %q", n, said.String())
	}
}

func TestAPartRunsOnceTheHostCalmsWithinItsWaitAndNotBefore(t *testing.T) {
	check, parts, _, waited, _ := partOnABusyHost(t, []application.LoadReading{farOver(), busy(), calm()}, 5*time.Minute)

	_, err := check()

	if err != nil {
		t.Fatalf("expected the part to pass once the host calmed, got %v", err)
	}
	if *waited != time.Minute {
		t.Errorf("expected two polls of 30s before the host calmed, it waited %s", *waited)
	}
	if len(parts.ran) != 1 || parts.ran[0] != "npx vitest run --shard 1/6" {
		t.Errorf("expected part 2 to run once, ran %v", parts.ran)
	}
}

func TestAPartOnACalmHostRunsAtOnceWithNoWait(t *testing.T) {
	check, parts, _, waited, _ := partOnABusyHost(t, []application.LoadReading{calm()}, 5*time.Minute)

	if _, err := check(); err != nil {
		t.Fatalf("expected the part to pass, got %v", err)
	}
	if *waited != 0 || len(parts.ran) != 1 {
		t.Errorf("expected no wait and one run, waited %s and ran %v", *waited, parts.ran)
	}
}

func TestAPartHandedBackOnTheHostsLoadTwiceInOneAttemptCountsTheSecondOnce(t *testing.T) {
	check, _, _, _, _ := partOnABusyHost(t, []application.LoadReading{busy()}, time.Minute)

	_, _ = check()
	_, err := check()

	if err == nil {
		t.Fatal("expected the second try to be handed back too")
	}
	for _, want := range []string{"counted once for this attempt", "not a failure", "`mw check mw-x.1 --part 2`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the repeat to say %q, got %q", want, err)
		}
	}
}

func TestAPartThatTheCalmWaitLeftNoRoomForInItsCallIsNotStartedAndSaysSo(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	parts := &toolParts{}
	_, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
		Parts: parts, Records: &mapRecords{}, Part: 2, PartSlotWait: 9 * time.Minute, KeepPlace: application.CheckKeepPlace,
		CallBudget: application.CheckCallBudget, CallNow: func() time.Time { return now },
		PartExpected: func(context.Context, string, int) time.Duration { return 4 * time.Minute },
		Load:         &apptest.FakeHostLoad{Readings: []application.LoadReading{busy(), busy(), calm()}},
		LoadPoll:     3 * time.Minute,
		LoadWait:     func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil },
		Host:         "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": rigDir},
		Out:  &strings.Builder{},
	}.Run(context.Background(), "mw-x.1")

	if err == nil {
		t.Fatal("expected the part not to start with 3m left of its call and 4m needed")
	}
	for _, want := range []string{"did not start", "needs about 4m", "to calm", "`mw check mw-x.1 --part 2`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the line to say %q, got %q", want, err)
		}
	}
	if len(parts.ran) != 0 {
		t.Errorf("expected nothing to run, ran %v", parts.ran)
	}
}
