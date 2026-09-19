package application

import (
	"context"
	"fmt"
	"io"
)

// SessionIDShown is how much of a session's id `mw seat context` prints: enough
// to tell one session from the next, on a line meant for a phone.
const SessionIDShown = 8

// Transcripts is the port a seat's live session is read through: the record the
// harness keeps of each session as it runs. It is read-only, and reading it
// costs no fuel.
type Transcripts interface {
	// LiveContext reports how much of the model's context window the newest
	// session run in dir has filled, as of its last assistant turn. A directory
	// with no transcript, or a transcript with no assistant turn yet, is an
	// error that says where it looked.
	LiveContext(ctx context.Context, dir string) (TranscriptContext, error)
}

// TranscriptContext is one session's live context.
type TranscriptContext struct {
	// SessionID is the session whose transcript this is.
	SessionID string
	// Tokens is the size of the context the last assistant turn was given:
	// fresh input, cache reads and cache writes together.
	Tokens int
}

// SeatContext says how full a seat's live session is against the limit at
// which it must hand off. It is the zero-token twin of a seat asking itself
// whether it has room left: it starts no session and writes nothing.
type SeatContext struct {
	Transcripts Transcripts

	// Dir is the working directory of the seat's session.
	Dir string

	// HandoffAt is the context size, in tokens, at which a session must hand
	// off. It must be at least 1: the default belongs to the configuration.
	HandoffAt int

	// Out is where the line is printed. A nil Out prints nothing.
	Out io.Writer
}

// SeatContextReport is what one reading found.
type SeatContextReport struct {
	Context   TranscriptContext
	HandoffAt int
}

// Handoff reports whether the session has reached the limit.
func (r SeatContextReport) Handoff() bool { return r.Context.Tokens >= r.HandoffAt }

// String is the report as `mw seat context` prints it: one line.
func (r SeatContextReport) String() string {
	verdict := "ok"
	if r.Handoff() {
		verdict = "handoff"
	}
	session := r.Context.SessionID
	if len(session) > SessionIDShown {
		session = session[:SessionIDShown]
	}
	return fmt.Sprintf("context=%d handoff_at=%d %s session=%s", r.Context.Tokens, r.HandoffAt, verdict, session)
}

// Run reads the seat's live context and prints the line. Being over the limit
// is a finding, not a failure: only a session that could not be read is an
// error.
func (s SeatContext) Run(ctx context.Context) (SeatContextReport, error) {
	switch {
	case s.Transcripts == nil:
		return SeatContextReport{}, fmt.Errorf("reading a seat's context needs the transcripts to read")
	case s.Dir == "":
		return SeatContextReport{}, fmt.Errorf("reading a seat's context needs the directory its session works in")
	case s.HandoffAt < 1:
		return SeatContextReport{}, fmt.Errorf("the handoff limit is %d tokens, so every session would be told to hand off at once: set it to 1 or more", s.HandoffAt)
	}

	live, err := s.Transcripts.LiveContext(ctx, s.Dir)
	if err != nil {
		return SeatContextReport{}, err
	}

	report := SeatContextReport{Context: live, HandoffAt: s.HandoffAt}
	if s.Out != nil {
		fmt.Fprintln(s.Out, report)
	}
	return report, nil
}
