package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runEvents runs mw events with args and returns what it printed.
func runEvents(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"events"}, args...))
	err := root.Execute()
	return out.String(), err
}

func eventsHome(t *testing.T) string {
	t.Helper()
	mwConfig(t, "vault = \"/nowhere/vault\"\nhost = \"laptop\"\n")
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	t.Setenv("MW_EVENTS_LOG_PATH", path)
	return path
}

func TestEventsEmitAppendsAJobEventAndTailPrintsIt(t *testing.T) {
	path := eventsHome(t)
	out, err := runEvents(t, "emit", "--kind", "job", "--bead", "mw-1", "--from", "scheduled", "--to", "running", "--actor", "dispatch@laptop")
	if err != nil {
		t.Fatalf("mw events emit: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "1" {
		t.Fatalf("expected emit to print the seq it was given, got %q", out)
	}
	if _, err := runEvents(t, "emit", "--kind", "job", "--from", "running", "--to", "done", "--detail", "ok"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || strings.Count(string(data), "\n") != 2 {
		t.Fatalf("expected two lines in %s, got %q (%v)", path, data, err)
	}
	out, err = runEvents(t, "tail", "--since", "1")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "2 ") || !strings.Contains(lines[0], " job - mw@laptop running->done ok") {
		t.Fatalf("tail --since 1 printed %q, want only seq 2, acted by mw@laptop", out)
	}
	if out, _ := runEvents(t, "tail"); strings.Count(out, "\n") != 2 {
		t.Fatalf("tail with no --since printed %q, want both events", out)
	}
}

func TestEventsEmitRefusesWhatTheMachineForbids(t *testing.T) {
	path := eventsHome(t)
	for _, args := range [][]string{
		{"emit", "--kind", "job", "--from", "scheduled", "--to", "done"},
		{"emit", "--kind", "weather"},
		{"emit"},
	} {
		if out, err := runEvents(t, args...); err == nil {
			t.Fatalf("mw events %v: expected a refusal, got\n%s", args, out)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a refused emit wrote the log: %v", err)
	}
}
