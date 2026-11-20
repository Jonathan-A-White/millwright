package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// bigEpicStandIn is a stand-in bd for one epic, t-epic, of children stories,
// that answers bd show of it and bd list --parent of it as bd does: every child
// with --limit 0, the first --limit of them otherwise (bd's default is 50). It
// writes each call's arguments, one line a call, to the log it returns.
func bigEpicStandIn(t *testing.T, children int) (g *beads.Gateway, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	log = filepath.Join(dir, "calls")
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >> %q
cmd=""
limit=50
prev=""
for arg in "$@"; do
  case "$arg" in
    show|list) [ -z "$cmd" ] && cmd="$arg" ;;
  esac
  [ "$prev" = "--limit" ] && limit="$arg"
  prev="$arg"
done
case "$cmd" in
  show)
    printf '[{"id":"t-epic","title":"A big epic","status":"open","issue_type":"epic"}]' ;;
  list)
    i=1
    printf '['
    while [ $i -le %d ]; do
      if [ "$limit" != 0 ] && [ $i -gt "$limit" ]; then break; fi
      [ $i = 1 ] || printf ','
      printf '{"id":"t-epic.%%d","title":"A story","status":"closed","issue_type":"task","parent":"t-epic"}' $i
      i=$((i+1))
    done
    printf ']' ;;
  *) printf '[]' ;;
esac
`, log, children)
	path := filepath.Join(dir, "bd-big-epic")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	return beads.New(dir, beads.WithProgram(path)), log
}

// An epic of more than application.BdBigParentChildren children is not listed
// whole: bd was stalled for every seat on a host by an unbounded list of one
// (mw-gq6.1504). The Gateway's own read of an epic's children, when it keeps no
// list of every bead, reads a bounded page and refuses with the epic named and
// the reason, and never asks bd for --limit 0 of it (mw-gq6.1535).
func TestChildrenOfABigEpicIsRefusedNotListedWhole(t *testing.T) {
	gateway, log := bigEpicStandIn(t, 700)

	_, err := gateway.ShowEpic(context.Background(), "t-epic")
	if err == nil {
		t.Fatal("expected ShowEpic of an epic of 700 children to be refused, it listed them")
	}
	for _, want := range []string{"t-epic", fmt.Sprint(application.BdBigParentChildren), "children"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q should name %q", err, want)
		}
	}
	calls, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, call := range strings.Split(string(calls), "\n") {
		if strings.Contains(call, "--parent") && strings.Contains(call, "--limit 0") {
			t.Errorf("bd was asked for every child of a big epic: %q", call)
		}
	}
}

// An epic at or under the limit is listed whole as before.
func TestChildrenOfAnEpicUnderTheLimitAreAllRead(t *testing.T) {
	gateway, _ := bigEpicStandIn(t, application.BdBigParentChildren)

	epic, err := gateway.ShowEpic(context.Background(), "t-epic")
	if err != nil {
		t.Fatalf("ShowEpic: %v", err)
	}
	if len(epic.Stories) != application.BdBigParentChildren {
		t.Errorf("expected %d stories, got %d", application.BdBigParentChildren, len(epic.Stories))
	}
}
