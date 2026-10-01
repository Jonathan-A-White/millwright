package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// TalkLogFileName is the file `mw talk model` writes what it did to:
// `.<seat>-talk.log` in the vault, host-local and untracked, beside the acting
// file it reads.
func TalkLogFileName(seat string) string {
	return "." + seat + "-talk.log"
}

// The pace `mw talk model` works at. It looks every two seconds, because the
// model is wanted from the Mayor's next turn and a turn can follow the last
// within seconds; it gives up after ten minutes, because a window not idle at
// an empty input line in that long is one a person is typing in, and the
// switch is better left to them than made late.
const (
	DefaultTalkModelInterval = 2 * time.Second
	DefaultTalkModelLimit    = 10 * time.Minute
)

// TalkModels are the models `mw talk model` switches the Mayor between.
var TalkModels = []domain.Model{domain.ModelOpus, domain.ModelSonnet, domain.ModelFable}

// TalkLog is the port `mw talk model` says what it did through: one line,
// appended to the seat's talk log. The line arrives whole and dated; the log is
// only where it goes.
type TalkLog interface {
	// NoteTalk appends one line to the seat's talk log.
	NoteTalk(ctx context.Context, seat, line string) error
}

// TalkModelOutcome is how a watch to switch the model ended.
type TalkModelOutcome string

const (
	// TalkModelTyped: /model and the model were typed, and Enter.
	TalkModelTyped TalkModelOutcome = "typed"
	// TalkModelGaveUp: the limit ran out and nothing was typed.
	TalkModelGaveUp TalkModelOutcome = "gave-up"
	// TalkModelFailed: the terminal would not take the keys.
	TalkModelFailed TalkModelOutcome = "failed"
)

// TalkModel switches the model a seat's live session runs on, by typing
// `/model <model>` and Enter into the window its acting file names, the way a
// person at the keyboard would. It is zero-token and meant to run detached: the
// session asking for the switch is mid-turn, and the switch can only be typed
// once that turn is over.
//
// It types only when the window's pane is idle at an empty input line on two
// looks in a row, and never over anything typed on that line: one wrong
// keystroke there would corrupt what someone was writing. The window is found
// again on every look, so a seat handed over mid-watch is followed to its new
// window, and the count of idle looks starts again there.
//
// It gives up after Limit, typing nothing. Arming, typing and giving up each
// append one dated line to the seat's talk log.
type TalkModel struct {
	Seats    SeatFiles
	Terminal ReapTerminal
	Log      TalkLog

	// Seat is the seat whose session is switched, and Host the host whose
	// windows it is looked for on. Model is what it is switched to.
	Seat  string
	Host  string
	Model domain.Model

	// Interval is how long between looks and Limit how long to keep looking;
	// zero is DefaultTalkModelInterval and DefaultTalkModelLimit.
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

// TalkModelReport is how a watch to switch the model ended.
type TalkModelReport struct {
	Seat    string
	Model   domain.Model
	Outcome TalkModelOutcome
	// Window is the window typed into, by id, empty when nothing was typed.
	Window string
	// Said is the line the watch ended with, as it was logged.
	Said string
}

// String is the report as `mw talk model` prints it: the line the watch ended
// with.
func (r TalkModelReport) String() string { return r.Said }

// CheckTalkModel says whether a model is one `mw talk model` switches to, and
// which ones it does when it is not.
func CheckTalkModel(model domain.Model) error {
	for _, known := range TalkModels {
		if model == known {
			return nil
		}
	}
	return fmt.Errorf("%q is not a model to switch to: say opus, sonnet or fable", model)
}

// Run watches until the keys are typed or the limit runs out, and reports
// which. A watch that gave up, or whose keys the terminal would not take,
// returns its report and an error saying so, so that the command exits
// non-zero.
//
// A look that cannot be made — the vault or the terminal failing to answer for
// a moment — counts as a look at a window that is not idle: typing is the one
// thing here that can spoil someone's words, so a doubt never types.
func (t TalkModel) Run(ctx context.Context) (TalkModelReport, error) {
	switch {
	case t.Seats == nil || t.Terminal == nil || t.Log == nil:
		return TalkModelReport{}, fmt.Errorf("switching a model needs the seat's files, a terminal to type into and a log to write")
	case t.Seat == "":
		return TalkModelReport{}, fmt.Errorf("whose model is to be switched?")
	case !plainSeatName(t.Seat):
		return TalkModelReport{}, fmt.Errorf("%q is not a seat: a seat is named in letters, digits, dashes and underscores", t.Seat)
	}
	if err := CheckTalkModel(t.Model); err != nil {
		return TalkModelReport{}, err
	}
	interval, limit := t.Interval, t.Limit
	if interval <= 0 {
		interval = DefaultTalkModelInterval
	}
	if limit <= 0 {
		limit = DefaultTalkModelLimit
	}
	text := "/model " + string(t.Model)

	deadline := t.now().Add(limit)
	if _, err := t.say(ctx, fmt.Sprintf("armed; waiting for the %s's window to be idle at an empty input line to type %s", t.Seat, text)); err != nil {
		return TalkModelReport{}, err
	}

	last := ActingFileName(t.Seat) + " was not read"
	window := ""
	idle := 0
	for t.now().Before(deadline) {
		if err := t.wait(ctx, interval); err != nil {
			return TalkModelReport{}, err
		}

		found, there, err := t.find(ctx)
		switch {
		case err != nil:
			last, idle = "the window could not be looked for: "+oneLine(err.Error()), 0
			continue
		case !there:
			last, idle = ActingFileName(t.Seat)+" named no window that is open", 0
			continue
		}
		if found.ID != window {
			window, idle = found.ID, 0
		}

		state, err := t.Terminal.PaneState(ctx, found.ID)
		if err != nil {
			last, idle = fmt.Sprintf("the window %s could not be read: %s", windowNamed(found), oneLine(err.Error())), 0
			continue
		}
		if state != PaneIdle {
			last, idle = fmt.Sprintf("the window %s was last seen %s", windowNamed(found), seenAs(state)), 0
			continue
		}
		if idle++; idle < 2 {
			continue
		}

		if err := t.Terminal.Type(ctx, found.ID, text); err != nil {
			report, sayErr := t.end(ctx, TalkModelFailed, "", fmt.Sprintf("typing %s into %s failed: %s", text, windowNamed(found), oneLine(err.Error())))
			if sayErr != nil {
				return report, sayErr
			}
			return report, fmt.Errorf("typing %s into the window %s: %w", text, found.ID, err)
		}
		return t.end(ctx, TalkModelTyped, found.ID, fmt.Sprintf("typed %s into %s", text, windowNamed(found)))
	}

	report, err := t.end(ctx, TalkModelGaveUp, "", fmt.Sprintf("gave up after %s, typing nothing: %s", limit, last))
	if err != nil {
		return report, err
	}
	return report, fmt.Errorf("%s", report.Said)
}

// find is the window the seat's acting file names, among those open.
func (t TalkModel) find(ctx context.Context) (ReapWindow, bool, error) {
	start, err := t.Seats.SeatStart(ctx, t.Seat, t.Host)
	if err != nil {
		return ReapWindow{}, false, err
	}
	open, err := t.Terminal.OpenWindows(ctx)
	if err != nil {
		return ReapWindow{}, false, err
	}
	names := make([]string, len(open))
	for i, window := range open {
		names[i] = window.Name
	}
	name, ok := MatchActingName(start.Acting, names)
	if !ok {
		return ReapWindow{}, false, nil
	}
	for _, window := range open {
		if window.Name == name {
			return window, true, nil
		}
	}
	return ReapWindow{}, false, nil
}

// windowNamed is how a window is named in the talk log: by its name, and its id.
func windowNamed(window ReapWindow) string {
	return fmt.Sprintf("%s (%s)", window.Name, window.ID)
}

// seenAs is what a pane in a state other than idle was doing, in words.
func seenAs(state PaneState) string {
	switch state {
	case PaneWorking:
		return "working"
	case PaneFirstRun:
		return "at Claude Code's first-run screen"
	default:
		return "not at an empty input line"
	}
}

// wait sleeps out one interval, ending early with the context's error when the
// context ends first.
func (t TalkModel) wait(ctx context.Context, interval time.Duration) error {
	if t.Sleep != nil {
		return t.Sleep(ctx, interval)
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

// say logs one line, dated and named for the model, and prints it. It returns
// the line as it was logged.
func (t TalkModel) say(ctx context.Context, text string) (string, error) {
	line := fmt.Sprintf("%s talk model %s: %s", t.now().UTC().Format(time.RFC3339), t.Model, strings.TrimSpace(text))
	if err := t.Log.NoteTalk(ctx, t.Seat, line); err != nil {
		return line, fmt.Errorf("mw talk model could not log %q: %w", line, err)
	}
	if t.Out != nil {
		fmt.Fprintln(t.Out, line)
	}
	return line, nil
}

// end logs the line a watch ends with and reports it.
func (t TalkModel) end(ctx context.Context, outcome TalkModelOutcome, window, text string) (TalkModelReport, error) {
	line, err := t.say(ctx, text)
	return TalkModelReport{Seat: t.Seat, Model: t.Model, Outcome: outcome, Window: window, Said: line}, err
}

func (t TalkModel) now() time.Time {
	if t.Now == nil {
		return time.Now()
	}
	return t.Now()
}
