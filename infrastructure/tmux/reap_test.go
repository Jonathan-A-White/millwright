package tmux

import (
	"os"
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
		{"claude's theme screen is its first-run screen",
			"Welcome to Claude Code v2.1.282\n\nLet's get started.\n\nChoose the text style that looks best with your terminal\nTo change this later, run /theme\n1. Auto (match terminal)\n2. Dark mode\n",
			application.PaneFirstRun},
		{"claude's login menu is its first-run screen",
			"Select login method:\n1. Claude account with subscription\n2. Anthropic Console account\n",
			application.PaneFirstRun},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyPane(tc.screen); got != tc.want {
				t.Errorf("classifyPane(%q) = %q, want %q", tc.screen, got, tc.want)
			}
		})
	}
}

// Claude Code paints a suggested next prompt on an empty input line as dim
// text. It is not something a person typed: capture-pane -e keeps the dim
// escape, and only undimmed text after the mark is a draft.
func TestClassifyPaneReadsGhostSuggestionsAsAnEmptyInputLine(t *testing.T) {
	cases := []struct {
		name   string
		screen string
		want   application.PaneState
	}{
		{"a dim suggestion after the mark is an empty input line",
			"❯\u00a0\x1b[2mWait for the Mayor's next mail.\x1b[0m\n", application.PaneIdle},
		{"a dim suggestion after a coloured mark is an empty input line",
			"\x1b[38;5;153m❯\x1b[39m\u00a0\x1b[2mthe dot is green\x1b[0m\n", application.PaneIdle},
		{"a dim suggestion ended by normal intensity is an empty input line",
			"❯ \x1b[1;2mthe dot is green\x1b[22m\n", application.PaneIdle},
		{"a colour's own 2 is not dim: text in 38;2;r;g;b is a draft",
			"❯ \x1b[38;2;10;20;30mhalf a sente\x1b[0m\n", application.PaneInput},
		{"undimmed text after the mark is a draft",
			"❯\u00a0half a sente\n", application.PaneInput},
		{"undimmed text after a reset is a draft",
			"❯ \x1b[2mthe dot is green\x1b[0m and more\n", application.PaneInput},
		{"dim text with a typed draft before it is a draft",
			"❯ half\x1b[2m a suggestion\x1b[0m\n", application.PaneInput},
		{"an escape that draws no text leaves the input line empty",
			"❯\x1b[7m \x1b[27m\x1b[0m\n", application.PaneIdle},
		{"a working session with a dim word in its marker is still working",
			"✻ Thinking… (12s · \x1b[2mesc\x1b[0m to interrupt)\n❯\u00a0\n", application.PaneWorking},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyPane(tc.screen); got != tc.want {
				t.Errorf("classifyPane(%q) = %q, want %q", tc.screen, got, tc.want)
			}
		})
	}
}

// The captured screens in testdata are shared with contrib/mail-notify's own
// test, so that the notifier and the tick and reaper answer the same.
func TestClassifyPaneOnCapturedScreens(t *testing.T) {
	for file, want := range map[string]application.PaneState{
		"testdata/ghost-suggestion.txt": application.PaneIdle,
		"testdata/real-draft.txt":       application.PaneInput,
	} {
		screen, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if got := classifyPane(string(screen)); got != want {
			t.Errorf("classifyPane(%s) = %q, want %q", file, got, want)
		}
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
