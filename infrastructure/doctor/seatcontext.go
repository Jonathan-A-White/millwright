package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*SeatContext)(nil)

// SeatContextName is what the check is called: in the log, and on the command
// line as `mw doctor seat-context`.
const SeatContextName = "seat-context"

// The seat-context check's damper: it never cures, so it is the shortest.
const (
	SeatContextDamperWait = 0 * time.Second
	SeatContextDamperCap  = 1
)

var errSeatContextNoCure = errors.New(
	"no cure: the session hands off by itself, told by the watcher in mw events follow; a person tells it or closes the window")

// SeatContext is the check that no seat's window is far past the context at
// which its session must hand off. The watcher in mw events follow has typed
// the line by then (application.SeatContextWatch); a window still running
// application.SeatContextRetell or more past its limit has not heeded it, or
// nothing is watching. It never cures.
type SeatContext struct {
	// Read is application.SeatContextWatch.Readings.
	Read func(ctx context.Context) ([]application.SeatWindowContext, error)
}

// NewSeatContext is the check over the readings read gives.
func NewSeatContext(read func(ctx context.Context) ([]application.SeatWindowContext, error)) *SeatContext {
	return &SeatContext{Read: read}
}

// Name implements application.DoctorCheck.
func (s *SeatContext) Name() string { return SeatContextName }

// Probe implements application.DoctorCheck: faulty naming each window
// SeatContextRetell or more past its seat's limit; cannot-tell when the
// windows or their transcripts cannot be read at all.
func (s *SeatContext) Probe(ctx context.Context) (application.Verdict, string) {
	readings, err := s.Read(ctx)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	var over []string
	for _, r := range readings {
		if r.Over(application.SeatContextRetell) {
			over = append(over, fmt.Sprintf("window %s is at %d tokens, past its %s handoff_at of %d by %d",
				r.Window.Name, r.Tokens, r.Seat, r.HandoffAt, r.Tokens-r.HandoffAt))
		}
	}
	if len(over) > 0 {
		return application.DoctorFaulty, strings.Join(over, "; ")
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: there is none.
func (s *SeatContext) Cure(context.Context) error { return errSeatContextNoCure }

// Damper implements application.DoctorCheck.
func (s *SeatContext) Damper() (time.Duration, int) {
	return SeatContextDamperWait, SeatContextDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes.
func (s *SeatContext) WayBack() string {
	return "none: no cure runs; tell the session to write its handoff and stop"
}
