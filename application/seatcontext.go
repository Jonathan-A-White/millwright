package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
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

// TranscriptTurns is the same record read for how many turns the newest session
// run in dir has taken so far, counted as the harness counts its num_turns
// (mw-1ljpo9.6): mw status names a running session past TurnsNamedPast. A
// directory with no transcript has taken none, which is not an error.
type TranscriptTurns interface {
	Turns(ctx context.Context, dir string) (int, error)
}

// TranscriptModes is the same record read for the permission mode the newest
// session run in dir ran in (mw-gq6.545). A session launched in auto whose
// transcript shows another mode had what it asked for outside its allow rules
// denied, whatever else its refusal says. A directory with no transcript, or
// one that records no mode, has the empty mode, which is not an error.
type TranscriptModes interface {
	PermissionMode(ctx context.Context, dir string) (string, error)
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

	// Mail, Events, Host and Spring make the watch act on a session that was
	// told to hand off and stopped without it, with mail unread (mw-gq6.975):
	// nudging it again only asks a seat that has refused to work. Such a
	// window, its pane idle, is closed, a seat_past_line event is written for
	// it, and Spring starts a successor for the seat. Without Mail the watch
	// only tells. Springs names the seats that Spring can bring up again: the
	// window of any other seat is left open.
	Mail    Mailbox
	Events  EventLog
	Host    string
	Spring  func(ctx context.Context, seat, reason string) error
	Springs []string

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
		case has && w.retire(ctx, r):
			// Stopped past its line with mail unread: its window is closed and
			// a successor sprung, so there is no mark to keep.
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

// retire closes the window of a session that was told to hand off, is past its
// line still, sits idle and leaves mail unread, tells the log, and springs a
// successor for the seat. It reports whether it closed the window. A seat that
// cannot be sprung, a box that cannot be counted or a pane at work is left as
// it is.
func (w *SeatContextWatch) retire(ctx context.Context, r SeatWindowContext) bool {
	if w.Mail == nil || w.Spring == nil || !containsSeat(w.Springs, r.Seat) {
		return false
	}
	unread, stopped, err := stoppedPastLine(ctx, w.Terminal, w.Mail, SeatMailboxes(r.Seat, w.Host), r.Window)
	if err != nil {
		w.say("retire:"+r.Window.Name, err)
		return false
	}
	w.quiet("retire:" + r.Window.Name)
	if !stopped {
		return false
	}
	if err := w.Terminal.Close(ctx, r.Window.ID); err != nil {
		w.say("retire:"+r.Window.Name, fmt.Errorf("closing the window of a session stopped past its line: %w", err))
		return false
	}
	past := events.SeatPastLine{Seat: r.Seat, Window: r.Window.Name, Tokens: r.Tokens, HandoffAt: r.HandoffAt, Unread: unread}
	if err := TellSeatPastLine(ctx, w.Events, w.Now, w.Host, past); err != nil {
		w.say("retire:"+r.Window.Name, err)
	}
	if err := w.Spring(ctx, r.Seat, PastLineReason(past)); err != nil {
		w.say("retire:"+r.Window.Name, fmt.Errorf("springing a successor for %s: %w", r.Seat, err))
	}
	return true
}

func containsSeat(seats []string, seat string) bool {
	for _, s := range seats {
		if s == seat {
			return true
		}
	}
	return false
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
		fmt.Fprintf(w.Err, "mw events follow: watching the context of %s: %v\n", strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(what, "read:"), "tell:"), "retire:"), err)
	}
	w.said[what] = err.Error()
}

func (w *SeatContextWatch) quiet(what string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.said, what)
}

// SeatLine is the context size at which one seat's session must hand off,
// read against the transcript of a window: what lets a use case that is not
// the context watch know a session is past its line. A zero SeatLine reads no
// window as past it.
type SeatLine struct {
	Transcripts TitledTranscripts
	Dir         string
	HandoffAt   int
}

// PastBy reports the context of the session in window and whether it has
// reached the line. A transcript that cannot be read is not past it.
func (l SeatLine) PastBy(ctx context.Context, window ReapWindow) (tokens int, past bool) {
	if l.Transcripts == nil || l.HandoffAt < 1 || window.Name == "" {
		return 0, false
	}
	live, err := l.Transcripts.TitledContext(ctx, l.Dir, window.Name)
	if err != nil {
		return 0, false
	}
	return live.Tokens, live.Tokens >= l.HandoffAt
}

// SeatMailboxes is every mailbox a seat's session reads on host: the
// Millhand's host-qualified box, its plain box and the host's own, any other
// seat's own.
func SeatMailboxes(seat, host string) []string {
	if seat == MillhandSeat {
		return []string{SeatIdentity(MillhandSeat, host), MillhandSeat, host}
	}
	return []string{seat}
}

// unreadIn is how many distinct unread mails wait in the boxes. The mail is
// not read.
func unreadIn(ctx context.Context, mail Mailbox, boxes []string) (int, error) {
	seen := map[string]bool{}
	for _, box := range boxes {
		inbox, err := mail.Inbox(ctx, box)
		if err != nil {
			return 0, fmt.Errorf("counting the unread mail for %s: %w", box, err)
		}
		for _, message := range inbox {
			seen[message.ID] = true
		}
	}
	return len(seen), nil
}

// stoppedPastLine reports whether the session in window, known to be past its
// line, has stopped with mail unread: its pane is idle at an empty input line,
// no background agent runs in it, no `mw next` runs in it, and its boxes hold
// unread mail, which it counts. A pane or a look that cannot be made is a
// session that has not stopped, and closing is the one thing that cannot be
// undone.
func stoppedPastLine(ctx context.Context, term ReapTerminal, mail Mailbox, boxes []string, window ReapWindow) (unread int, stopped bool, err error) {
	state, perr := term.PaneState(ctx, window.ID)
	if perr != nil || state != PaneIdle {
		return 0, false, nil
	}
	if agents, aerr := term.BackgroundAgents(ctx, window.ID); aerr != nil || agents != "" {
		return 0, false, nil
	}
	if _, running, nerr := term.NextRunning(ctx, window.ID); nerr != nil || running {
		return 0, false, nil
	}
	unread, err = unreadIn(ctx, mail, boxes)
	if err != nil || unread == 0 {
		return 0, false, err
	}
	return unread, true, nil
}

// TellSeatPastLine writes the seat_past_line event for a window closed over a
// session stopped past its line. A nil log writes nothing.
func TellSeatPastLine(ctx context.Context, log EventLog, now func() time.Time, host string, past events.SeatPastLine) error {
	if log == nil {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	actor := past.Seat
	if host != "" {
		actor += "@" + host
	}
	if _, err := (EventEmit{Log: log, Now: now, Event: events.Event{Kind: events.KindSeatPastLine, Actor: actor, Detail: past.Detail()}}).Run(ctx); err != nil {
		return fmt.Errorf("telling the event log %s stopped past its context line: %w", past.Seat, err)
	}
	return nil
}

// PastLineReason is why a successor is sprung for a seat whose session stopped
// past its line.
func PastLineReason(past events.SeatPastLine) string {
	return fmt.Sprintf("the session before you stopped at context %d, past its line of %d, with %d unread mail(s) for the seat: read your mail", past.Tokens, past.HandoffAt, past.Unread)
}
