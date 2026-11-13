package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/commandlog"
)

func lastCommandLine(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the command log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	return lines[len(lines)-1]
}

func TestEveryRunIsRecorded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "commands.jsonl")
	log := commandlog.New(path)
	at := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	ticks := 0
	now := func() time.Time { ticks++; return at.Add(time.Duration(ticks) * 250 * time.Millisecond) }
	run := func(args ...string) error {
		root := newRootCmd()
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		return executeRecorded(context.Background(), root, args, log, now)
	}

	if err := run("version"); err != nil {
		t.Fatalf("mw version: %v", err)
	}
	var line struct {
		Command    string `json:"command"`
		Status     *int   `json:"status"`
		DurationMs *int64 `json:"duration_ms"`
	}
	if err := json.Unmarshal([]byte(lastCommandLine(t, path)), &line); err != nil {
		t.Fatalf("the last line is not JSON: %v", err)
	}
	if line.Command != "mw version" || line.Status == nil || *line.Status != 0 || line.DurationMs == nil || *line.DurationMs != 250 {
		t.Errorf("the last line is %+v, want mw version, status 0, 250 ms", line)
	}

	// An argument, and a refused run's status, are told apart from the command.
	if err := run("version", "secret-argument-text"); err == nil {
		t.Fatal("mw version takes no arguments, yet it succeeded")
	}
	last := lastCommandLine(t, path)
	if strings.Contains(last, "secret-argument-text") {
		t.Errorf("an argument was kept: %s", last)
	}
	if err := json.Unmarshal([]byte(last), &line); err != nil || line.Command != "mw version" || *line.Status == 0 {
		t.Errorf("the refused run's line is %s, want mw version with a status", last)
	}
}

func TestCommandNamesListTheRunnableCommands(t *testing.T) {
	names := commandNames(newRootCmd())
	found := map[string]bool{}
	for _, name := range names {
		found[name] = true
	}
	if !found["mw version"] || found["mw help"] || found["mw completion"] {
		t.Errorf("command names %v, want mw version and neither help nor completion", names)
	}
}
