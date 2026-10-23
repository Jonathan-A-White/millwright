package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	printed, err := w.call(ctx, "capture-pane", "-p", "-e", "-t", window)
	if err != nil {
		return "", err
	}
	return classifyPane(string(printed)), nil
}

// UsageLimit implements application.ReapTerminal: the harness's usage-limit
// notice and menu on the window's screen, read by application.PaneUsageLimit.
func (w *Windows) UsageLimit(ctx context.Context, window string) (string, error) {
	if err := windowID(window); err != nil {
		return "", err
	}
	printed, err := w.call(ctx, "capture-pane", "-p", "-e", "-t", window)
	if err != nil {
		return "", err
	}
	shown, _ := application.PaneUsageLimit(application.StripEscapes(string(printed)))
	return shown, nil
}

// classifyPane says what a screen of Claude Code is doing. A session that says
// how to interrupt it is working. One with a prompt mark on a line of its own
// is at an empty input line, and idle; so is one whose input line holds only
// dim text, which is Claude Code's suggested next prompt and not a draft
// (application.InputLineHoldsDraft). The screen is read with its escapes, capture-pane -e.
// One stopped at the harness's usage limit — its menu up, or its notice just
// above the input line (application.PaneUsageLimit) — is limited, not idle,
// whatever its input line holds. One that shows a firstRunMarkers line
// is at Claude Code's own first-run screen, not yet a prompt at all. One with
// text on its input line and nothing running is a draft (application.
// PaneHoldsOnlyDraft). Anything else is not to be closed over: there is no
// prompt on the screen at all — a question being asked, a menu, a session that
// has not started.
func classifyPane(screen string) application.PaneState {
	// Markers are matched on the screen's text, not on the escapes drawn
	// around it.
	text := application.StripEscapes(screen)
	if strings.Contains(text, application.PaneWorkingMarker) {
		return application.PaneWorking
	}
	if _, limited := application.PaneUsageLimit(text); limited {
		return application.PaneLimited
	}
	for _, line := range strings.Split(screen, "\n") {
		if isPrompt, holdsDraft := application.InputLineHoldsDraft(line); isPrompt && !holdsDraft {
			return application.PaneIdle
		}
	}
	for _, marker := range firstRunMarkers {
		if strings.Contains(text, marker) {
			return application.PaneFirstRun
		}
	}
	if application.PaneHoldsOnlyDraft(screen) {
		return application.PaneDraft
	}
	return application.PaneInput
}

// enterPause is how long Type waits between the text and the Enter key: Claude
// Code's input line reads the text first, and an Enter sent right behind it is
// not taken as a submit (contrib/mail-notify's type_line pauses the same).
var enterPause = 500 * time.Millisecond

// Type implements application.ReapTerminal: the text, typed literally into the
// window's pane, a short pause, then the Enter key.
func (w *Windows) Type(ctx context.Context, window, text string) error {
	if err := windowID(window); err != nil {
		return err
	}
	if _, err := w.call(ctx, "send-keys", "-t", window, "-l", "--", text); err != nil {
		return err
	}
	select {
	case <-time.After(enterPause):
	case <-ctx.Done():
		return ctx.Err()
	}
	return w.Enter(ctx, window)
}

// Enter implements application.ReapTerminal: the Enter key, pressed in the
// window's pane.
func (w *Windows) Enter(ctx context.Context, window string) error {
	if err := windowID(window); err != nil {
		return err
	}
	_, err := w.call(ctx, "send-keys", "-t", window, "Enter")
	return err
}

// InputLine implements application.ReapTerminal: the undimmed text after the
// last prompt mark on the window's screen, "" when the line is empty.
func (w *Windows) InputLine(ctx context.Context, window string) (string, error) {
	if err := windowID(window); err != nil {
		return "", err
	}
	printed, err := w.call(ctx, "capture-pane", "-p", "-e", "-t", window)
	if err != nil {
		return "", err
	}
	return application.InputDraft(string(printed)), nil
}

// NextRunning implements application.ReapTerminal: whether a process below the
// pids of the window's panes is a running `mw next`, found in one listing of
// every process.
func (w *Windows) NextRunning(ctx context.Context, window string) (string, bool, error) {
	if err := windowID(window); err != nil {
		return "", false, err
	}
	panes, err := w.call(ctx, "list-panes", "-t", window, "-F", "#{pane_pid}")
	if err != nil {
		if noServer(err) {
			return "", false, nil
		}
		return "", false, err
	}
	processes, err := exec.CommandContext(ctx, psProgram, "-e", "-o", "pid=,ppid=,args=").Output()
	if err != nil {
		return "", false, fmt.Errorf("listing processes: %w", err)
	}
	story, running := nextBelow(strings.Fields(string(panes)), string(processes))
	return story, running, nil
}

// nextBelow reads a listing of processes, a line each of pid, parent pid and
// the command line, and finds a `mw next` among those below the root pids: the
// story it names, "" when it names none, and whether there is one. A command
// is `mw next` when its program is named mw and its first word is next, so a
// shell whose script mentions mw next is not.
func nextBelow(roots []string, processes string) (story string, running bool) {
	type process struct {
		pid  string
		args []string
	}
	children := map[string][]process{}
	for _, line := range strings.Split(processes, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		children[fields[1]] = append(children[fields[1]], process{pid: fields[0], args: fields[2:]})
	}
	seen := map[string]bool{}
	for queue := roots; len(queue) > 0; queue = queue[1:] {
		parent := queue[0]
		if seen[parent] {
			continue
		}
		seen[parent] = true
		for _, child := range children[parent] {
			if filepath.Base(child.args[0]) == "mw" && len(child.args) > 1 && child.args[1] == "next" {
				for _, arg := range child.args[2:] {
					if !strings.HasPrefix(arg, "-") {
						return arg, true
					}
				}
				return "", true
			}
			queue = append(queue, child.pid)
		}
	}
	return "", false
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

// Rename implements application.SeatTakeTerminal: the window named by id is
// called name from now on. tmux stops renaming a window itself once it is
// named by hand, so the name stays.
func (w *Windows) Rename(ctx context.Context, window, name string) error {
	if err := windowID(window); err != nil {
		return err
	}
	_, err := w.call(ctx, "rename-window", "-t", window, name)
	return err
}

// noServer reports whether tmux failed for want of a server to ask: none is
// running, or there is no socket to reach one by. It is an answer — nothing is
// open — and not a failure.
func noServer(err error) bool {
	said := err.Error()
	return strings.Contains(said, "no server running") || strings.Contains(said, "error connecting to")
}
