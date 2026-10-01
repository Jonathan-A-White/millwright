package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// talkCount names each test's tmux socket apart from every other's.
var talkCount int

// talkModelCmd runs mw talk model against a tmux server that is not there: the
// socket is named for this test alone, so nothing here can reach the default
// server, which holds the person's windows. What would be started detached is
// recorded, not started.
func talkModelCmd(t *testing.T, vault string, args ...string) (out string, started [][]string, err error) {
	t.Helper()
	mwConfig(t, "vault = \""+vault+"\"\nhost = \"laptop\"\n")
	talkCount++
	t.Setenv(TmuxSocketEnv, fmt.Sprintf("mw-test-talk-%d-%d", os.Getpid(), talkCount))
	was := startDetached
	t.Cleanup(func() { startDetached = was })
	startDetached = func(args []string) error {
		started = append(started, args)
		return nil
	}

	buf := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(append([]string{"talk", "model"}, args...))
	err = root.Execute()
	return buf.String(), started, err
}

func TestTalkModelHelpRuns(t *testing.T) {
	out, started, err := talkModelCmd(t, t.TempDir(), "--help")
	if err != nil {
		t.Fatalf("mw talk model --help: %v", err)
	}
	if !strings.Contains(out, "opus|sonnet|fable") || !strings.Contains(out, "--foreground") {
		t.Errorf("expected the help to name the models and --foreground, got:\n%s", out)
	}
	if len(started) != 0 {
		t.Errorf("expected --help to start nothing, it started %q", started)
	}
}

func TestTalkModelStartsTheWatchDetached(t *testing.T) {
	vault := t.TempDir()
	out, started, err := talkModelCmd(t, vault, "sonnet", "--limit", "5m")
	if err != nil {
		t.Fatalf("mw talk model sonnet: %v", err)
	}
	want := [][]string{{"talk", "model", "sonnet", "--foreground", "--interval", "2s", "--limit", "5m0s"}}
	if !reflect.DeepEqual(started, want) {
		t.Errorf("expected the watch started detached as %q, got %q", want, started)
	}
	if !strings.Contains(out, filepath.Join(vault, ".mayor-talk.log")) {
		t.Errorf("expected the command to say where the watch logs, got %q", out)
	}
}

func TestTalkModelRefusesAModelItDoesNotSwitchTo(t *testing.T) {
	vault := t.TempDir()
	out, started, err := talkModelCmd(t, vault, "haiku")
	if err == nil || !strings.Contains(err.Error(), "opus, sonnet or fable") {
		t.Fatalf("expected a refusal naming the models, got %q, %v", out, err)
	}
	if len(started) != 0 {
		t.Errorf("expected a refused model to start nothing, it started %q", started)
	}
	if _, statErr := os.Stat(filepath.Join(vault, ".mayor-talk.log")); statErr == nil {
		t.Errorf("expected a refused model to log nothing")
	}
}

func TestTalkModelInTheForegroundGivesUpWithAnErrorSoTheCommandExitsNonZero(t *testing.T) {
	vault := t.TempDir()
	out, started, err := talkModelCmd(t, vault, "opus", "--foreground", "--interval", "10ms", "--limit", "50ms")
	if err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("expected the watch to give up, got %q, %v", out, err)
	}
	if len(started) != 0 {
		t.Errorf("expected --foreground to start nothing detached, it started %q", started)
	}
	logged, readErr := os.ReadFile(filepath.Join(vault, ".mayor-talk.log"))
	if readErr != nil {
		t.Fatalf("reading the talk log: %v", readErr)
	}
	if lines := strings.Split(strings.TrimSpace(string(logged)), "\n"); len(lines) != 2 {
		t.Errorf("expected an armed line and a gave-up line, got:\n%s", logged)
	}
}
