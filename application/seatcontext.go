package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
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

// TranscriptReplies is the same record read for what the harness answered: the
// Mayor's session is stuck when every reply it got lately was an API error.
// It is read-only and costs no fuel.
type TranscriptReplies interface {
	// Replies reports the harness's replies in the newest session run in dir,
	// oldest first, that were recorded at or after since. A directory with no
	// transcript has none, which is not an error.
	Replies(ctx context.Context, dir string, since time.Time) ([]HarnessReply, error)
}

// HarnessReply is one reply the harness recorded in a session: what the model
// answered, or the "API Error" the harness wrote down in its place.
type HarnessReply struct {
	At       time.Time
	APIError bool
	// Text is the reply's first line of text, cut short.
	Text string
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

// TitledTranscripts is the transcripts read by the name a session was started
// under. A seat's window and its session carry the same name (`mw seat up`
// names both), so a window says which transcript is its own even when several
// seats work in one directory.
type TitledTranscripts interface {
	// TitledContext is Transcripts.LiveContext for the newest session run in
	// dir that was named title. No such session is an error that says where it
	// looked.
	TitledContext(ctx context.Context, dir, title string) (TranscriptContext, error)
}

// ContextMarks keeps, for each window told to hand off, the context it was
// told at: what lets a restarted follower know it has already spoken.
type ContextMarks interface {
	Load(ctx context.Context) (map[string]int, error)
	Save(ctx context.Context, marks map[string]int) error
}

// ContextWatcher is what EventFollow calls each pass to watch the seats'
// context: SeatContextWatch.
type ContextWatcher interface {
	Watch(ctx context.Context) error
}

const (
	// SeatContextRetell is how much further a session must grow, past the
	// context it was last told at, to be told again. It is also how far past
	// its limit mw doctor calls a seat's window overdue.
	SeatContextRetell = 20000

	// DefaultSeatContextEvery is how often SeatContextWatch reads the
	// transcripts.
	DefaultSeatContextEvery = time.Minute
)

// SeatContextLine is the line typed into a window whose session is past its
// limit.
func SeatContextLine(tokens, handoffAt int) string {
	return fmt.Sprintf("Context %d past %d: write your handoff now and stop.", tokens, handoffAt)
}

// SeatWindowContext is one open seat window and how full its session is.
type SeatWindowContext struct {
	Seat      string
	Window    ReapWindow
	Tokens    int
	HandoffAt int
}

// Past reports whether the session has reached its limit.
func (r SeatWindowContext) Past() bool { return r.Tokens >= r.HandoffAt }

// Over reports whether the session is margin tokens or more past its limit.
func (r SeatWindowContext) Over(margin int) bool { return r.Tokens >= r.HandoffAt+margin }

// SeatContextWatch is the one watcher of every seat's context. Limits says
// which seats are watched and at what size each must hand off; a seat is
// watched by being listed there, and nothing in the watcher names a seat. Each
// Watch, at most once an Every, reads the transcript of every open window named
// <seat>-* of a listed seat, and types SeatContextLine into one that has reached
// its limit, when its pane is idle: once for the crossing, and again only
// after the session has grown SeatContextRetell further. A window under its
// limit again (the session was compacted) is told afresh on its next crossing.
// What was told is kept in Marks, so a restart does not tell it twice.
//
// It starts no session and spends no fuel. A window whose transcript cannot be
// read is said on Err, once while it repeats, and left alone.
type SeatContextWatch struct {
	// Limits is the context size, in tokens, at which each listed seat's
	// session must hand off. A seat at 0 or less is not watched.
	Limits      func() (map[string]int, error)
	Dir         string
	Transcripts TitledTranscripts
	Terminal    ReapTerminal
	Marks       ContextMarks

	// Every is the wait between two readings; zero means DefaultSeatContextEvery.
	Every time.Duration
	// Settle is how long after typing the input line is read, to see that
	// Enter took it.
	Settle time.Duration
	Now    func() time.Time
	Err    io.Writer

	mu   sync.Mutex
	last time.Time
	said map[string]string
}

// Watch reads the seats' context and tells the sessions past their limit.
func (w *SeatContextWatch) Watch(ctx context.Context) error {
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	every := w.Every
	if every <= 0 {
		every = DefaultSeatContextEvery
	}
	w.mu.Lock()
	if !w.last.IsZero() && now().Sub(w.last) < every {
		w.mu.Unlock()
		return nil
	}
	w.last = now()
	w.mu.Unlock()

	if w.Marks == nil {
		return fmt.Errorf("telling the seats of their context needs somewhere to keep what was told")
	}
	readings, err := w.Readings(ctx)
	if err != nil {
		return err
	}
	marks, err := w.Marks.Load(ctx)
	if err != nil {
		return fmt.Errorf("loading the context marks: %w", err)
	}
	if marks == nil {
		marks = map[string]int{}
	}
	next := map[string]int{}
	for _, r := range readings {
		told, has := marks[r.Window.Name]
		switch {
		case !r.Past():
			// Under the limit: the next crossing is a new one.
		case has && r.Tokens < told+SeatContextRetell:
			next[r.Window.Name] = told
		default:
			sent, err := w.tell(ctx, r)
			if err != nil {
				w.say("tell:"+r.Window.Name, err)
			} else {
				w.quiet("tell:" + r.Window.Name)
			}
			switch {
			case sent:
				next[r.Window.Name] = r.Tokens
			case has:
				next[r.Window.Name] = told
			}
		}
	}
	if !sameMarks(marks, next) {
		return w.Marks.Save(ctx, next)
	}
	return nil
}

// Readings is how full each open window of a listed seat is. A window whose
// transcript cannot be read is said and left out.
func (w *SeatContextWatch) Readings(ctx context.Context) ([]SeatWindowContext, error) {
	if w.Limits == nil || w.Transcripts == nil || w.Terminal == nil {
		return nil, fmt.Errorf("reading the seats' context needs their limits, the transcripts and a terminal")
	}
	limits, err := w.Limits()
	if err != nil {
		return nil, fmt.Errorf("reading the seats' handoff limits: %w", err)
	}
	windows, err := w.Terminal.OpenWindows(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing the windows: %w", err)
	}
	seats := make([]string, 0, len(limits))
	for seat, limit := range limits {
		if limit > 0 {
			seats = append(seats, seat)
		}
	}
	sort.Strings(seats)
	var out []SeatWindowContext
	for _, seat := range seats {
		for _, window := range windows {
			if !strings.HasPrefix(window.Name, seat+"-") {
				continue
			}
			live, err := w.Transcripts.TitledContext(ctx, w.Dir, window.Name)
			if err != nil {
				w.say("read:"+window.Name, err)
				continue
			}
			w.quiet("read:" + window.Name)
			out = append(out, SeatWindowContext{Seat: seat, Window: window, Tokens: live.Tokens, HandoffAt: limits[seat]})
		}
	}
	return out, nil
}

// tell types the line into the window when its pane is idle, and reports
// whether it did. A pane at work is left alone and is not a failure: it is
// told on a later reading.
func (w *SeatContextWatch) tell(ctx context.Context, r SeatWindowContext) (bool, error) {
	state, err := w.Terminal.PaneState(ctx, r.Window.ID)
	if err != nil {
		return false, fmt.Errorf("reading the pane of %s: %w", r.Window.Name, err)
	}
	if state != PaneIdle {
		return false, nil
	}
	line := SeatContextLine(r.Tokens, r.HandoffAt)
	outcome, err := TypeConfirmed(ctx, w.Terminal, r.Window, line, w.Settle)
	if err != nil {
		return false, fmt.Errorf("typing the line into %s: %w", r.Window.Name, err)
	}
	if said := outcome.Said(r.Window.Name, line); said != "" && w.Err != nil {
		w.mu.Lock()
		fmt.Fprintf(w.Err, "mw events follow: %s\n", said)
		w.mu.Unlock()
	}
	return true, nil
}

func sameMarks(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if got, ok := b[k]; !ok || got != v {
			return false
		}
	}
	return true
}

func (w *SeatContextWatch) say(what string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.said == nil {
		w.said = map[string]string{}
	}
	if w.Err != nil && w.said[what] != err.Error() {
		fmt.Fprintf(w.Err, "mw events follow: watching the context of %s: %v\n", strings.TrimPrefix(strings.TrimPrefix(what, "read:"), "tell:"), err)
	}
	w.said[what] = err.Error()
}

func (w *SeatContextWatch) quiet(what string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.said, what)
}
