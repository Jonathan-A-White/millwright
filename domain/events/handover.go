package events

import (
	"fmt"
	"strconv"
	"strings"
)

// Handover is what a handover event says in its detail: the seat that hands
// over, the window of the session it hands to, and At, the seq of the last
// event the old session answers. The successor reads from At; the old session
// answers nothing past it.
type Handover struct {
	Seat      string
	Successor string
	At        uint64
}

// Detail is the text a handover event carries: "mayor to mayor-2026-10-01-160 at 105".
func (h Handover) Detail() string {
	return fmt.Sprintf("%s to %s at %d", h.Seat, h.Successor, h.At)
}

// ParseHandover reads a handover event's detail, false when it is not one.
func ParseHandover(detail string) (Handover, bool) {
	f := strings.Fields(detail)
	if len(f) != 5 || f[1] != "to" || f[3] != "at" {
		return Handover{}, false
	}
	at, err := strconv.ParseUint(f[4], 10, 64)
	if err != nil {
		return Handover{}, false
	}
	return Handover{Seat: f[0], Successor: f[2], At: at}, true
}

// HandoverOf is the handover ev carries, false when ev is no handover event
// or its detail is unreadable.
func HandoverOf(ev Event) (Handover, bool) {
	if ev.Kind != KindHandover {
		return Handover{}, false
	}
	return ParseHandover(ev.Detail)
}
