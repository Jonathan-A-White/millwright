package tmux

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// PaneEnv is what tmux sets in every process it starts to name the pane the
// process runs in, and so how a process tells which window is its own.
const PaneEnv = "TMUX_PANE"

// Windows is the terminal a reaper watches as well: unlike List, which is the
// one session seats are opened in, these look at every session on the server,
// because the window a seat is handed over from is wherever the person started
// it.
var _ application.ReapTerminal = (*Windows)(nil)

// windowFormat is what tmux is asked to print of a window: its id, the pid of
// its pane's process, and its name last, since a name is whatever a person
// typed and may hold the separator.
const windowFormat = "#{window_id}|#{pane_pid}|#{window_name}"

// The marks of Claude Code's screen a pane is read by. Its input line is a
// prompt mark and, when nothing is typed, a no-break space; while it is
// working it says how to interrupt it.
const (
	promptMark    = "❯"
	workingMarker = "esc to interrupt"
	noBreakSpace  = " "
)

// firstRunMarkers are lines Claude Code's own first-run screens show, before
// it has a prompt of its own at all: a theme choice and the login menu.
var firstRunMarkers = []string{
	"Welcome to Claude Code",
	"Choose the text style",
	"Select login method",
}

// OpenWindows implements application.ReapTerminal: every window of every
// session on the server, each dated by the process in its pane as List does.
func (w *Windows) OpenWindows(ctx context.Context) ([]application.ReapWindow, error) {
	printed, err := w.call(ctx, "list-windows", "-a", "-F", windowFormat)
	if err != nil {
		if noServer(err) {
			return nil, nil
		}
		return nil, err
	}
	return w.reapWindows(ctx, string(printed)), nil
}

// ThisWindow implements application.ReapTerminal: the window of the pane the
// calling process runs in, which tmux names in the process's environment.
func (w *Windows) ThisWindow(ctx context.Context) (application.ReapWindow, bool, error) {
	pane := strings.TrimSpace(os.Getenv(PaneEnv))
	if pane == "" {
		return application.ReapWindow{}, false, nil
	}
	printed, err := w.call(ctx, "display-message", "-p", "-t", pane, windowFormat)
	if err != nil {
		return application.ReapWindow{}, false, fmt.Errorf("asking tmux which window pane %s is in: %w", pane, err)
	}
	windows := w.reapWindows(ctx, string(printed))
	if len(windows) == 0 {
		return application.ReapWindow{}, false, nil
	}
	return windows[0], true, nil
}

// reapWindows reads what windowFormat printed, a window a line, and dates each
// window by its pane's process. A window nothing can date is left undated.
func (w *Windows) reapWindows(ctx context.Context, printed string) []application.ReapWindow {
	var (
		windows []application.ReapWindow
		pids    []string
		at      = map[string]int{} // pid -> the window it is the pane of
	)
	for _, line := range strings.Split(printed, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(fields) != 3 || fields[0] == "" {
			continue
		}
		windows = append(windows, application.ReapWindow{ID: fields[0], Name: fields[2]})
		if fields[1] != "" {
			pids = append(pids, fields[1])
			at[fields[1]] = len(windows) - 1
		}
	}
	for pid, started := range startTimes(ctx, pids) {
		windows[at[pid]].Opened = started
	}
	return windows
}

// windowID reports whether a window is named the way tmux names one for good,
// @ and a number, and says why not when it is not. tmux would take a name or a
// pattern for a target just as readily and act on whichever window matched, and
// a reaper is told which window to close, not to find one.
func windowID(window string) error {
	digits, isID := strings.CutPrefix(window, "@")
	if isID && digits != "" && strings.Trim(digits, "0123456789") == "" {
		return nil
	}
	return fmt.Errorf("%q is not a tmux window id: a window is named for closing by its id, like @3", window)
}

// PaneState implements application.ReapTerminal: what the window's screen
// says its session is doing. This is the one place a pane is read.
func (w *Windows) PaneState(ctx context.Context, window string) (application.PaneState, error) {
	if err := windowID(window); err != nil {
		return "", err
	}
	printed, err := w.call(ctx, "capture-pane", "-p", "-t", window)
	if err != nil {
		return "", err
	}
	return classifyPane(string(printed)), nil
}

// classifyPane says what a screen of Claude Code is doing. A session that says
// how to interrupt it is working. One with a prompt mark on a line of its own
// is at an empty input line, and idle. One that shows a firstRunMarkers line
// is at Claude Code's own first-run screen, not yet a prompt at all. Anything
// else is not to be closed over: the mark has text after it, or there is no
// prompt on the screen at all — a question being asked, a menu, a session
// that has not started.
func classifyPane(screen string) application.PaneState {
	// An empty input line is the mark and a no-break space, which a pattern for
	// blank space does not always take for one.
	screen = strings.ReplaceAll(screen, noBreakSpace, " ")
	if strings.Contains(screen, workingMarker) {
		return application.PaneWorking
	}
	for _, line := range strings.Split(screen, "\n") {
		if rest, isPrompt := strings.CutPrefix(line, promptMark); isPrompt && strings.TrimSpace(rest) == "" {
			return application.PaneIdle
		}
	}
	for _, marker := range firstRunMarkers {
		if strings.Contains(screen, marker) {
			return application.PaneFirstRun
		}
	}
	return application.PaneInput
}

// Close implements application.ReapTerminal: the window named by id, and what
// runs in it. A window that is already gone is not a failure.
func (w *Windows) Close(ctx context.Context, window string) error {
	if err := windowID(window); err != nil {
		return err
	}
	if _, err := w.call(ctx, "kill-window", "-t", window); err != nil {
		open, listErr := w.OpenWindows(ctx)
		if listErr == nil {
			for _, other := range open {
				if other.ID == window {
					return err
				}
			}
			return nil
		}
		return err
	}
	return nil
}

// noServer reports whether tmux failed for want of a server to ask: none is
// running, or there is no socket to reach one by. It is an answer — nothing is
// open — and not a failure.
func noServer(err error) bool {
	said := err.Error()
	return strings.Contains(said, "no server running") || strings.Contains(said, "error connecting to")
}
