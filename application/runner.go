package application

import (
	"context"
	"fmt"
	"strings"
)

// SessionSpec is everything needed to start one runner session: what to call
// it, where to run, what environment to run with, and the command to run.
//
// Name is the session's name on the runner, not a story id: derive it with
// SessionName so that the session can be pointed at and attached to later.
// Command is an argv — the program and its arguments, not a line for a shell
// to read. Dir and Env may be empty, and then the session runs where and as
// the factory itself does.
type SessionSpec struct {
	Name    string
	Dir     string
	Env     map[string]string
	Command []string
}

// Validate reports the first reason a spec could not be started.
func (s SessionSpec) Validate() error {
	switch {
	case s.Name == "":
		return fmt.Errorf("a session needs a name")
	case s.Name != SessionName(s.Name):
		return fmt.Errorf("a session cannot be named %q: use SessionName, which makes it %q", s.Name, SessionName(s.Name))
	case len(s.Command) == 0:
		return fmt.Errorf("session %q needs a command to run", s.Name)
	}
	return nil
}

// SessionName is the runner session a story is worked in: the story's id with
// every character a runner might read as punctuation replaced by an underscore.
// Story ids carry dots (mw-gq6.4), and a dot or a colon is how tmux separates a
// session from a window from a pane, so the id itself cannot be the name. What
// comes out is its own SessionName, and a person can attach to it by it.
func SessionName(storyID string) string {
	var name strings.Builder
	for _, r := range storyID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			name.WriteRune(r)
		default:
			name.WriteRune('_')
		}
	}
	return name.String()
}

// SessionState is what the command in a runner session is doing.
type SessionState string

// The states a runner session can be found in.
const (
	// StateRunning: the command is still going.
	StateRunning SessionState = "running"
	// StateExited: the command has finished, and its exit status is known.
	StateExited SessionState = "exited"
	// StateGone: there is no session of that name — never started, or closed.
	StateGone SessionState = "gone"
)

// SessionStatus is what a Runner knows about one session right now. ExitCode is
// the command's exit status and means something only once it has exited.
type SessionStatus struct {
	Name     string
	State    SessionState
	ExitCode int
}

// Running reports whether the session's command is still going.
func (s SessionStatus) Running() bool { return s.State == StateRunning }

// Finished reports whether the session's command has exited and left a status
// behind. A session that was closed, or never started, has not finished: it is
// gone, and there is nothing left to read from it.
func (s SessionStatus) Finished() bool { return s.State == StateExited }

// Runner is the port the factory runs its sessions through: it starts a command
// in a named session with a terminal attached, types into it, reads what it has
// printed, and reports whether it is still going and how it ended.
//
// Sessions are named, not handed back as objects, so that a later `mw` can pick
// up a session an earlier one started, and a person can attach to one by name.
// Nothing here is tmux's: an adapter may back a session with anything that
// keeps a command running with a terminal attached to it.
//
// Every method takes the session by name, and a name that is not there is not
// an error for Status or Close — it is StateGone and nothing to close.
type Runner interface {
	// Start runs spec's command in a new session. It fails if the spec could
	// not be started or a session of that name is already there.
	Start(ctx context.Context, spec SessionSpec) error

	// Send types input into the session, as at its keyboard: the characters as
	// they are, a newline as the Enter key. A line a command is waiting for
	// therefore ends with "\n".
	Send(ctx context.Context, name, input string) error

	// Output is what the session has printed lately: at most lines lines, the
	// most recent last, still readable after the command has exited. lines must
	// be at least 1.
	Output(ctx context.Context, name string, lines int) (string, error)

	// Status reports whether the session's command is still running and, once
	// it is not, the status it exited with.
	Status(ctx context.Context, name string) (SessionStatus, error)

	// Wait blocks until the session's command is no longer running, and reports
	// the status it ended with. It gives up when ctx does, returning ctx's
	// error and the last status it saw.
	Wait(ctx context.Context, name string) (SessionStatus, error)

	// Close ends the session and everything running in it. Closing a session
	// that is already gone is harmless. Nothing of it can be read afterwards.
	Close(ctx context.Context, name string) error
}

// RecentLines is the tail of what a session printed: at most lines lines, with
// the blank padding a terminal leaves below the output trimmed off. Runner
// adapters share it, so that Output means the same thing behind every backend.
func RecentLines(output string, lines int) string {
	printed := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	for len(printed) > 0 && strings.TrimSpace(printed[len(printed)-1]) == "" {
		printed = printed[:len(printed)-1]
	}
	if lines > 0 && len(printed) > lines {
		printed = printed[len(printed)-lines:]
	}
	return strings.Join(printed, "\n")
}
