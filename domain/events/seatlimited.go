package events

import (
	"strings"
	"time"
)

// SeatLimitedTimeFormat is how a seat_limited event's detail writes the reset.
const SeatLimitedTimeFormat = time.RFC3339

// SeatLimited is what a seat_limited event says in its detail: the seat whose
// pane was found stopped at the harness's usage limit, the window it is in,
// when the limit resets, and the harness's own words when it said any.
type SeatLimited struct {
	Seat   string
	Window string
	Resets time.Time
	Notice string
}

// Detail is the text a seat_limited event carries:
// "deputy in deputy until 2026-10-16T07:00:00Z: You've hit your weekly limit · resets 7am".
func (l SeatLimited) Detail() string {
	d := l.Seat + " in " + l.Window + " until " + l.Resets.UTC().Format(SeatLimitedTimeFormat)
	if l.Notice != "" {
		d += ": " + l.Notice
	}
	return d
}

// ParseSeatLimited reads a seat_limited event's detail, false when it is not
// one.
func ParseSeatLimited(detail string) (SeatLimited, bool) {
	head, notice, _ := strings.Cut(detail, ": ")
	f := strings.Fields(head)
	if len(f) != 5 || f[1] != "in" || f[3] != "until" {
		return SeatLimited{}, false
	}
	resets, err := time.Parse(SeatLimitedTimeFormat, f[4])
	if err != nil {
		return SeatLimited{}, false
	}
	return SeatLimited{Seat: f[0], Window: f[2], Resets: resets, Notice: notice}, true
}

// SeatLimitedOf is the limit ev carries, false when ev is no seat_limited
// event or its detail is unreadable.
func SeatLimitedOf(ev Event) (SeatLimited, bool) {
	if ev.Kind != KindSeatLimited {
		return SeatLimited{}, false
	}
	return ParseSeatLimited(ev.Detail)
}
