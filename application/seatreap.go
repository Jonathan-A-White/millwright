package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// ReapLogFileName is the file a seat's reaper writes what it decides to:
// `.<seat>-reaper.log` in the vault, host-local and untracked, beside the
// acting file.
func ReapLogFileName(seat string) string {
	return "." + seat + "-reaper.log"
}

// The pace a reaper works at. It looks every half minute, because a session's
// window is not in a hurry to close and a look costs a process, and it gives
// up after three hours, because a window nothing has closed by then is one a
// person is using or one that will need closing by hand, and a watcher that
// waits for ever is a leak.
const (
	DefaultReapInterval = 30 * time.Second
	DefaultReapLimit    = 3 * time.Hour
)

// ReapWindow is one window of the terminal as a reaper sees it: named by the
// id the terminal gives it, which — unlike its name — is never reused for
// another window while the terminal runs.
type ReapWindow struct {
	// ID is what the terminal calls the window, like @3 in tmux.
	ID string
	// Name is the window's name, which a seat's acting file names it by.
	Name string
	// Opened is when the window was opened, zero when the terminal could not
	// say.
	Opened time.Time
}

// PaneState is what a window's pane is doing, as far as closing it goes.
type PaneState string

const (
	// PaneIdle is a session waiting at an empty input line with nothing running:
	// the one state a window may be closed in.
	PaneIdle PaneState = "idle"
	// PaneWorking is a session that is running something.
	PaneWorking PaneState = "working"
	// PaneInput is a pane that is not at an empty input line: there is text on
	// it, or it is not at a prompt at all. Someone may be in the middle of a
	// sentence, and a window is never closed over their words.
	PaneInput PaneState = "input"
)

// ReapTerminal is the port a reaper looks at windows through, and closes one
// through. It is the terminal every seat's window is in, all of it: the window
// a seat is being handed over from is in whichever session the person started
// it in, not only the one mw opens seats in.
//
// It is the only place a pane is read: what an idle session looks like is the
// terminal adapter's to know, and the use case only asks.
type ReapTerminal interface {
	// OpenWindows reports every window open on the terminal, in every session.
	// A terminal that is not running holds none, which is not an error.
	OpenWindows(ctx context.Context) ([]ReapWindow, error)

	// ThisWindow reports the window the calling process is running in, false
	// when it is running in none.
	ThisWindow(ctx context.Context) (ReapWindow, bool, error)

	// PaneState reports what the window's pane is doing.
	PaneState(ctx context.Context, window string) (PaneState, error)

	// Close closes the window and what runs in it. Closing a window that is
	// already gone is not a failure.
	Close(ctx context.Context, window string) error
}

// ReapLog is the port a reaper says what it did through: one line, appended to
// the seat's reaper log. The line arrives whole and dated; the log is only
// where it goes.
type ReapLog interface {
	// Note appends one line to the seat's reaper log.
	Note(ctx context.Context, seat, line string) error
}

// ReapMode says what a reaper waits for before it closes its window.
type ReapMode string

const (
	// ReapSuccessor waits for the seat's acting file to name someone else, in a
	// window that is open: a seat handed over.
	ReapSuccessor ReapMode = "successor"
	// ReapWhenIdle waits for a handoff written after the window was opened: a
	// session that is done with nobody to hand the seat to.
	ReapWhenIdle ReapMode = "when-idle"
)

// ReapArming is one reaper to start: which seat's, on which window, in which
// mode.
type ReapArming struct {
	Seat   string
	Window string
	Mode   ReapMode
}

// ReapArmer is the port a reaper is started through. The reaper it starts
// outlives whatever started it — it is the watch that closes a window after
// the session in it is gone — so it is a process of its own, detached.
type ReapArmer interface {
	// Arm starts the reaper and returns once it is running.
	Arm(ctx context.Context, arming ReapArming) error
}

// ReapOutcome is how a reaper's watch ended.
type ReapOutcome string

const (
	// ReapClosed: the window was closed.
	ReapClosed ReapOutcome = "closed"
	// ReapGone: the window was closed by someone else before the reaper got to
	// it.
	ReapGone ReapOutcome = "gone"
	// ReapGaveUp: the limit ran out and nothing was closed.
	ReapGaveUp ReapOutcome = "gave-up"
	// ReapFailed: the terminal would not close the window.
	ReapFailed ReapOutcome = "failed"
)

// SeatReap is a watcher that closes a finished session's window: zero tokens,
// started detached, and a window closes itself only in the sense that a
// session ends its own window — no session ever closes another's.
//
// In successor mode it closes the window once the seat's acting file names
// someone else than it did when the watch was armed, that someone's window is
// open, and the window's pane is idle. In idle mode, for a session with nobody
// to hand over to, it closes the window once a handoff has been written after
// the window was opened and the pane is idle. Idle is asked on two looks in a
// row, and a pane with anything on its input line is never closed, in either
// mode: a person may be typing.
//
// It gives up after Limit, closing nothing. Arming, closing and giving up each
// append one dated line to the seat's reaper log.
type SeatReap struct {
	Seats    SeatFiles
	Terminal ReapTerminal
	Log      ReapLog

	// Seat is the seat whose window is being watched, Host the host whose
	// handoffs count, and Window the window to close, by the id the terminal
	// gives it.
	Seat   string
	Host   string
	Window string

	// WhenIdle is idle mode; the default is successor mode.
	WhenIdle bool

	// Interval is how long between looks and Limit how long to keep looking;
	// zero is DefaultReapInterval and DefaultReapLimit.
	Interval time.Duration
	Limit    time.Duration

	// Now is the clock; nil is time.Now. Sleep waits out an interval, or until
	// ctx ends; nil sleeps for real.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	// Out is where each line is printed as it is logged. A nil Out prints
	// nothing.
	Out io.Writer
}

// ReapReport is how a watch ended.
type ReapReport struct {
	Seat    string
	Window  string
	Outcome ReapOutcome
	// Said is the line the watch ended with, as it was logged.
	Said string
}

// String is the report as `mw seat reap` prints it: the line the watch ended
// with.
func (r ReapReport) String() string { return r.Said }

// Run watches until the window is closed, is found gone, or the limit runs
// out, and reports which. A watch that gave up returns its report and an error
// saying so, so that the command exits non-zero.
//
// A look that cannot be made — the terminal or the vault failing to answer for
// a moment — counts as a look at a window that is not ready to close: closing
// is the one thing here that cannot be undone, so a doubt never closes it.
func (s SeatReap) Run(ctx context.Context) (ReapReport, error) {
	switch {
	case s.Seats == nil || s.Terminal == nil || s.Log == nil:
		return ReapReport{}, fmt.Errorf("a reaper needs the seat's files, a terminal to watch and a log to write")
	case s.Seat == "":
		return ReapReport{}, fmt.Errorf("which seat's window is to be reaped?")
	case !plainSeatName(s.Seat):
		return ReapReport{}, fmt.Errorf("%q is not a seat: a seat is named in letters, digits, dashes and underscores", s.Seat)
	case strings.TrimSpace(s.Window) == "":
		return ReapReport{}, fmt.Errorf("which window is to be reaped? name it by the id the terminal gives it")
	}
	interval, limit := s.Interval, s.Limit
	if interval <= 0 {
		interval = DefaultReapInterval
	}
	if limit <= 0 {
		limit = DefaultReapLimit
	}

	start, err := s.Seats.SeatStart(ctx, s.Seat, s.Host)
	if err != nil {
		return ReapReport{}, err
	}
	armed := s.now()
	deadline := armed.Add(limit)
	if _, err := s.say(ctx, s.armedLine()); err != nil {
		return ReapReport{}, err
	}

	// The acting file as it was when the watch was armed: the seat is handed
	// over when it says anything else.
	was := strings.TrimSpace(start.Acting)
	idle := 0
	for s.now().Before(deadline) {
		if err := s.wait(ctx, interval); err != nil {
			return ReapReport{}, err
		}

		window, there, err := s.find(ctx)
		switch {
		case err != nil:
			idle = 0
			continue
		case !there:
			return s.end(ctx, ReapGone, "the window is already gone; nothing to do")
		}

		held, err := s.held(ctx, window, was, armed)
		if err != nil || !held {
			idle = 0
			continue
		}
		if state, err := s.Terminal.PaneState(ctx, s.Window); err != nil || state != PaneIdle {
			idle = 0
			continue
		}
		if idle++; idle < 2 {
			continue
		}

		if err := s.Terminal.Close(ctx, s.Window); err != nil {
			report, sayErr := s.end(ctx, ReapFailed, fmt.Sprintf("closing the window failed: %v", err))
			if sayErr != nil {
				return report, sayErr
			}
			return report, fmt.Errorf("closing the window %s: %w", s.Window, err)
		}
		return s.end(ctx, ReapClosed, s.closedLine())
	}

	report, err := s.end(ctx, ReapGaveUp, fmt.Sprintf("gave up after %s without closing anything", limit))
	if err != nil {
		return report, err
	}
	return report, fmt.Errorf("%s", report.Said)
}

// held reports whether what the watch waits for has happened: in successor
// mode, the acting file names someone else whose window is open; in idle mode,
// a handoff was written after the window was opened. A window nothing can date
// counts as opened when the watch was armed, so that only a handoff that is
// surely newer than it can close it.
func (s SeatReap) held(ctx context.Context, window ReapWindow, was string, armed time.Time) (bool, error) {
	start, err := s.Seats.SeatStart(ctx, s.Seat, s.Host)
	if err != nil {
		return false, err
	}

	if s.WhenIdle {
		opened := window.Opened
		if opened.IsZero() {
			opened = armed
		}
		return handedOffSince(start.Handoffs, opened), nil
	}

	if strings.TrimSpace(start.Acting) == was {
		return false, nil
	}
	open, err := s.Terminal.OpenWindows(ctx)
	if err != nil {
		return false, err
	}
	for _, other := range open {
		if other.ID != window.ID && other.Name != "" && other.Name != window.Name && strings.Contains(start.Acting, other.Name) {
			return true, nil
		}
	}
	return false, nil
}

// find is the window being reaped, among those open.
func (s SeatReap) find(ctx context.Context) (ReapWindow, bool, error) {
	open, err := s.Terminal.OpenWindows(ctx)
	if err != nil {
		return ReapWindow{}, false, err
	}
	for _, window := range open {
		if window.ID == s.Window {
			return window, true, nil
		}
	}
	return ReapWindow{}, false, nil
}

// wait sleeps out one interval, ending early with the context's error when the
// context ends first.
func (s SeatReap) wait(ctx context.Context, interval time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, interval)
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// say logs one line, dated and named for the window, and prints it. It returns
// the line as it was logged.
func (s SeatReap) say(ctx context.Context, text string) (string, error) {
	line := fmt.Sprintf("%s reap %s: %s", s.now().UTC().Format(time.RFC3339), s.Window, text)
	if err := s.Log.Note(ctx, s.Seat, line); err != nil {
		return line, fmt.Errorf("the reaper could not log %q: %w", line, err)
	}
	if s.Out != nil {
		fmt.Fprintln(s.Out, line)
	}
	return line, nil
}

// end logs the line a watch ends with and reports it.
func (s SeatReap) end(ctx context.Context, outcome ReapOutcome, text string) (ReapReport, error) {
	line, err := s.say(ctx, text)
	return ReapReport{Seat: s.Seat, Window: s.Window, Outcome: outcome, Said: line}, err
}

func (s SeatReap) armedLine() string {
	if s.WhenIdle {
		return "armed in idle mode; waiting for a handoff newer than the window and an idle pane"
	}
	return fmt.Sprintf("armed; waiting for %s to name a successor whose window is open, and an idle pane", ActingFileName(s.Seat))
}

func (s SeatReap) closedLine() string {
	if s.WhenIdle {
		return "closed: the seat has written a handoff since this window opened, and the pane was idle"
	}
	return "closed: the seat is held by someone else, and the pane was idle"
}

func (s SeatReap) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}
