package sessionshell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestShellQuoteLeavesPlainWordsAloneAndWrapsTheRest(t *testing.T) {
	for _, word := range []string{"claude", "--model", "/a/b/boot.md", "a,b=c+d%e@f:g"} {
		if got := ShellQuote(word); got != word {
			t.Errorf("expected %q to be left alone, got %q", word, got)
		}
	}
	wrapped := map[string]string{
		"":          "''",
		"two words": "'two words'",
		"it's":      `'it'\''s'`,
		"$HOME":     "'$HOME'",
	}
	for word, want := range wrapped {
		if got := ShellQuote(word); got != want {
			t.Errorf("quoting %q: expected %q, got %q", word, want, got)
		}
	}
}

func TestShellLineJoinsTheWordsItQuoted(t *testing.T) {
	got := ShellLine([]string{"agent", "--name", "mw-1", "it's a story"})
	if want := `agent --name mw-1 'it'\''s a story'`; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestResultIsWrittenBesideThenMovedIntoPlace(t *testing.T) {
	got := ResultRedirect([]string{"agent", "go"}, "/v/runs/mw-1/result.json")
	want := "agent go > /v/runs/mw-1/result.json.tmp; mv /v/runs/mw-1/result.json.tmp /v/runs/mw-1/result.json"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func launch() application.Launch {
	return application.Launch{
		Heartbeat: []string{"mw", "next", "--heartbeat", "mw-1"},
		After:     []string{"mw", "next", "mw-1"},
	}
}

func TestHeartbeatRunsBesideAndIsWaitedForBeforeWhatFollows(t *testing.T) {
	l := launch()
	got := WithHeartbeat("agent go", l)
	for _, want := range []string{"mw next --heartbeat mw-1 & HB=$!; agent go; kill $HB 2>/dev/null; wait $HB 2>/dev/null"} {
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	}
	l.HeartbeatLog = "/v/hb.log"
	got = WithHeartbeat("agent go", l)
	for _, want := range []string{"mkfifo", "tee -a /v/hb.log", "HBT=$!", "wait $HBT", `rm -rf "$HBD"`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
}

func TestAfterIsChainedWithASemicolonAndKeptInItsLog(t *testing.T) {
	l := launch()
	if got, want := WithAfter("agent go", l), "agent go; mw next mw-1"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	l.AfterLog = "/v/next.log"
	got := AfterLine(l)
	for _, want := range []string{"{ printf ", application.CloseOutStarted, "; mw next mw-1; s=$?; ", application.CloseOutEnded} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
	if want := "; } 2>&1 | tee -a /v/next.log"; !strings.HasSuffix(got, want) {
		t.Errorf("expected the line to end %q, got %q", want, got)
	}
	l.After = nil
	if got := WithAfter("agent go", l); got != "agent go" {
		t.Errorf("expected no After to add nothing, got %q", got)
	}
}

func TestBdShimIsOnlyAddedWhenALauncherAsksForIt(t *testing.T) {
	if got := WithBdShim("agent go", nil); got != "agent go" {
		t.Errorf("expected no retry to add nothing, got %q", got)
	}
	got := WithBdShim("agent go", []string{"mw", "bd"})
	for _, want := range []string{"SHIM=$(mktemp -d)", `MW_BD_REAL="$REAL"`, "exec mw bd", "agent go; if [ -n \"$SHIM\" ]; then rm -rf \"$SHIM\"; fi"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
}

func TestPreludeReadsEachEnvFileThenSetsTheServerHost(t *testing.T) {
	got := Prelude("agent go", Env{File: "/h/beads.env", HarnessFile: "/h/claude.env", ServerHost: "10.0.0.2"})
	want := "if [ -r /h/beads.env ]; then set -a; . /h/beads.env; set +a; fi; " +
		"if [ -r /h/claude.env ]; then set -a; . /h/claude.env; set +a; fi; " +
		"export BEADS_DOLT_SERVER_HOST=10.0.0.2; agent go"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	if got := Prelude("agent go", Env{}); got != "agent go" {
		t.Errorf("expected an empty Env to add nothing, got %q", got)
	}
}

// runAfter runs the close-out as the session's line would, returning what the
// log kept. The close-out is a script that does what the test says.
func runAfter(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	closeOut := filepath.Join(dir, "closeout")
	if err := os.WriteFile(closeOut, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "next-1.log")
	l := launch()
	l.After = []string{closeOut}
	l.AfterLog = log
	_ = exec.Command("sh", "-c", AfterLine(l)).Run()
	kept, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("expected the log to exist: %v", err)
	}
	return string(kept)
}

// mw-gq6.1038: a close-out that says nothing still leaves its start line and
// how it ended, so an empty log no longer hides that it ran at all.
func TestACloseOutThatSaysNothingStillLeavesItsStartAndItsExitStatus(t *testing.T) {
	kept := runAfter(t, "exit 3")
	lines := strings.Split(strings.TrimRight(kept, "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], application.CloseOutStarted+" 20") || !strings.HasSuffix(lines[0], "Z") {
		t.Errorf("expected a start line with a UTC time, then an end line, got %q", kept)
	}
	if len(lines) == 2 && lines[1] != application.CloseOutEnded+": exit 3" {
		t.Errorf("expected the end line to carry exit 3, got %q", lines[1])
	}
}

// A close-out killed by a signal is named by it.
func TestACloseOutKilledBySignalIsEndedWithTheSignalNamed(t *testing.T) {
	kept := runAfter(t, "kill -TERM $$")
	if want := application.CloseOutEnded + ": exit 143 (signal 15)"; !strings.Contains(kept, want) {
		t.Errorf("expected %q in %q", want, kept)
	}
}

// A close-out whose whole pane is killed (the host shut down under it) cannot
// write an end line; the start line it wrote first is what is left.
func TestACloseOutKilledWithItsShellLeavesOnlyItsStartLine(t *testing.T) {
	kept := runAfter(t, "kill -KILL $PPID")
	if !strings.HasPrefix(kept, application.CloseOutStarted+" 20") || strings.Contains(kept, application.CloseOutEnded) {
		t.Errorf("expected only the start line, got %q", kept)
	}
}
