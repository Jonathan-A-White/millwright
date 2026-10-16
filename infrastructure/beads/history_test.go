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

// historyStandIn writes a bd that answers the history's queries the way a
// served Dolt database does — a long history, its first commit, the one
// remote-tracking branch the Laptop's backup left — records every call it gets
// one per line, and says commitSays and exits commitExit on the squash's own
// commit.
func historyStandIn(t *testing.T, commitSays string, commitExit int) (gateway *beads.Gateway, calls string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	calls = filepath.Join(dir, "calls")
	path := filepath.Join(dir, "bd-stand-in")
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >> %q
case "$*" in
  *"AS commits"*) echo '[{"commits": 96100}]' ;;
  *"ORDER BY date ASC LIMIT 1"*) echo '[{"commit_hash": "o2lrqserg17hatn16t1act9uiiron8uj"}]' ;;
  *"FROM dolt_remote_branches"*) echo '[{"name": "remotes/origin/main"}]' ;;
  *"DOLT_COMMIT"*) echo %q; exit %d ;;
esac
exit 0
`, calls, commitSays, commitExit)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(dir, beads.WithProgram(path)), calls
}

func readCalls(t *testing.T, calls string) []string {
	t.Helper()
	said, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("reading what was called: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(said)), "\n")
}

// indexOf is the first call holding every one of parts, or -1.
func indexOf(calls []string, parts ...string) int {
	for i, call := range calls {
		all := true
		for _, part := range parts {
			if !strings.Contains(call, part) {
				all = false
				break
			}
		}
		if all {
			return i
		}
	}
	return -1
}

func TestHistoryCommitsCountsTheDatabasesCommits(t *testing.T) {
	gateway, _ := historyStandIn(t, "", 0)

	commits, err := gateway.HistoryCommits(context.Background())
	if err != nil {
		t.Fatalf("counting the history: %v", err)
	}
	if commits != 96_100 {
		t.Fatalf("expected 96100 commits, got %d", commits)
	}
}

// The squash is the recipe tried on a copy of the Laptop's database while three
// writers wrote to it (mw-gq6.455): the branch is soft-reset onto its first
// commit and committed, which never touches the working set, so a write that
// lands meanwhile is kept, unlike bd flatten's hard reset; the remote-tracking
// branch is dropped, since it alone kept 43,506 old commits alive; and only a
// full collection reaches history an earlier collection moved to the old
// generation.
func TestSquashHistoryFoldsTheHistoryOntoItsFirstCommitAndCollectsEveryGeneration(t *testing.T) {
	gateway, calls := historyStandIn(t, `[{"hash": "n3w"}]`, 0)

	if err := gateway.SquashHistory(context.Background()); err != nil {
		t.Fatalf("squashing: %v", err)
	}
	ran := readCalls(t, calls)
	reset := indexOf(ran, "sql", "DOLT_RESET('--soft', 'o2lrqserg17hatn16t1act9uiiron8uj')")
	commit := indexOf(ran, "sql", "DOLT_COMMIT('-Am'")
	drop := indexOf(ran, "sql", "DOLT_BRANCH('-D', '-r', 'remotes/origin/main')")
	collect := indexOf(ran, "gc", "--skip-decay", "--force", "--full")
	if reset < 0 || commit < 0 || drop < 0 || collect < 0 {
		t.Fatalf("expected a soft reset, a commit, the remote-tracking branch dropped and a full gc, got:\n%s", strings.Join(ran, "\n"))
	}
	if !(reset < commit && commit < drop && drop < collect) {
		t.Fatalf("expected reset, commit, drop, collect in that order, got:\n%s", strings.Join(ran, "\n"))
	}
	if hard := indexOf(ran, "--hard"); hard >= 0 {
		t.Fatalf("expected no hard reset, which would lose a write landing meanwhile, got %q", ran[hard])
	}
}

// A bd write that lands between the reset and the squash's own commit commits
// the whole working set onto the first commit itself, and the squash's commit
// then has nothing left to commit: that is the squash done, not a failure.
func TestSquashHistoryTakesNothingToCommitAsAWriterHavingFoldedItIn(t *testing.T) {
	gateway, calls := historyStandIn(t, `{"error": "exec error: Error 1105 (HY000): nothing to commit", "schema_version": 1}`, 1)

	if err := gateway.SquashHistory(context.Background()); err != nil {
		t.Fatalf("expected nothing to commit to be the squash done, got %v", err)
	}
	if collect := indexOf(readCalls(t, calls), "gc", "--full"); collect < 0 {
		t.Fatal("expected the full collection to run after it")
	}
}

func TestSquashHistoryThatCannotCommitSaysSoAndCollectsNothing(t *testing.T) {
	gateway, calls := historyStandIn(t, `{"error": "exec error: connection refused", "schema_version": 1}`, 1)

	err := gateway.SquashHistory(context.Background())
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected the commit's failure, got %v", err)
	}
	if collect := indexOf(readCalls(t, calls), "gc", "--full"); collect >= 0 {
		t.Fatal("expected no collection after a squash that did not commit")
	}
}
