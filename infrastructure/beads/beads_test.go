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

// holdStandIn is a stand-in bd that keeps one story's status and assignee in
// files, and answers bd show and bd update as bd 1.3.0 does: an update that
// carries --if-assignee writes nothing and exits 13 when the assignee is not
// the one named (an empty value names nobody). It returns the Gateway, a function that
// claims the story as a session on another host would (between a caller's read
// and its hold), and a function that reads the status back.
func holdStandIn(t *testing.T, status, assignee string) (g *beads.Gateway, claim func(), statusNow func() string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "status")
	assigneeFile := filepath.Join(dir, "assignee")
	write := func(path, text string) {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(statusFile, status)
	write(assigneeFile, assignee)
	script := fmt.Sprintf(`#!/bin/sh
status_file=%q
assignee_file=%q
sub=""
for a in "$@"; do
  case "$a" in
    show|update) sub="$a"; break;;
  esac
done
case "$sub" in
show)
  printf '[{"id": "t-1", "title": "A story", "status": "%%s", "assignee": "%%s"}]\n' "$(cat "$status_file")" "$(cat "$assignee_file")"
  exit 0;;
update)
  new_status=""
  guard=""
  guarded=no
  while [ $# -gt 0 ]; do
    case "$1" in
      --status) new_status="$2"; shift;;
      --if-assignee) guard="$2"; guarded=yes; shift;;
    esac
    shift
  done
  if [ "$guarded" = yes ] && [ "$(cat "$assignee_file")" != "$guard" ]; then
    echo "update refused: assignee is not $guard" >&2
    exit 13
  fi
  [ -n "$new_status" ] && printf '%%s' "$new_status" > "$status_file"
  exit 0;;
esac
exit 0
`, statusFile, assigneeFile)
	path := filepath.Join(dir, "bd-hold-stand-in")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}
	return beads.New(dir, beads.WithProgram(path), beads.WithActor("mw@vps")),
		func() { write(statusFile, "in_progress"); write(assigneeFile, "mw@other") },
		func() string { return read(statusFile) }
}

// A story claimed after the caller read it open is not held: the hold is
// guarded on there being no assignee, so bd writes nothing and the Builder's
// run stays in progress (mw-gq6.1150).
func TestHoldStoryIsRefusedForAStoryClaimedSinceTheCallerRead(t *testing.T) {
	gateway, claim, status := holdStandIn(t, "open", "")
	claim()

	err := gateway.HoldStory(context.Background(), "t-1")
	if err == nil {
		t.Fatal("expected the hold of a claimed story to be refused")
	}
	if !strings.Contains(err.Error(), "mw@other") {
		t.Errorf("expected the refusal to name who holds the claim, got %v", err)
	}
	if got := status(); got != "in_progress" {
		t.Errorf("expected the story still in_progress, got %q", got)
	}
}

// A story nobody has claimed, open or already held, is held as before.
func TestHoldStoryHoldsAnUnclaimedStory(t *testing.T) {
	for _, was := range []string{"open", "deferred"} {
		gateway, _, status := holdStandIn(t, was, "")
		if err := gateway.HoldStory(context.Background(), "t-1"); err != nil {
			t.Fatalf("holding a %s unclaimed story: %v", was, err)
		}
		if got := status(); got != "deferred" {
			t.Errorf("a %s unclaimed story: expected deferred, got %q", was, got)
		}
	}
}

// A failure that is not a claim is still the failure bd gave.
func TestHoldStoryKeepsAnyOtherFailureOfBd(t *testing.T) {
	gateway, _ := leaseStandIn(t, map[string]string{"update": "fail:Error: no issue found matching \"t-1\""})
	err := gateway.HoldStory(context.Background(), "t-1")
	if err == nil || !strings.Contains(err.Error(), "no issue found") {
		t.Fatalf("expected bd's own failure, got %v", err)
	}
}
