package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func TestTheReserveIsAStraightLineFromTenPercentToNothing(t *testing.T) {
	for left, want := range map[time.Duration]float64{
		application.UsageWeek:     10,
		2 * application.UsageWeek: 10,
		time.Hour:                 0,
		30 * time.Minute:          0,
		-time.Hour:                0,
	} {
		if got := application.UsageReserve(left); got != want {
			t.Errorf("reserve with %s left = %v, want %v", left, got, want)
		}
	}
	mid := application.UsageReserve((application.UsageWeek + time.Hour) / 2)
	if mid < 4.999 || mid > 5.001 {
		t.Errorf("halfway between an hour and a week the reserve is %v, want 5", mid)
	}
}

func TestTheUsageTripsAtOneHundredLessTheReserve(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	resets := now.Add(application.UsageWeek)
	if tripped, _ := application.UsageTripped(89.9, resets, now, application.UsageFloor{}); tripped {
		t.Error("89.9% tripped with a week left")
	}
	if tripped, _ := application.UsageTripped(90, resets, now, application.UsageFloor{}); !tripped {
		t.Error("90% did not trip with a week left")
	}
	if tripped, _ := application.UsageTripped(99.9, now.Add(time.Hour), now, application.UsageFloor{}); tripped {
		t.Error("99.9% tripped with an hour left")
	}
	if tripped, _ := application.UsageTripped(100, now.Add(time.Hour), now, application.UsageFloor{}); !tripped {
		t.Error("100% did not trip with an hour left")
	}
}

func TestTheNextWeeklyResetIsTheNextFridayAtNineInTheZone(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no zone data")
	}
	thursday := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	if got := application.NextWeeklyReset(thursday, zone).UTC(); !got.Equal(time.Date(2026, 10, 23, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("after a Thursday noon the reset is %s", got)
	}
	fridayLater := time.Date(2026, 10, 23, 14, 0, 0, 0, time.UTC)
	if got := application.NextWeeklyReset(fridayLater, zone).UTC(); !got.Equal(time.Date(2026, 10, 30, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("after the Friday reset it is %s", got)
	}
}

func TestAFloorHoldsTheReserveUpUntilItsTimeAndThenAllOut(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	resets := now.Add(30 * time.Minute) // R(t) is 0
	until := now.Add(time.Hour)
	floor := application.UsageFloor{Percent: 3, Until: until}

	if tripped, reserve := application.UsageTripped(96.9, resets, now, floor); tripped || reserve != 3 {
		t.Errorf("96.9%% with the floor on: tripped %v, reserve %v; want not tripped, 3", tripped, reserve)
	}
	if tripped, reserve := application.UsageTripped(97, resets, now, floor); !tripped || reserve != 3 {
		t.Errorf("97%% with the floor on: tripped %v, reserve %v; want tripped, 3", tripped, reserve)
	}
	// R(t) above the floor still wins.
	week := now.Add(application.UsageWeek)
	if _, reserve := application.UsageTripped(50, week, now, floor); reserve != application.UsageReserveMax {
		t.Errorf("with a week left and a floor of 3 the reserve is %v, want %v", reserve, application.UsageReserveMax)
	}
	// At and after until, R(t) alone: here 0, so 97% no longer trips and 100% does.
	for _, at := range []time.Time{until, until.Add(time.Minute)} {
		r := at.Add(30 * time.Minute)
		if tripped, reserve := application.UsageTripped(99.9, r, at, floor); tripped || reserve != 0 {
			t.Errorf("at %s, after the floor's end: 99.9%% tripped %v, reserve %v; want not tripped, 0", at, tripped, reserve)
		}
		if tripped, _ := application.UsageTripped(100, r, at, floor); !tripped {
			t.Errorf("at %s, after the floor's end: 100%% did not trip", at)
		}
	}
	// No floor kept, or off: R(t) alone.
	if _, reserve := application.UsageTripped(97, resets, now, application.UsageFloor{}); reserve != 0 {
		t.Errorf("with no floor the reserve is %v, want 0", reserve)
	}
}

func TestTheFloorIsKeptAndClearedInANote(t *testing.T) {
	notes := apptest.NewFakeTracker()
	floors := application.UsageFloors{Notes: notes}
	if got, err := floors.Get(context.Background()); err != nil || got != (application.UsageFloor{}) {
		t.Fatalf("a floor before one was set: %+v, %v", got, err)
	}
	until := time.Date(2026, 10, 30, 11, 0, 0, 0, time.UTC)
	if err := floors.Set(context.Background(), application.UsageFloor{Percent: 3, Until: until}); err != nil {
		t.Fatal(err)
	}
	got, err := floors.Get(context.Background())
	if err != nil || got.Percent != 3 || !got.Until.Equal(until) {
		t.Fatalf("the floor kept: %+v, %v", got, err)
	}
	if err := floors.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := floors.Get(context.Background()); got != (application.UsageFloor{}) {
		t.Errorf("a floor after off: %+v", got)
	}
}
