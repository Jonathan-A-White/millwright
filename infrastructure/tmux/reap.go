package tmux

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

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
	printed, err := w.call(ctx, "capture-pane", "-p", "-e", "-t", window)
	if err != nil {
		return "", err
	}
	return classifyPane(string(printed)), nil
}

// classifyPane says what a screen of Claude Code is doing. A session that says
// how to interrupt it is working. One with a prompt mark on a line of its own
// is at an empty input line, and idle; so is one whose input line holds only
// dim text, which is Claude Code's suggested next prompt and not a draft
// (inputLineHoldsDraft). The screen is read with its escapes, capture-pane -e. One that shows a firstRunMarkers line
// is at Claude Code's own first-run screen, not yet a prompt at all. Anything
// else is not to be closed over: the mark has text after it, or there is no
// prompt on the screen at all — a question being asked, a menu, a session
// that has not started.
func classifyPane(screen string) application.PaneState {
	// Markers are matched on the screen's text, not on the escapes drawn
	// around it.
	text := stripEscapes(screen)
	if strings.Contains(text, workingMarker) {
		return application.PaneWorking
	}
	for _, line := range strings.Split(screen, "\n") {
		if isPrompt, holdsDraft := inputLineHoldsDraft(line); isPrompt && !holdsDraft {
			return application.PaneIdle
		}
	}
	for _, marker := range firstRunMarkers {
		if strings.Contains(text, marker) {
			return application.PaneFirstRun
		}
	}
	return application.PaneInput
}

// Type implements application.ReapTerminal: the text, typed literally into the
// window's pane, followed by the Enter key.
func (w *Windows) Type(ctx context.Context, window, text string) error {
	if err := windowID(window); err != nil {
		return err
	}
	if _, err := w.call(ctx, "send-keys", "-t", window, "-l", "--", text); err != nil {
		return err
	}
	_, err := w.call(ctx, "send-keys", "-t", window, "Enter")
	return err
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

// inputLineHoldsDraft reads one line of a capture-pane -e screen. isPrompt is
// whether the line, its escapes aside, starts with the prompt mark. holdsDraft
// is whether anything undimmed and not blank follows the mark: an empty input
// line is the mark and blank space (Claude Code's is a no-break space), and
// Claude Code's suggested next prompt is drawn after it as dim text, SGR 2,
// which is not a draft either. This is the one rule for "is the input line
// empty"; contrib/mail-notify's pane_idle is the same rule in shell.
func inputLineHoldsDraft(line string) (isPrompt, holdsDraft bool) {
	dim := false
	seenMark := false
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			n, params, isSGR := escapeAt(line[i:])
			if isSGR {
				dim = sgrDim(params, dim)
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		i += size
		switch {
		case !seenMark:
			if r != []rune(promptMark)[0] {
				return false, false
			}
			seenMark = true
		case !unicode.IsSpace(r) && !dim:
			return true, true
		}
	}
	return seenMark, false
}

// escapeAt reads the escape sequence at the start of s and says how long it is;
// for a CSI ending in m, an SGR, it gives the parameters as well.
func escapeAt(s string) (n int, params string, isSGR bool) {
	if len(s) < 2 {
		return len(s), "", false
	}
	switch s[1] {
	case '[':
		for n = 2; n < len(s); n++ {
			if s[n] >= 0x40 && s[n] <= 0x7e {
				return n + 1, s[2:n], s[n] == 'm'
			}
		}
		return len(s), "", false
	case ']': // an operating system command, to a bell or ESC \
		for n = 2; n < len(s); n++ {
			if s[n] == 0x07 {
				return n + 1, "", false
			}
			if s[n] == 0x1b && n+1 < len(s) && s[n+1] == '\\' {
				return n + 2, "", false
			}
		}
		return len(s), "", false
	}
	return 2, "", false
}

// sgrDim is whether text is dim after an SGR sequence with these parameters,
// given whether it was before. The colour codes 38 and 48 carry arguments that
// are not attributes of their own: 38;2;r;g;b holds a 2 that is no dim.
func sgrDim(params string, dim bool) bool {
	args := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	if len(args) == 0 {
		return false
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "0", "00", "22":
			dim = false
		case "2", "02":
			dim = true
		case "38", "48", "58":
			if i+1 < len(args) && args[i+1] == "5" {
				i += 2
			} else if i+1 < len(args) && args[i+1] == "2" {
				i += 4
			}
		}
	}
	return dim
}

// stripEscapes is the text of a screen without the escape sequences drawn in it.
func stripEscapes(screen string) string {
	var text strings.Builder
	for i := 0; i < len(screen); {
		if screen[i] == 0x1b {
			n, _, _ := escapeAt(screen[i:])
			i += n
			continue
		}
		text.WriteByte(screen[i])
		i++
	}
	return text.String()
}
