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

// mw events emit --kind control carries a word to the factory, refusing a word
// it does not know or one that names the wrong thing; a seat subscribed to
// control hears it.
func TestEventsEmitCarriesControlWords(t *testing.T) {
	eventsHome(t)
	for _, args := range [][]string{
		{"emit", "--kind", "control", "--detail", "pause-host laptop"},
		{"emit", "--kind", "control", "--bead", "mw-1", "--detail", "cancel"},
		{"emit", "--kind", "control", "--bead", "mw-1", "--detail", "priority 0"},
	} {
		if out, err := runEvents(t, args...); err != nil {
			t.Fatalf("mw events %v: %v\n%s", args, err, out)
		}
	}
	for _, args := range [][]string{
		{"emit", "--kind", "control", "--detail", "explode"},
		{"emit", "--kind", "control", "--detail", "cancel"},
		{"emit", "--kind", "control", "--bead", "mw-1", "--detail", "pause-host laptop"},
	} {
		if out, err := runEvents(t, args...); err == nil {
			t.Fatalf("mw events %v: expected a refusal, got\n%s", args, out)
		}
	}
	out, err := runEvents(t, "wait", "--for", "builder", "--kinds", "control", "--since", "0", "--limit", "5s")
	if err != nil {
		t.Fatalf("wait: %v\n%s", err, out)
	}
	if strings.Count(out, " control ") != 3 || !strings.Contains(out, "pause-host laptop") {
		t.Fatalf("a seat subscribed to control heard %q, want the three words", out)
	}
}

func TestEventsWaitReturnsOnTheFirstMatchingEventAfterThoseItIgnores(t *testing.T) {
	eventsHome(t)
	for _, args := range [][]string{
		{"emit", "--kind", "job", "--from", "scheduled", "--to", "running"},
		{"emit", "--kind", "message", "--bead", "mw-1", "--detail", "direct:abc"},
		{"emit", "--kind", "mail", "--bead", "mw-m1", "--detail", "mayor"},
	} {
		if out, err := runEvents(t, args...); err != nil {
			t.Fatalf("mw events %v: %v\n%s", args, err, out)
		}
	}
	out, err := runEvents(t, "wait", "--for", "mayor", "--kinds", "mail,card-answered", "--since", "0", "--limit", "5s")
	if err != nil {
		t.Fatalf("wait: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || lines[0] != "New events for mayor: 1. Run mw events tail --since 2." || !strings.HasPrefix(lines[1], "3 ") {
		t.Fatalf("wait printed %q, want the header and seq 3 only", out)
	}
}

func TestEventsWaitRefusesABadKindListingTheKinds(t *testing.T) {
	eventsHome(t)
	out, err := runEvents(t, "wait", "--for", "mayor", "--kinds", "mail,pigeon")
	if err == nil {
		t.Fatalf("a bad kind was accepted:\n%s", out)
	}
	for _, want := range []string{"pigeon", "mail", "card_answered", "landing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not say %q", err, want)
		}
	}
	if _, err := runEvents(t, "wait"); err == nil {
		t.Fatal("wait with no --for was accepted")
	}
}

func TestEventsWaitReadsTheKindsFromTheSeatsSubscribeFile(t *testing.T) {
	eventsHome(t)
	vaultDir := t.TempDir()
	mwConfig(t, "vault = \""+vaultDir+"\"\nhost = \"laptop\"\n")
	if err := os.MkdirAll(filepath.Join(vaultDir, "seats", "deputy"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(vaultDir, "seats", "deputy", "subscribe.toml")
	if err := os.WriteFile(file, []byte("kinds = [\"message\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runEvents(t, "emit", "--kind", "mail", "--bead", "mw-m1", "--detail", "deputy"); err != nil {
		t.Fatal(err)
	}
	if _, err := runEvents(t, "emit", "--kind", "message", "--bead", "mw-1", "--detail", "direct:abc"); err != nil {
		t.Fatal(err)
	}
	out, err := runEvents(t, "wait", "--for", "deputy", "--since", "0", "--limit", "5s")
	if err != nil || !strings.HasPrefix(out, "New events for deputy: 1. Run mw events tail --since 1.") {
		t.Fatalf("got %q, %v; want the message alone, as the file says", out, err)
	}
	if err := os.WriteFile(file, []byte("kinds = [\"smoke\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runEvents(t, "wait", "--for", "deputy", "--since", "0"); err == nil || !strings.Contains(err.Error(), "smoke") || !strings.Contains(err.Error(), "card_answered") {
		t.Fatalf("a bad kind in the file: %v", err)
	}
}

func TestEventsEmitEmergencyWritesTheEventInTheEmergencyLane(t *testing.T) {
	path := eventsHome(t)
	if out, err := runEvents(t, "emit", "--kind", "job", "--from", "running", "--to", "failed", "--emergency"); err != nil {
		t.Fatalf("mw events emit --emergency: %v\n%s", err, out)
	}
	if _, err := runEvents(t, "emit", "--kind", "job", "--from", "scheduled", "--to", "running"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"lane":"emergency"`) || !strings.Contains(lines[1], `"lane":"normal"`) {
		t.Fatalf("expected an emergency then a normal event, got %q", data)
	}
}

func TestEventsTrimMovesOldEventsToTheArchiveAndTailReadsThemBack(t *testing.T) {
	path := eventsHome(t)
	for i := 0; i < 6; i++ {
		if out, err := runEvents(t, "emit", "--kind", "job", "--bead", "mw-1", "--from", "scheduled", "--to", "running", "--actor", "dispatch@laptop"); err != nil {
			t.Fatalf("emit: %v\n%s", err, out)
		}
	}
	dir := filepath.Dir(path)
	if err := os.WriteFile(filepath.Join(dir, "ship.json"), []byte(`{"shipped":6}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runEvents(t, "trim", "--keep", "2")
	if err != nil || !strings.Contains(out, "Moved 4 events, up to seq 4") {
		t.Fatalf("mw events trim = %q, %v; want 4 moved", out, err)
	}
	if data, _ := os.ReadFile(path); strings.Count(string(data), "\n") != 2 {
		t.Fatalf("the log holds %q, want the newest two", data)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "archive", "log-*.jsonl")); len(matches) != 1 {
		t.Fatalf("archive files %v, want one", matches)
	}
	out, err = runEvents(t, "tail", "--since", "1")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 5 || !strings.HasPrefix(lines[0], "2 ") || !strings.HasPrefix(lines[4], "6 ") {
		t.Fatalf("tail --since 1 after a trim printed %q, want seqs 2 to 6", out)
	}
}
