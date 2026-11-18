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

// A host with no beads server, or whose kept connection failed, reads `mw queue`
// by bd, and every bd opens its own connections to the server: about 7 bd calls,
// 21 connections, where the trace of the command is to name 20 at most
// (mw-gq6.1442). The ready, the blocked, the children's counts and what is
// running are read from `bd ready`, `bd blocked` and one `bd list --all`.
func TestQueueReadsByAtMostThreeBdsWhenThereIsNoKeptConnection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	json := func(id, status, kind, parent, assignee string) string {
		return fmt.Sprintf(`{"id":"%s","title":"t","status":"%s","issue_type":"%s","parent":"%s","assignee":"%s"}`,
			id, status, kind, parent, assignee)
	}
	epic := epic("mw-e1", "desktop")
	ready := json("mw-gq6.1", "open", "task", "mw-e1", "")
	parent := json("mw-gq6.2", "open", "task", "mw-e1", "")
	child := json("mw-gq6.3", "open", "task", "mw-gq6.2", "")
	waiting := json("mw-gq6.4", "open", "task", "mw-e1", "")
	running := json("mw-gq6.5", "in_progress", "task", "mw-e1", "builder@desktop")
	gone := json("mw-gq6.6", "closed", "task", "mw-gq6.2", "")
	all := strings.Join([]string{epic, ready, parent, child, waiting, running, gone}, ",")

	dir := t.TempDir()
	log := filepath.Join(dir, "asked.log")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %s
case " $* " in
  *" ready "*) printf '%%s' '[%s,%s]' ;;
  *" blocked "*) printf '%%s' '[{"id":"mw-gq6.4","title":"t","status":"open","issue_type":"task","blocked_by":["mw-gq6.1"]}]' ;;
  *" list "*" --all "*) printf '%%s' '[%s]' ;;
  *" list "*" in_progress "*) printf '%%s' '[%s]' ;;
  *" list "*) printf '%%s' '[%s,%s,%s,%s,%s,%s]' ;;
  *) exit 1 ;;
esac
`, log, ready, parent, all, running, epic, ready, parent, child, waiting, running)
	path := filepath.Join(dir, "bd-queue")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	gateway := beads.New(t.TempDir(), beads.WithProgram(path), beads.WithRunCache())
	ctx := context.Background()

	readyOnHost, err := gateway.ReadyForHost(ctx, "desktop")
	if err != nil {
		t.Fatalf("reading what is ready: %v", err)
	}
	children := map[string]int{}
	for _, detail := range readyOnHost {
		children[detail.Story.ID] = detail.Children
	}
	if len(readyOnHost) != 2 || children["mw-gq6.1"] != 0 || children["mw-gq6.2"] != 1 {
		t.Errorf("expected mw-gq6.1 with no child and mw-gq6.2 with one, got %v", children)
	}
	blocked, err := gateway.BlockedForHost(ctx, "desktop")
	if err != nil || len(blocked) != 1 || blocked[0].Story.ID != "mw-gq6.4" {
		t.Fatalf("expected mw-gq6.4 blocked, got %+v (%v)", blocked, err)
	}
	here, err := gateway.RunningStories(ctx, "desktop")
	if err != nil || len(here) != 1 || here[0].Story.ID != "mw-gq6.5" {
		t.Fatalf("expected mw-gq6.5 running here, got %+v (%v)", here, err)
	}
	away, err := gateway.RunningElsewhere(ctx, "desktop")
	if err != nil || len(away) != 0 {
		t.Fatalf("expected nothing running elsewhere, got %+v (%v)", away, err)
	}

	calls := asked(t, log)
	if len(calls) > 3 {
		t.Errorf("expected at most 3 bd calls (ready, blocked, one list of every bead), got %d: %v", len(calls), calls)
	}
}
