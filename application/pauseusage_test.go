package application_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
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
	if tripped, _ := application.UsageTripped(89.9, resets, now); tripped {
		t.Error("89.9% tripped with a week left")
	}
	if tripped, _ := application.UsageTripped(90, resets, now); !tripped {
		t.Error("90% did not trip with a week left")
	}
	if tripped, _ := application.UsageTripped(99.9, now.Add(time.Hour), now); tripped {
		t.Error("99.9% tripped with an hour left")
	}
	if tripped, _ := application.UsageTripped(100, now.Add(time.Hour), now); !tripped {
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
