package doctor_test

import (
	"context"
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
	for _, want := range []string{"11h2m old", "the mw-usage-read timer has not run: systemctl --user list-timers | grep usage"} {
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
