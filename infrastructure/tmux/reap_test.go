package tmux

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// The screens here are what Claude Code prints, cut down to the lines that
// say what it is doing: the rest of the screen never decides anything.
func TestClassifyPane(t *testing.T) {
	const rule = "────────────────────────────────────────\n"
	cases := []struct {
		name   string
		screen string
		want   application.PaneState
	}{
		{"an empty input line is a prompt mark and a no-break space",
			"● Done.\n\n" + rule + "❯\u00a0\n" + rule + "  ? for shortcuts\n", application.PaneIdle},
		{"an empty input line with a plain space", "❯ \n", application.PaneIdle},
		{"an empty input line with nothing after the mark", "❯\n", application.PaneIdle},
		{"text on the input line is not idle", rule + "❯ half a sente\n" + rule, application.PaneInput},
		{"text after a no-break space is not idle", "❯\u00a0half a sente\n", application.PaneInput},
		{"a working session says how to interrupt it", "✻ Thinking… (12s · esc to interrupt)\n\n❯\u00a0\n", application.PaneWorking},
		{"a screen with no prompt on it is not idle", "Do you want to proceed?\n 1. Yes\n 2. No\n", application.PaneInput},
		{"a blank screen is not idle", "\n\n\n", application.PaneInput},
		{"a mark that is not at the start of a line is not the input line", "  ❯\u00a0\n", application.PaneInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyPane(tc.screen); got != tc.want {
				t.Errorf("classifyPane(%q) = %q, want %q", tc.screen, got, tc.want)
			}
		})
	}
}

func TestNoServerIsAnAnswerNotAFailure(t *testing.T) {
	for _, said := range []string{
		"tmux list-windows -a: exit status 1: no server running on /tmp/tmux-1000/mw-test",
		"tmux list-windows -a: exit status 1: error connecting to /tmp/tmux-1000/mw-test (No such file or directory)",
	} {
		if !noServer(errString(said)) {
			t.Errorf("expected %q to be read as no server", said)
		}
	}
	if noServer(errString("tmux list-windows -a: exit status 1: protocol version mismatch")) {
		t.Errorf("a protocol mismatch is a failure, not an absence")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestOnlyAWindowIDIsClosable(t *testing.T) {
	for _, id := range []string{"@0", "@3", "@117"} {
		if err := windowID(id); err != nil {
			t.Errorf("expected %s to be a window id: %v", id, err)
		}
	}
	// A name or a pattern would have tmux choose the window.
	for _, not := range []string{"", "@", "3", "mayor-2026-09-19-12", "mayor-*", "=mayor:", "@3.1", "@3 ", ":", "%3", "@x"} {
		if err := windowID(not); err == nil {
			t.Errorf("expected %q to be refused as a window id", not)
		}
	}
}
