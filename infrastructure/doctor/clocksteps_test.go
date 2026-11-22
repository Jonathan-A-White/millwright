package doctor_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// stepClock is an injected pair of clocks: the monotonic one advances only in
// Sleep, and the wall one moves with it plus whatever steps are scripted at a
// given sample.
type stepClock struct {
	start time.Time
	mono  time.Duration
	skew  time.Duration
	reads int
	// stepAt maps the read number to the wall clock's step at that read: a
	// negative duration is a step back.
	stepAt map[int]time.Duration
	slept  time.Duration
}

func newStepClock(stepAt map[int]time.Duration) *stepClock {
	return &stepClock{start: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC), stepAt: stepAt}
}

func (c *stepClock) Read() doctor.ClockReading {
	c.reads++
	c.skew += c.stepAt[c.reads]
	return doctor.ClockReading{Wall: c.start.Add(c.mono + c.skew), Mono: c.mono}
}

func (c *stepClock) Sleep(_ context.Context, d time.Duration) {
	c.mono += d
	c.slept += d
}

func (c *stepClock) check(virt string) *doctor.ClockSteps {
	check := doctor.NewClockSteps()
	check.Read, check.Sleep = c.Read, c.Sleep
	check.DetectVirt = func(context.Context) (string, error) { return virt, nil }
	return check
}

func TestClockStepsNoStepIsOK(t *testing.T) {
	clock := newStepClock(nil)
	verdict, reason := clock.check("wsl").Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %v: %s", verdict, reason)
	}
	if clock.slept < 90*time.Second {
		t.Errorf("expected a sample of 90 s, slept %v", clock.slept)
	}
}

func TestClockStepsOneStepOf1300msIsFaultyAndNamed(t *testing.T) {
	clock := newStepClock(map[int]time.Duration{40: -1300 * time.Millisecond})
	verdict, reason := clock.check("wsl").Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %v: %s", verdict, reason)
	}
	for _, want := range []string{"1 step", "1.3 s", "hv_utils.timesync_implicit=0"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected %q in %q", want, reason)
		}
	}
}

func TestClockStepsCountsEveryStepAndNamesTheLargest(t *testing.T) {
	clock := newStepClock(map[int]time.Duration{100: -1300 * time.Millisecond, 400: -2100 * time.Millisecond, 700: -400 * time.Millisecond})
	verdict, reason := clock.check("wsl").Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %v: %s", verdict, reason)
	}
	if !strings.Contains(reason, "3 steps") || !strings.Contains(reason, "largest 2.1 s") {
		t.Errorf("expected 3 steps, largest 2.1 s, got %q", reason)
	}
}

func TestClockStepsAForwardJumpIsNoFault(t *testing.T) {
	clock := newStepClock(map[int]time.Duration{40: 5 * time.Second})
	if verdict, reason := clock.check("wsl").Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("a forward jump is not a fault, got %v: %s", verdict, reason)
	}
}

func TestClockStepsABackStepOf100msOrLessIsNoFault(t *testing.T) {
	clock := newStepClock(map[int]time.Duration{40: -100 * time.Millisecond})
	if verdict, reason := clock.check("wsl").Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("a step of exactly 100 ms is under the line, got %v: %s", verdict, reason)
	}
}

func TestClockStepsIsInertOffWSL(t *testing.T) {
	clock := newStepClock(map[int]time.Duration{40: -1300 * time.Millisecond})
	verdict, reason := clock.check("none").Probe(context.Background())
	if verdict != application.DoctorOK || !strings.Contains(reason, "not a WSL host") {
		t.Fatalf("expected ok, not a WSL host, got %v: %s", verdict, reason)
	}
	if clock.reads != 0 {
		t.Errorf("expected no sample off WSL, read %d times", clock.reads)
	}
}

func TestClockStepsSamplesOnceAnHourAndRepeatsWhatItFound(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	store := doctor.New(t.TempDir())
	clock := newStepClock(map[int]time.Duration{40: -1300 * time.Millisecond})
	check := clock.check("wsl")
	check.State, check.Now = store, func() time.Time { return now }
	_, first := check.Probe(context.Background())
	reads := clock.reads

	now = now.Add(30 * time.Minute)
	verdict, again := check.Probe(context.Background())
	if clock.reads != reads {
		t.Errorf("expected no new sample inside the hour, read %d more times", clock.reads-reads)
	}
	if verdict != application.DoctorFaulty || again != first {
		t.Errorf("expected the last finding again, got %v %q want %q", verdict, again, first)
	}

	now = now.Add(31 * time.Minute)
	check.Probe(context.Background())
	if clock.reads == reads {
		t.Error("expected a new sample after the hour")
	}
}

func TestClockStepsEndedEarlyCannotTell(t *testing.T) {
	clock := newStepClock(nil)
	ctx, cancel := context.WithCancel(context.Background())
	check := clock.check("wsl")
	check.Sleep = func(context.Context, time.Duration) { cancel() }
	if verdict, _ := check.Probe(ctx); verdict != application.DoctorCannotTell {
		t.Fatalf("a sample cut short cannot tell, got %v", verdict)
	}
}

func TestClockStepsCureIsOneNote(t *testing.T) {
	clock := newStepClock(map[int]time.Duration{40: -1300 * time.Millisecond})
	check := clock.check("wsl")
	var notes []string
	check.Note = func(_ context.Context, text string) error { notes = append(notes, text); return nil }
	check.Probe(context.Background())
	if err := check.Cure(context.Background()); err != nil || len(notes) != 1 || !strings.Contains(notes[0], "clock-steps") {
		t.Fatalf("expected one note naming the check, got %v %v", notes, err)
	}
}

func TestClockStepsADryRunSamplesAndSavesNothingSoItsHourIsLeftAlone(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	store := doctor.New(t.TempDir())
	clock := newStepClock(map[int]time.Duration{40: -1300 * time.Millisecond})
	check := clock.check("wsl")
	check.State, check.Now = store, func() time.Time { return now }
	doc := application.Doctor{Checks: application.DoctorChecks{check}, State: store, Log: store,
		Host: "desktop", Now: func() time.Time { return now }}
	for i := 0; i < 2; i++ {
		if _, err := doc.Run(context.Background(), doctor.ClockStepsName, true); err != nil {
			t.Fatal(err)
		}
		kept, err := store.Load(context.Background(), doctor.ClockStepsSampledStateName)
		if err != nil {
			t.Fatal(err)
		}
		if !kept.FirstFaulty.IsZero() || len(kept.SeenPaths) != 0 {
			t.Fatalf("dry run %d saved the hourly state: %+v", i+1, kept)
		}
	}
}

func TestClockStepsADryRunInsideTheHourLeavesTheKeptClockAlone(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	store := doctor.New(t.TempDir())
	clock := newStepClock(nil)
	check := clock.check("wsl")
	check.State, check.Now = store, func() time.Time { return now }
	check.Probe(context.Background())
	before, _ := store.Load(context.Background(), doctor.ClockStepsSampledStateName)
	if before.FirstFaulty.IsZero() {
		t.Fatal("a real run keeps its clock")
	}
	now = now.Add(61 * time.Minute)
	doc := application.Doctor{Checks: application.DoctorChecks{check}, State: store, Log: store,
		Host: "desktop", Now: func() time.Time { return now }}
	if _, err := doc.Run(context.Background(), doctor.ClockStepsName, true); err != nil {
		t.Fatal(err)
	}
	after, _ := store.Load(context.Background(), doctor.ClockStepsSampledStateName)
	if !after.FirstFaulty.Equal(before.FirstFaulty) {
		t.Errorf("a dry run moved the kept clock from %v to %v", before.FirstFaulty, after.FirstFaulty)
	}
}
