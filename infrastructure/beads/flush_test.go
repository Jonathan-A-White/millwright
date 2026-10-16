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

// flushStandIn writes a bd that records every call one per line, and answers
// `dolt commit` with commitSays and commitExit. Every other call exits 0.
func flushStandIn(t *testing.T, commitSays string, commitExit int) (gateway *beads.Gateway, calls string) {
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
  *"dolt commit"*) echo %q >&2; exit %d ;;
  *"AS commits"*) echo '[{"commits": 96100}]' ;;
  *"ORDER BY date ASC LIMIT 1"*) echo '[{"commit_hash": "o2lrqserg17hatn16t1act9uiiron8uj"}]' ;;
  *"FROM dolt_remote_branches"*) echo '[]' ;;
  *"config get"*) echo Batch ;;
esac
exit 0
`, calls, commitSays, commitExit)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(dir, beads.WithProgram(path)), calls
}

func TestFlushCommitsTheWorkingSetWithAMessageNamingTheTime(t *testing.T) {
	gateway, calls := flushStandIn(t, "", 0)
	at := time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)

	committed, err := gateway.Flush(context.Background(), at)

	if err != nil || !committed {
		t.Fatalf("expected a commit, got committed=%v err=%v", committed, err)
	}
	if got := readCalls(t, calls); len(got) != 1 || !strings.HasPrefix(got[0], "-C ") || !strings.HasSuffix(got[0], "dolt commit -m mw flush 2026-10-16T12:30:00Z") {
		t.Fatalf("expected one bd dolt commit with the time in its message, got %q", got)
	}
}

func TestAFlushOfACleanWorkingSetIsAPassNotAFailure(t *testing.T) {
	for _, said := range []string{"nothing to commit", "Error: no changes to commit"} {
		gateway, _ := flushStandIn(t, said, 1)

		committed, err := gateway.Flush(context.Background(), time.Now())

		if err != nil || committed {
			t.Fatalf("bd saying %q: expected a pass that committed nothing, got committed=%v err=%v", said, committed, err)
		}
	}
}

func TestAFlushThatFailsReportsWhatBdSaid(t *testing.T) {
	gateway, _ := flushStandIn(t, "dolt server unreachable", 1)

	if _, err := gateway.Flush(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "dolt server unreachable") {
		t.Fatalf("expected the failure with what bd said, got %v", err)
	}
}

func TestSyncFlushesBeforeItPullsAndPushes(t *testing.T) {
	gateway, calls := flushStandIn(t, "", 0)

	if err := gateway.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	got := readCalls(t, calls)
	flush, sync := indexOf(got, "dolt commit"), indexOf(got, " sync")
	if flush < 0 || sync < 0 || flush > sync {
		t.Fatalf("expected a flush and then bd sync, got %q", got)
	}
}

func TestSyncOfACleanWorkingSetStillSyncs(t *testing.T) {
	gateway, calls := flushStandIn(t, "nothing to commit", 1)

	if err := gateway.Sync(context.Background()); err != nil {
		t.Fatalf("a clean working set is not a failed sync: %v", err)
	}
	if indexOf(readCalls(t, calls), " sync") < 0 {
		t.Fatal("bd sync was not run")
	}
}

func TestSyncThatCannotFlushDoesNotSync(t *testing.T) {
	gateway, calls := flushStandIn(t, "dolt server unreachable", 1)

	err := gateway.Sync(context.Background())

	if err == nil || !strings.Contains(err.Error(), "dolt server unreachable") {
		t.Fatalf("expected the flush's failure, got %v", err)
	}
	if indexOf(readCalls(t, calls), " sync") >= 0 {
		t.Fatal("bd sync ran over an unflushed working set")
	}
}

func TestSquashFlushesBeforeItFoldsTheHistory(t *testing.T) {
	gateway, calls := flushStandIn(t, "", 0)

	if err := gateway.SquashHistory(context.Background()); err != nil {
		t.Fatalf("squash: %v", err)
	}

	got := readCalls(t, calls)
	flush, reset := indexOf(got, "dolt commit"), indexOf(got, "DOLT_RESET")
	if flush < 0 || reset < 0 || flush > reset {
		t.Fatalf("expected a flush and then the squash, got %q", got)
	}
}

func TestAutoCommitReadsBdsAnswerInLowerCase(t *testing.T) {
	gateway, calls := flushStandIn(t, "", 0)

	got, err := gateway.AutoCommit(context.Background())

	if err != nil || got != "batch" {
		t.Fatalf("expected batch, got %q (%v)", got, err)
	}
	if lines := readCalls(t, calls); !strings.HasSuffix(lines[0], "config get dolt.auto-commit") {
		t.Fatalf("expected bd config get dolt.auto-commit, got %q", lines)
	}
}
