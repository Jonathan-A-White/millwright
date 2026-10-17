package events_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

func TestASeatLimitedDetailReadsBack(t *testing.T) {
	l := events.SeatLimited{
		Seat: "deputy", Window: "deputy", Resets: time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC),
		Notice: "You've hit your weekly limit · resets Oct 16, 7am (UTC)",
	}
	back, ok := events.ParseSeatLimited(l.Detail())
	if !ok || back != l {
		t.Fatalf("expected %+v back from %q, got %+v (%v)", l, l.Detail(), back, ok)
	}
	bare := events.SeatLimited{Seat: "mayor", Window: "@3", Resets: l.Resets}
	back, ok = events.ParseSeatLimited(bare.Detail())
	if !ok || back != bare {
		t.Fatalf("expected %+v back from %q, got %+v (%v)", bare, bare.Detail(), back, ok)
	}
	got, ok := events.SeatLimitedOf(events.Event{Kind: events.KindSeatLimited, Detail: l.Detail()})
	if !ok || got != l {
		t.Errorf("expected the event to carry %+v, got %+v (%v)", l, got, ok)
	}
}

func TestOnlyASeatLimitedEventWithAReadableDetailIsOne(t *testing.T) {
	for _, detail := range []string{"", "deputy", "deputy in deputy until soon", "deputy at deputy until 2026-10-16T07:00:00Z"} {
		if _, ok := events.ParseSeatLimited(detail); ok {
			t.Errorf("expected %q to be no seat_limited detail", detail)
		}
	}
	if _, ok := events.SeatLimitedOf(events.Event{Kind: events.KindHandover, Detail: "deputy in deputy until 2026-10-16T07:00:00Z"}); ok {
		t.Error("expected a handover event to carry no limit")
	}
}
