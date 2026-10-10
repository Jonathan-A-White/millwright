package gitcmd_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
)

// A stand-in git writes what it was given to a file, so the test can check the
// command without a real stalled connection.
func TestACommandRunsInAProcessGroupOfItsOwnWithGitsGuards(t *testing.T) {
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s %%s %%s %%s\n' "$GIT_TERMINAL_PROMPT" "$GIT_OPTIONAL_LOCKS" "$GIT_HTTP_LOW_SPEED_LIMIT" "$GIT_HTTP_LOW_SPEED_TIME" > %q
ps -o pgid= -p $$ >> %q
pwd >> %q
`, seen, seen, seen)
	program := filepath.Join(dir, "git")
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for git: %v", err)
	}
	work := t.TempDir()

	if err := gitcmd.Command(context.Background(), program, work, "status").Run(); err != nil {
		t.Fatalf("running the stand-in: %v", err)
	}

	data, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("reading what the stand-in saw: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected three lines from the stand-in, got %q", data)
	}
	if want := "0 0 1000 60"; lines[0] != want {
		t.Errorf("expected the child to see %q, got %q", want, lines[0])
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(lines[1]))
	if err != nil {
		t.Fatalf("reading the child's process group %q: %v", lines[1], err)
	}
	if pgid == syscall.Getpgrp() {
		t.Errorf("expected the child in a process group of its own, but it shares ours (%d)", pgid)
	}
	if got, _ := filepath.EvalSymlinks(work); lines[2] != got {
		t.Errorf("expected the child to run in %s, got %s", got, lines[2])
	}
}
