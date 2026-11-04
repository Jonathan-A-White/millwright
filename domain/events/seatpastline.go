package events

import (
	"fmt"
	"strings"
)

// SeatPastLine is what a seat_past_line event says in its detail: a seat's
// session, past the context size at which it must hand off, stopped with mail
// still unread, and so its window was closed and a successor sprung to read it.
type SeatPastLine struct {
	Seat      string
	Window    string
	Tokens    int
	HandoffAt int
	// Unread is how many unread mails waited for the seat.
	Unread int
}

// Detail is the text a seat_past_line event carries:
// "deputy in deputy-2026-11-04-02: context 231000 past 200000, 5 unread".
func (p SeatPastLine) Detail() string {
	return fmt.Sprintf("%s in %s: context %d past %d, %d unread", p.Seat, p.Window, p.Tokens, p.HandoffAt, p.Unread)
}

// ParseSeatPastLine reads a seat_past_line event's detail, false when it is not
// one.
func ParseSeatPastLine(detail string) (SeatPastLine, bool) {
	head, tail, ok := strings.Cut(detail, ": context ")
	f := strings.Fields(head)
	if !ok || len(f) != 3 || f[1] != "in" {
		return SeatPastLine{}, false
	}
	var tokens, handoffAt, unread int
	if n, err := fmt.Sscanf(tail, "%d past %d, %d unread", &tokens, &handoffAt, &unread); err != nil || n != 3 {
		return SeatPastLine{}, false
	}
	return SeatPastLine{Seat: f[0], Window: f[2], Tokens: tokens, HandoffAt: handoffAt, Unread: unread}, true
}

// SeatPastLineOf is the stop ev carries, false when ev is no seat_past_line
// event or its detail is unreadable.
func SeatPastLineOf(ev Event) (SeatPastLine, bool) {
	if ev.Kind != KindSeatPastLine {
		return SeatPastLine{}, false
	}
	return ParseSeatPastLine(ev.Detail)
}
