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

// blockedStandIn is a stand-in bd that answers `blocked` with the rows given,
// `show <id>` with the canned bead for that id (an error for any other), and
// writes down every argv it was asked. The Gateway has no actor, so the
// subcommand is the third argument and its id the fourth.
func blockedStandIn(t *testing.T, blocked string, shown map[string]string) (*beads.Gateway, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "asked.log")
	var cases strings.Builder
	for id, bead := range shown {
		fmt.Fprintf(&cases, "    %s) printf '%%s' '[%s]' ;;\n", id, bead)
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %s
case "$3" in
  blocked) printf '%%s' '%s' ;;
  show)
    case "$4" in
%s    *) echo "no issues found matching $4" >&2; exit 1 ;;
    esac ;;
  *) exit 1 ;;
esac
`, log, blocked, cases.String())
	path := filepath.Join(dir, "bd-blocked")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(t.TempDir(), beads.WithProgram(path)), log
}

func asked(t *testing.T, log string) []string {
	t.Helper()
	said, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("reading what bd was asked: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(said)), "\n")
}

func story(id, parent string) string {
	return fmt.Sprintf(`{"id":"%s","title":"t","status":"open","issue_type":"task","parent":"%s"}`, id, parent)
}

func epic(id, host string) string {
	return fmt.Sprintf(`{"id":"%s","title":"e","status":"open","issue_type":"epic","metadata":{"host":"%s"}}`, id, host)
}

func row(id string) string {
	return fmt.Sprintf(`{"id":"%s","title":"t","status":"open","issue_type":"task"}`, id)
}

// A poured formula step waits on the step before it, so every running story
// puts steps in `bd blocked`; none of them is a story to show, and reading one
// back over a slow link is what made `mw status` run past its time. A step is
// left out before any bd show.
func TestBlockedForHostNeverReadsAPouredStep(t *testing.T) {
	gateway, log := blockedStandIn(t,
		"["+row("mw-mol-aaaaa")+","+row("mw-gq6.1")+","+row("mw-mol-bbbbb")+"]",
		map[string]string{
			"mw-gq6.1": story("mw-gq6.1", "mw-e1"),
			"mw-e1":    epic("mw-e1", "laptop"),
		})

	blocked, err := gateway.BlockedForHost(context.Background(), "laptop")
	if err != nil {
		t.Fatalf("listing what is blocked: %v", err)
	}
	if len(blocked) != 1 || blocked[0].Story.ID != "mw-gq6.1" {
		t.Fatalf("expected only mw-gq6.1 to be blocked, got %+v", blocked)
	}
	for _, line := range asked(t, log) {
		if strings.Contains(line, "-mol-") {
			t.Errorf("expected no bd call about a poured step, got %q", line)
		}
	}
}

// Stories of one epic share what it says: the epic is read once, not once per
// story, and a story whose epic sends it to another host is still left out.
func TestBlockedForHostReadsEachEpicOnce(t *testing.T) {
	gateway, log := blockedStandIn(t,
		"["+row("mw-gq6.1")+","+row("mw-gq6.2")+","+row("mw-gq6.3")+"]",
		map[string]string{
			"mw-gq6.1": story("mw-gq6.1", "mw-e1"),
			"mw-gq6.2": story("mw-gq6.2", "mw-e1"),
			"mw-gq6.3": story("mw-gq6.3", "mw-e2"),
			"mw-e1":    epic("mw-e1", "laptop"),
			"mw-e2":    epic("mw-e2", "vps"),
		})

	blocked, err := gateway.BlockedForHost(context.Background(), "laptop")
	if err != nil {
		t.Fatalf("listing what is blocked: %v", err)
	}
	if len(blocked) != 2 || blocked[0].Story.ID != "mw-gq6.1" || blocked[1].Story.ID != "mw-gq6.2" {
		t.Fatalf("expected mw-gq6.1 and mw-gq6.2 to be blocked on laptop, got %+v", blocked)
	}

	reads := map[string]int{}
	for _, line := range asked(t, log) {
		if fields := strings.Fields(line); len(fields) >= 4 && fields[2] == "show" {
			reads[fields[3]]++
		}
	}
	if reads["mw-e1"] != 1 {
		t.Errorf("expected the epic mw-e1 to be read once, got %d (%v)", reads["mw-e1"], reads)
	}
}
