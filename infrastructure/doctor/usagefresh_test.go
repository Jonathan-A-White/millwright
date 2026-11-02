package doctor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

var usageFreshNow = time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC)

// usageFreshCheck is the check over one reading taken the given age before
// usageFreshNow, its week resetting in the given time from usageFreshNow.
func usageFreshCheck(t *testing.T, age, resetsIn time.Duration) *doctor.UsageFresh {
	t.Helper()
	notes := apptest.NewFakeTracker()
	readings := application.UsageReadings{Notes: notes}
	if age >= 0 {
		err := readings.Add(context.Background(), application.UsageReading{
			Percent: 95, At: usageFreshNow.Add(-age), Resets: usageFreshNow.Add(resetsIn),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	check := doctor.NewUsageFresh(readings)
	check.Now = func() time.Time { return usageFreshNow }
	return check
}

func TestUsageFreshIsOKForAFreshReading(t *testing.T) {
	verdict, reason := usageFreshCheck(t, 30*time.Minute, 24*time.Hour).Probe(context.Background())
	if verdict != application.DoctorOK || reason != "" {
		t.Errorf("a reading 30 minutes old: %v %q, want ok", verdict, reason)
	}
}

func TestUsageFreshIsFaultyAtMoreThanSeventyMinutesAndOKAtSeventy(t *testing.T) {
	if verdict, _ := usageFreshCheck(t, 70*time.Minute, 24*time.Hour).Probe(context.Background()); verdict != application.DoctorOK {
		t.Errorf("a reading exactly 70 minutes old: %v, want ok", verdict)
	}
	verdict, reason := usageFreshCheck(t, 11*time.Hour+2*time.Minute, 24*time.Hour).Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("a reading 11h2m old: %v, want faulty", verdict)
	}
	for _, want := range []string{"11h2m old", "list-timers | grep usage"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the fault should say %q, got %q", want, reason)
		}
	}
}

func TestUsageFreshIsFaultyForAReadingTakenBeforeAResetThatPassed(t *testing.T) {
	verdict, reason := usageFreshCheck(t, 10*time.Minute, -5*time.Minute).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "taken before the week reset at 2026-10-30T19:55:00Z") {
		t.Errorf("a young reading from before the reset: %v %q", verdict, reason)
	}
}

func TestUsageFreshIsFaultyWhenNoReadingWasEverKept(t *testing.T) {
	verdict, reason := usageFreshCheck(t, -1, 0).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "list-timers | grep usage") {
		t.Errorf("no reading: %v %q, want faulty naming the fix", verdict, reason)
	}
}

func TestUsageFreshAlarmsTheGovernorOnAStaleReading(t *testing.T) {
	check := usageFreshCheck(t, 11*time.Hour, 24*time.Hour)
	var alarms []string
	check.Alarm = func(_ context.Context, text string) (uint64, error) {
		alarms = append(alarms, text)
		return 1, nil
	}
	store := doctor.New(t.TempDir())
	if _, err := (application.Doctor{
		Checks: application.DoctorChecks{check}, State: store, Log: store, Now: func() time.Time { return usageFreshNow },
	}).Run(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	if len(alarms) != 1 || !strings.Contains(alarms[0], "11h old") {
		t.Errorf("a stale reading: alarms %q, want one naming the age", alarms)
	}
}

// usageFreshAt is the check over one reading of percent, taken age before
// usageFreshNow, its week resetting in a day; the readings are returned so a
// later reading can be added.
func usageFreshAt(t *testing.T, percent float64, age time.Duration) (*doctor.UsageFresh, application.UsageReadings) {
	t.Helper()
	readings := application.UsageReadings{Notes: apptest.NewFakeTracker()}
	if age >= 0 {
		err := readings.Add(context.Background(), application.UsageReading{
			Percent: percent, At: usageFreshNow.Add(-age), Resets: usageFreshNow.Add(24 * time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	check := doctor.NewUsageFresh(readings)
	check.Now = func() time.Time { return usageFreshNow }
	return check, readings
}

// usageFreshRun runs the check once through the Doctor, as mw doctor does, and
// gives the alarms sent and the clears (the seqs they name).
func usageFreshRun(t *testing.T, check *doctor.UsageFresh, store application.DoctorState, now time.Time) (alarms []string, clears []uint64) {
	t.Helper()
	check.State = store
	check.Alarm = func(_ context.Context, text string) (uint64, error) {
		alarms = append(alarms, text)
		return uint64(40 + len(alarms)), nil
	}
	check.Clear = func(_ context.Context, _ string, seq uint64) error {
		clears = append(clears, seq)
		return nil
	}
	logStore := doctor.New(t.TempDir())
	if _, err := (application.Doctor{
		Checks: application.DoctorChecks{check}, State: store, Log: logStore, Now: func() time.Time { return now },
	}).Run(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	return alarms, clears
}

func TestUsageFreshSendsNoAlarmForAStaleReadingWithRoomToSpare(t *testing.T) {
	// 41% read 72 minutes ago: the pause trips near 98.6% with a day left.
	check, _ := usageFreshAt(t, 41, 72*time.Minute)
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("a stale reading with room: %v %q, want ok", verdict, reason)
	}
	for _, want := range []string{"STALE", "no page", "mw status"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the probe should say %q, got %q", want, reason)
		}
	}
	alarms, _ := usageFreshRun(t, check, doctor.New(t.TempDir()), usageFreshNow)
	if len(alarms) != 0 {
		t.Errorf("a stale reading with room: alarms %q, want none", alarms)
	}
}

func TestUsageFreshAlarmsForEachReasonThatPages(t *testing.T) {
	cases := []struct {
		name    string
		percent float64
		age     time.Duration
		resets  time.Duration
		none    bool
		want    string
	}{
		{"within the margin of the trip", 85, 72 * time.Minute, 24 * time.Hour, false, "85.0%"},
		{"no reading at all", 0, 0, 24 * time.Hour, true, "no usage reading was ever kept"},
		{"taken before a reset that passed", 41, 10 * time.Minute, -5 * time.Minute, false, "taken before the week reset"},
		{"over the age limit", 41, 6*time.Hour + time.Minute, 24 * time.Hour, false, "6h1m old"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			readings := application.UsageReadings{Notes: apptest.NewFakeTracker()}
			if !c.none {
				err := readings.Add(context.Background(), application.UsageReading{
					Percent: c.percent, At: usageFreshNow.Add(-c.age), Resets: usageFreshNow.Add(c.resets),
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			check := doctor.NewUsageFresh(readings)
			check.Now = func() time.Time { return usageFreshNow }
			if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorFaulty {
				t.Errorf("verdict %v, want faulty", verdict)
			}
			alarms, _ := usageFreshRun(t, check, doctor.New(t.TempDir()), usageFreshNow)
			if len(alarms) != 1 || !strings.Contains(alarms[0], c.want) {
				t.Errorf("alarms %q, want one naming %q", alarms, c.want)
			}
		})
	}
}

func TestUsageFreshDoesNotPageAtExactlySixHoursOrExactlyTheMargin(t *testing.T) {
	check, _ := usageFreshAt(t, 41, 6*time.Hour)
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Errorf("a reading exactly 6h old at 41%%: %v %q, want ok", verdict, reason)
	}
	// 24h left: the reserve is 10*(23h)/(167h) of the quota, the trip 100 less it.
	trip := 100 - application.UsageReserve(24*time.Hour)
	room, _ := usageFreshAt(t, trip-20.1, 2*time.Hour)
	if verdict, reason := room.Probe(context.Background()); verdict != application.DoctorOK {
		t.Errorf("20.1 points under the trip: %v %q, want ok", verdict, reason)
	}
	near, _ := usageFreshAt(t, trip-19.9, 2*time.Hour)
	if verdict, _ := near.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Errorf("19.9 points under the trip: %v, want faulty", verdict)
	}
}

func TestUsageFreshClearsItsAlarmWhenAFreshReadingArrives(t *testing.T) {
	check, readings := usageFreshAt(t, 41, 7*time.Hour)
	store := doctor.New(t.TempDir())
	alarms, clears := usageFreshRun(t, check, store, usageFreshNow)
	if len(alarms) != 1 || len(clears) != 0 {
		t.Fatalf("a 7h old reading: alarms %q clears %v, want one alarm and no clear", alarms, clears)
	}
	err := readings.Add(context.Background(), application.UsageReading{
		Percent: 42, At: usageFreshNow.Add(-5 * time.Minute), Resets: usageFreshNow.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, clears = usageFreshRun(t, check, store, usageFreshNow.Add(time.Minute))
	if len(clears) != 1 || clears[0] != 41 {
		t.Fatalf("a fresh reading: clears %v, want one naming seq 41", clears)
	}
	_, clears = usageFreshRun(t, check, store, usageFreshNow.Add(2*time.Minute))
	if len(clears) != 0 {
		t.Errorf("a second fresh run: clears %v, want none (the alarm is forgotten)", clears)
	}
}

func TestUsageFreshSaysWhetherTheTimerIsInstalled(t *testing.T) {
	cases := []struct {
		name  string
		state doctor.UsageTimerState
		err   error
		want  string
		not   string
	}{
		{"not installed", doctor.UsageTimerState{}, nil, "is not installed", "has not run"},
		{"installed but off", doctor.UsageTimerState{Installed: true}, nil, "is installed but not turned on", "is not installed"},
		{"on", doctor.UsageTimerState{Installed: true, Active: true}, nil, "is on, but", "is not installed"},
		{"cannot be asked", doctor.UsageTimerState{}, errors.New("no bus"), "could not be asked", "is not installed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			check, _ := usageFreshAt(t, 41, 7*time.Hour)
			check.Timer = func(context.Context) (doctor.UsageTimerState, error) { return c.state, c.err }
			verdict, reason := check.Probe(context.Background())
			if verdict != application.DoctorFaulty {
				t.Fatalf("verdict %v, want faulty", verdict)
			}
			if !strings.Contains(reason, c.want) || strings.Contains(reason, c.not) {
				t.Errorf("the fault should say %q and not %q, got %q", c.want, c.not, reason)
			}
		})
	}
}
