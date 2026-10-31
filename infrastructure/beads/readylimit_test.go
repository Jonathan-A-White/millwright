package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// readyLimitStandIn is a stand-in bd whose `ready` lists readyCount stories
// pathed to laptop, and only the first defaultLimit of them unless it is given
// --limit: the cap real bd puts on `bd ready` (100 by default, so that a vault
// with 133 ready beads showed 100, mw-x5ml13c). Nothing else is listed.
func readyLimitStandIn(t *testing.T, readyCount, defaultLimit int) *beads.Gateway {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	script := fmt.Sprintf(`#!/bin/sh
shift 2
case "$1" in
  --actor) shift 2 ;;
esac
cmd="$1"
limit=%d
prev=""
for arg in "$@"; do
  [ "$prev" = "--limit" ] && limit="$arg"
  prev="$arg"
done
case "$cmd" in
  ready)
    i=1
    printf '['
    while [ $i -le %d ]; do
      if [ "$limit" != 0 ] && [ $i -gt "$limit" ]; then break; fi
      [ $i = 1 ] || printf ','
      printf '{"id":"mw-r.%%d","title":"t","status":"open","issue_type":"task","metadata":{"host":"laptop","rig":"millwright"}}' $i
      i=$((i+1))
    done
    printf ']' ;;
  *) printf '[]' ;;
esac
`, defaultLimit, readyCount)
	path := filepath.Join(t.TempDir(), "bd-ready-limit")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(t.TempDir(), beads.WithProgram(path))
}

// bd caps `bd ready` at a default number of beads, so every read of what is
// ready asks for all of them: mw status listed READY (2) of 13 while mw dispatch
// passed all 13 (mw-x5ml13c).
func TestReadStoriesAreNotCutOffAtBdsDefaultLimit(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	const ready = 15
	gateway := readyLimitStandIn(t, ready, 10)

	inHand, err := gateway.WorkInHand(context.Background())
	if err != nil {
		t.Fatalf("reading what is in hand: %v", err)
	}
	if got := len(inHand.ReadyOn("laptop")); got != ready {
		t.Errorf("WorkInHand: expected all %d ready stories on laptop, got %d", ready, got)
	}

	forHost, err := gateway.ReadyForHost(context.Background(), "laptop")
	if err != nil {
		t.Fatalf("reading what is ready for laptop: %v", err)
	}
	if len(forHost) != ready {
		t.Errorf("ReadyForHost: expected all %d ready stories, got %d", ready, len(forHost))
	}
}
