package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestSessionNameIsTheStoryIDWithoutPunctuation(t *testing.T) {
	cases := []struct {
		storyID string
		want    string
	}{
		// A story id with a dot: tmux reads a dot in a target as "pane of a
		// window", so the name a session is attached to cannot keep it.
		{"mw-gq6.4", "mw-gq6_4"},
		// A colon separates session from window in a target.
		{"mw:gq6", "mw_gq6"},
		{"mw-gq6.4.1", "mw-gq6_4_1"},
		// Letters, digits, dash and underscore are left alone.
		{"mw-gq6", "mw-gq6"},
		{"already_legal99", "already_legal99"},
		// Anything else a bead id or a hand-written name might carry.
		{"mw gq6", "mw_gq6"},
		{"$mw/gq6*", "_mw_gq6_"},
		{"mw-héllo", "mw-h_llo"},
		{"", ""},
	}
	for _, c := range cases {
		if got := application.SessionName(c.storyID); got != c.want {
			t.Errorf("SessionName(%q) = %q, want %q", c.storyID, got, c.want)
		}
	}
}

func TestSessionNameIsItsOwnSessionName(t *testing.T) {
	// Sanitising twice changes nothing, so a name can be handed round freely.
	once := application.SessionName("mw-gq6.4")
	if twice := application.SessionName(once); twice != once {
		t.Errorf("SessionName is not settled: %q became %q", once, twice)
	}
}

func TestSessionSpecValidateAcceptsAWorkableSession(t *testing.T) {
	spec := application.SessionSpec{
		Name:    application.SessionName("mw-gq6.4"),
		Dir:     "/root/.mw-worktrees/mw-gq6.4",
		Env:     map[string]string{"MW_STORY": "mw-gq6.4"},
		Command: []string{"sh", "-c", "echo hello"},
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("expected a workable session spec, got %v", err)
	}
}

func TestSessionSpecValidateRefusesWhatCannotBeStarted(t *testing.T) {
	cases := []struct {
		why  string
		spec application.SessionSpec
		says string
	}{
		{
			why:  "no name",
			spec: application.SessionSpec{Command: []string{"sh"}},
			says: "name",
		},
		{
			why:  "a name no backend can be pointed at",
			spec: application.SessionSpec{Name: "mw-gq6.4", Command: []string{"sh"}},
			says: "mw-gq6_4",
		},
		{
			why:  "no command",
			spec: application.SessionSpec{Name: "mw-gq6_4"},
			says: "command",
		},
	}
	for _, c := range cases {
		err := c.spec.Validate()
		if err == nil {
			t.Errorf("expected %s to be refused", c.why)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("expected the refusal of %s to mention %q, got %q", c.why, c.says, err)
		}
	}
}

func TestSessionStatusReportsWhetherTheCommandIsStillRunning(t *testing.T) {
	running := application.SessionStatus{Name: "mw-gq6_4", State: application.StateRunning}
	if !running.Running() || running.Finished() {
		t.Errorf("expected %+v to be running and not finished", running)
	}
	exited := application.SessionStatus{Name: "mw-gq6_4", State: application.StateExited, ExitCode: 3}
	if exited.Running() || !exited.Finished() {
		t.Errorf("expected %+v to be finished and not running", exited)
	}
	gone := application.SessionStatus{Name: "mw-gq6_4", State: application.StateGone}
	if gone.Running() || gone.Finished() {
		t.Errorf("expected %+v to be neither running nor finished", gone)
	}
	unknown := application.SessionStatus{Name: "mw-gq6_4", State: application.StateExitUnknown}
	if unknown.Running() || unknown.Finished() {
		t.Errorf("expected %+v to be neither running nor finished: its exit status is not known", unknown)
	}
}

func TestRecentLinesIsTheTailWithoutTheTerminalsPadding(t *testing.T) {
	cases := []struct {
		why    string
		output string
		lines  int
		want   string
	}{
		{
			why:    "the blank lines a terminal pads its screen with are dropped",
			output: "hello\nthere\n\n\n\n",
			lines:  5,
			want:   "hello\nthere",
		},
		{
			why:    "only the last lines asked for come back",
			output: "one\ntwo\nthree\nfour\n",
			lines:  2,
			want:   "three\nfour",
		},
		{
			why:    "blank lines in the middle are output too",
			output: "one\n\ntwo\n",
			lines:  5,
			want:   "one\n\ntwo",
		},
		{
			why:    "a screen with nothing on it is nothing",
			output: "\n\n\n",
			lines:  5,
			want:   "",
		},
		{
			why:    "trailing spaces are padding as much as blank lines are",
			output: "hello\n   \n",
			lines:  5,
			want:   "hello",
		},
	}
	for _, c := range cases {
		if got := application.RecentLines(c.output, c.lines); got != c.want {
			t.Errorf("%s: RecentLines(%q, %d) = %q, want %q", c.why, c.output, c.lines, got, c.want)
		}
	}
}
