package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1328: a seat's session is opened in a window, not through the Runner,
// and mw-gq6.1281's wait for claude did not reach it. A seat starts only once
// claude --version answers on the host.
func TestASeatUpWaitsForClaudeToAnswerBeforeItOpensTheWindow(t *testing.T) {
	files := &fakeSeatFiles{start: aSeatStart(nil)}
	windows := apptest.NewFakeWindows()

	asks := 0
	var waits []time.Duration
	up := aSeatUp(files, application.HarnessReadyWindows{Windows: windows, Host: "laptop",
		Sleep: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			return nil
		},
		Ready: func(context.Context) error {
			asks++
			if len(windows.Opened) != 0 {
				t.Errorf("ask %d: expected no window opened before claude answers, got %v", asks, windows.Opened)
			}
			if asks < 3 {
				return errors.New("claude: command not found")
			}
			return nil
		}})
	if _, err := up.Run(context.Background()); err != nil {
		t.Fatalf("starting the seat: %v", err)
	}
	if len(windows.Opened) != 1 {
		t.Fatalf("expected the window opened once claude answered, got %v", windows.Opened)
	}
	if asks != 3 || len(waits) != 2 || waits[0] != application.DefaultHarnessPoll {
		t.Errorf("expected 3 asks with 2 waits of %s between, got %d asks and waits %v", application.DefaultHarnessPoll, asks, waits)
	}
}

func TestASeatUpWhoseClaudeNeverAnswersOpensNoWindow(t *testing.T) {
	files := &fakeSeatFiles{start: aSeatStart(nil)}
	windows := apptest.NewFakeWindows()
	up := aSeatUp(files, application.HarnessReadyWindows{Windows: windows, Host: "laptop", Tries: 3,
		Sleep: func(context.Context, time.Duration) error { return nil },
		Ready: func(context.Context) error { return errors.New("claude: command not found") }})
	_, err := up.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "claude --version did not answer") {
		t.Fatalf("expected the seat to say claude never answered, got %v", err)
	}
	if len(windows.Opened) != 0 {
		t.Fatalf("expected no window opened, got %v", windows.Opened)
	}
}

// The seat's argv reaches the window through the env-file script, which runs
// "$@": the program is the word after the script's own arguments.
func TestAWindowThroughTheEnvFileScriptWaitsForClaudeToo(t *testing.T) {
	script := `f=$1; h=$2; c=$3; shift 3; exec "$@"`
	for name, command := range map[string][]string{
		"bare":   {"claude", "--model", "opus", "kickoff"},
		"script": {"/bin/sh", "-c", script, "sh", "/env", "", "", "claude", "--model", "opus", "kickoff"},
	} {
		windows := apptest.NewFakeWindows()
		asks := 0
		wrapped := application.HarnessReadyWindows{Windows: windows, Host: "laptop", Tries: 2,
			Sleep: func(context.Context, time.Duration) error { return nil },
			Ready: func(context.Context) error { asks++; return errors.New("no claude") }}
		err := wrapped.Open(context.Background(), application.WindowSpec{Name: "mayor-1", Dir: "/v", Command: command})
		if err == nil || asks != 2 || len(windows.Opened) != 0 {
			t.Errorf("%s: expected claude asked for twice and no window, got err %v, %d asks, %v", name, err, asks, windows.Opened)
		}
	}
}

func TestAWindowThatRunsAnotherProgramOpensAtOnce(t *testing.T) {
	windows := apptest.NewFakeWindows()
	wrapped := application.HarnessReadyWindows{Windows: windows, Host: "laptop",
		Ready: func(context.Context) error { t.Error("claude asked for by a window that does not run it"); return nil }}
	err := wrapped.Open(context.Background(), application.WindowSpec{Name: "w-1", Dir: "/v", Command: []string{"codex", "claude notes"}})
	if err != nil || len(windows.Opened) != 1 {
		t.Fatalf("expected the window opened at once, got %v, %v", err, windows.Opened)
	}
}
