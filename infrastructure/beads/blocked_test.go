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
// `show <id>...` with the canned bead for each id (an error naming any other),
// and writes down every argv it was asked. The Gateway has no actor, so the
// subcommand is the third argument and its ids follow.
func blockedStandIn(t *testing.T, blocked string, shown map[string]string) (*beads.Gateway, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "asked.log")
	var cases strings.Builder
	for id, bead := range shown {
		fmt.Fprintf(&cases, "      %s) bead='%s' ;;\n", id, bead)
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %s
case "$3" in
  blocked) printf '%%s' '%s' ;;
  show)
    shift 3
    first=1
    printf '['
    for id in "$@"; do
      case "$id" in
      --json) continue ;;
%s      *) echo "no issues found matching $id" >&2; exit 1 ;;
      esac
      [ $first = 1 ] || printf ','
      first=0
      printf '%%s' "$bead"
    done
    printf ']' ;;
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
			for _, id := range fields[3:] {
				reads[id]++
			}
		}
	}
	if reads["mw-e1"] != 1 {
		t.Errorf("expected the epic mw-e1 to be read once, got %d (%v)", reads["mw-e1"], reads)
	}
}

// servedBlockedStandIn is a stand-in bd for a served host: `blocked` answers
// the rows given, `list --id a,b` the canned bead of each id it holds, and
// `show` the same beads, one bd apiece as the real one is asked. It writes down
// every argv, one line a call.
func servedBlockedStandIn(t *testing.T, blocked string, held map[string]string) (*beads.Gateway, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.88.0.2")
	dir := t.TempDir()
	log := filepath.Join(dir, "asked.log")
	var cases strings.Builder
	for id, bead := range held {
		fmt.Fprintf(&cases, "      %s) bead='%s' ;;\n", id, bead)
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %s
emit() {
  first=1
  printf '['
  for id in "$@"; do
    case "$id" in
    --json) continue ;;
%s    *) continue ;;
    esac
    [ $first = 1 ] || printf ','
    first=0
    printf '%%s' "$bead"
  done
  printf ']'
}
case "$3" in
  blocked) printf '%%s' '%s' ;;
  list)
    ids=""
    while [ $# -gt 0 ]; do
      if [ "$1" = "--id" ]; then ids="$2"; fi
      shift
    done
    emit $(echo "$ids" | tr ',' ' ') ;;
  show) shift 3; emit "$@" ;;
  *) exit 1 ;;
esac
`, log, cases.String(), blocked)
	path := filepath.Join(dir, "bd-served")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(t.TempDir(), beads.WithProgram(path)), log
}

// On a served host `mw queue` took 195.87 s at load 25: BlockedForHost read each
// blocked story with a bd show of its own, side by side, and every bd opens its
// own connection and runs a dozen statements (240 connections, 3,565
// statements, mw-gq6.1341). All the blocked stories are read in one bd list.
func TestBlockedForHostOnAServedHostReadsTheStoriesInOneBd(t *testing.T) {
	const stories = 30
	var rows []string
	held := map[string]string{
		"mw-e1": epic("mw-e1", "laptop"),
		"mw-e2": epic("mw-e2", "laptop"),
	}
	for i := 0; i < stories; i++ {
		id := fmt.Sprintf("mw-gq6.%d", i+1)
		rows = append(rows, row(id))
		held[id] = story(id, fmt.Sprintf("mw-e%d", i%2+1))
	}
	gateway, log := servedBlockedStandIn(t, "["+strings.Join(rows, ",")+"]", held)

	blocked, err := gateway.BlockedForHost(context.Background(), "laptop")
	if err != nil {
		t.Fatalf("listing what is blocked: %v", err)
	}
	if len(blocked) != stories {
		t.Fatalf("expected %d blocked stories, got %d", stories, len(blocked))
	}
	calls := asked(t, log)
	for _, line := range calls {
		if strings.Contains(line, " show ") {
			t.Errorf("expected no bd show for a blocked story, got %q", line)
		}
	}
	if len(calls) > 4 {
		t.Errorf("expected at most 4 bd calls (blocked, the stories, the epics), got %d: %v", len(calls), calls)
	}
}

// A listing prints a story's links as edges, closed blockers among them, which
// bd show narrows to the unfinished; the row of `bd blocked` names the blockers
// still open, so the story's Needs are those and not the closed one.
func TestBlockedForHostOnAServedHostNeedsOnlyTheOpenBlockers(t *testing.T) {
	waiting := `{"id":"mw-gq6.1","title":"t","status":"open","issue_type":"task","parent":"mw-e1","dependencies":[` +
		`{"issue_id":"mw-gq6.1","depends_on_id":"mw-e1","type":"parent-child"},` +
		`{"issue_id":"mw-gq6.1","depends_on_id":"mw-gq6.0","type":"blocks"},` +
		`{"issue_id":"mw-gq6.1","depends_on_id":"mw-gq6.9","type":"blocks"}]}`
	blockedRow := `{"id":"mw-gq6.1","title":"t","status":"open","issue_type":"task","blocked_by":["mw-gq6.9"],"blocked_by_count":1}`
	gateway, _ := servedBlockedStandIn(t, "["+blockedRow+"]", map[string]string{
		"mw-gq6.1": waiting,
		"mw-e1":    epic("mw-e1", "laptop"),
	})

	blocked, err := gateway.BlockedForHost(context.Background(), "laptop")
	if err != nil {
		t.Fatalf("listing what is blocked: %v", err)
	}
	if len(blocked) != 1 {
		t.Fatalf("expected one blocked story, got %+v", blocked)
	}
	if got := blocked[0].Needs; len(got) != 1 || got[0] != "mw-gq6.9" {
		t.Errorf("expected the story to wait on mw-gq6.9 alone, got %v", got)
	}
}
