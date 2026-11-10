package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// stateBd is a stand-in bd whose show prints a story carrying run:running and
// mainred:mw-bug.1, and which logs the arguments of every other command it is
// run with, one line each, to the returned log. A set-state is logged too, so a
// test can say mw no longer runs one (mw-t5shinh.12).
func stateBd(t *testing.T) (*beads.Gateway, func() string) {
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
echo "$*" >> %q
case "$1" in
show) echo '[{"id":"mw-x.1","labels":["run:running","other:kept","mainred:mw-bug.1"]}]';;
esac
`, log)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	gateway := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithActor("mw@laptop"),
		beads.WithClock(func() time.Time { return time.Date(2026, 11, 10, 14, 41, 5, 0, time.UTC) }))
	return gateway, func() string {
		data, _ := os.ReadFile(log)
		return string(data)
	}
}

func TestAStateOfARunIsOneBdUpdateThatSwapsTheLabelAndAppendsANotesLine(t *testing.T) {
	gateway, logged := stateBd(t)

	if err := gateway.SetStoryState(context.Background(), "mw-x.1", "run", "landed", "landed at abc"); err != nil {
		t.Fatalf("expected the state to be recorded, got %v", err)
	}

	lines := strings.Split(strings.TrimSpace(logged()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly one bd, got %d:\n%s", len(lines), logged())
	}
	said := lines[0]
	if !strings.HasPrefix(said, "update mw-x.1 ") {
		t.Errorf("expected a bd update, got %q", said)
	}
	for _, unwanted := range []string{"set-state", "show", "comment"} {
		if strings.Contains(said, unwanted) {
			t.Errorf("expected no bd %s, got %q", unwanted, said)
		}
	}
	if !strings.Contains(said, "--remove-label run:running") || !strings.Contains(said, "--add-label run:landed") {
		t.Errorf("expected the run labels swapped, got %q", said)
	}
	if strings.Contains(said, "--remove-label run:landed") {
		t.Errorf("expected the new label not to be removed, got %q", said)
	}
	if strings.Contains(said, "other:kept") {
		t.Errorf("expected the story's other labels left alone, got %q", said)
	}
	if !strings.Contains(said, "--append-notes=2026-11-10T14:41:05Z run=landed mw@laptop landed at abc") {
		t.Errorf("expected the notes line to carry the time, the state, the actor and the reason, got %q", said)
	}
}

func TestAStateWhoseBdFailsIsReturnedAsBdSaidIt(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "bd-fails")
	script := "#!/bin/sh\necho 'Error: no such issue: mw-x.1' >&2\nexit 1\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	gateway := beads.New(t.TempDir(), beads.WithProgram(program))

	err := gateway.SetStoryState(context.Background(), "mw-x.1", "run", "landed", "")

	if err == nil || !strings.Contains(err.Error(), "no such issue") {
		t.Fatalf("expected bd's own words as the error, got %v", err)
	}
}

func TestARebaseAndAMergedTestsStateMoveTheirLabelsWithoutAShow(t *testing.T) {
	for _, dimension := range []string{"rebase", "mergedtests"} {
		gateway, logged := stateBd(t)

		if err := gateway.SetStoryState(context.Background(), "mw-x.1", dimension, "sent-back", "sent back"); err != nil {
			t.Fatalf("%s: %v", dimension, err)
		}
		said := strings.TrimSpace(logged())
		if strings.Contains(said, "\n") || strings.Contains(said, "show") || strings.Contains(said, "set-state") {
			t.Errorf("%s: expected one bd update, got:\n%s", dimension, said)
		}
		if !strings.Contains(said, "--add-label "+dimension+":sent-back") ||
			!strings.Contains(said, "--append-notes=2026-11-10T14:41:05Z "+dimension+"=sent-back mw@laptop sent back") {
			t.Errorf("%s: expected the label and the notes line, got %q", dimension, said)
		}
	}
}

func TestAMainRedStateSwapsTheBeadItNamedForTheNewValue(t *testing.T) {
	gateway, logged := stateBd(t)

	if err := gateway.SetStoryState(context.Background(), "mw-x.1", "mainred", "fixed", "the fix landed"); err != nil {
		t.Fatalf("recording mainred=fixed: %v", err)
	}

	said := logged()
	if !strings.Contains(said, "show mw-x.1") {
		t.Errorf("expected the story's mainred label to be read, got:\n%s", said)
	}
	if !strings.Contains(said, "update mw-x.1 --remove-label mainred:mw-bug.1 --add-label mainred:fixed") {
		t.Errorf("expected the named bead's label swapped for fixed, got:\n%s", said)
	}
	if strings.Contains(said, "set-state") {
		t.Errorf("expected no set-state, got:\n%s", said)
	}
}

func TestAStateWithNoDimensionIsRefused(t *testing.T) {
	gateway, logged := stateBd(t)
	if err := gateway.SetStoryState(context.Background(), "mw-x.1", "", "x", ""); err == nil {
		t.Fatal("expected a state with no dimension to be refused")
	}
	if logged() != "" {
		t.Errorf("expected no bd run, got %q", logged())
	}
}
