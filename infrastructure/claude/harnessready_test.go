package claude

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1329: HarnessReadyRunner waits for claude on the line this adapter
// builds, with the host's options on it, and starts at once a line that only
// says claude in a path.
func TestHarnessReadyRunnerAsksForClaudeOnTheLineThisAdapterBuilds(t *testing.T) {
	spec, err := New(WithNice(10), WithEnvFile("/home/me/.config/mw/beads.env"), WithBeadsServerHost("hub")).Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling: %v", err)
	}
	if asks := asksBeforeStart(t, spec); asks != 2 {
		t.Errorf("expected claude asked for on each of the 2 tries before its session starts, got %d asks", asks)
	}
}

func TestHarnessReadyRunnerStartsAtOnceASessionThatRunsCodexAndSaysClaude(t *testing.T) {
	spec := application.SessionSpec{Name: "mw-gq6_6"}
	spec.Command = []string{"/bin/sh", "-c", "cd /home/me/claude-work && nice -n 10 codex exec --json 'about claude'"}
	if asks := asksBeforeStart(t, spec); asks != 0 {
		t.Errorf("expected no ask of claude for a session that runs codex, got %d", asks)
	}
}

func asksBeforeStart(t *testing.T, spec application.SessionSpec) int {
	t.Helper()
	asks := 0
	runner := application.HarnessReadyRunner{Runner: apptest.NewFakeRunner(), Host: "vps", Tries: 2,
		Sleep: func(context.Context, time.Duration) error { return nil },
		Ready: func(context.Context) error { asks++; return errors.New("not yet") }}
	_ = runner.Start(context.Background(), spec)
	return asks
}
