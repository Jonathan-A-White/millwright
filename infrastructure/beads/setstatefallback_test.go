package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// stateBd is a stand-in bd whose set-state says setStateSays and exits 1, whose
// show prints a story carrying run:running, and which logs the arguments of every
// other command it is run with, one line each, to the returned log (mw-gq6.1046).
func stateBd(t *testing.T, setStateSays string) (*beads.Gateway, func() string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	program := filepath.Join(dir, "bd-state")
	script := fmt.Sprintf(`#!/bin/sh
# the subcommand is the first word after the global flags
while [ "$1" = "-C" ] || [ "$1" = "--actor" ]; do shift 2; done
case "$1" in
set-state) echo '%s' >&2; exit 1;;
show) echo '[{"id":"mw-x.1","labels":["run:running","other:kept"]}]';;
*) echo "$*" >> %q;;
esac
`, setStateSays, log)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	gateway := beads.New(t.TempDir(), beads.WithProgram(program))
	return gateway, func() string {
		data, _ := os.ReadFile(log)
		return string(data)
	}
}

func TestAStateWhoseEventCannotBeFiledIsKeptAsTheLabelAloneAndSaidSo(t *testing.T) {
	gateway, logged := stateBd(t, "Error: creating event: invalid dependency mw-x.1.3 -> mw-x.1: failed to check for dependency cycle: invalid connection")

	err := gateway.SetStoryState(context.Background(), "mw-x.1", "run", "landed", "landed at abc")

	if err != nil {
		t.Fatalf("expected the label to be written alone, got %v", err)
	}
	said := logged()
	if !strings.Contains(said, "update mw-x.1 --remove-label run:running --add-label run:landed") {
		t.Errorf("expected the old label swapped for the new by bd update, got:\n%s", said)
	}
	if strings.Contains(said, "other:kept") {
		t.Errorf("expected the story's other labels left alone, got:\n%s", said)
	}
	if !strings.Contains(said, "comment mw-x.1") || !strings.Contains(said, "recorded without its event") {
		t.Errorf("expected a note on the story that the event was skipped, got:\n%s", said)
	}
}

func TestAStateWhoseSetStateTimesOutIsKeptAsTheLabelAlone(t *testing.T) {
	gateway, logged := stateBd(t, "read tcp 10.88.0.3:49398->10.88.0.2:3307: i/o timeout")

	if err := gateway.SetStoryState(context.Background(), "mw-x.1", "run", "landed", ""); err != nil {
		t.Fatalf("expected the label to be written alone, got %v", err)
	}
	if !strings.Contains(logged(), "--remove-label run:running --add-label run:landed") {
		t.Errorf("expected the label swapped, got:\n%s", logged())
	}
}

func TestAStateThatFailsForAnotherReasonIsNotSwallowedByTheFallback(t *testing.T) {
	gateway, logged := stateBd(t, "Error: no such issue: mw-x.1")

	err := gateway.SetStoryState(context.Background(), "mw-x.1", "run", "landed", "")

	if err == nil || !strings.Contains(err.Error(), "no such issue") {
		t.Fatalf("expected bd's own words as the error, got %v", err)
	}
	if logged() != "" {
		t.Errorf("expected no label or note written, got:\n%s", logged())
	}
}
