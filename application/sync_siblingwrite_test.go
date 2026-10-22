package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// aPullRefusedForUncommittedChanges is git's refusal of a pull --rebase while a
// sibling mw command is between writing a vault file and committing it, as the
// vault adapter hands it up (mw-gq6.706).
func aPullRefusedForUncommittedChanges() error {
	return fmt.Errorf("%w: git pull --rebase: error: cannot pull with rebase: You have unstaged changes. error: Please commit or stash them.",
		application.ErrVaultBeingWritten)
}

func TestSyncWaitsForASiblingsCommitAndPullsAgain(t *testing.T) {
	sync, files, _ := syncing(t)
	files.PullErr, files.PullErrFor = aPullRefusedForUncommittedChanges(), 1
	var waited []time.Duration
	sync.Sleep = func(_ context.Context, d time.Duration) error {
		waited = append(waited, d)
		files.Dirty = nil // the sibling's close-out commits during the wait
		return nil
	}
	files.Incoming = 3

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected a pull refused while a sibling commits to be tried again, got %v", err)
	}
	if report.Pulled != 3 || files.PullTries() != 2 {
		t.Fatalf("expected the second pull to bring 3 in, got %d after %d tries", report.Pulled, files.PullTries())
	}
	if len(waited) != 1 {
		t.Fatalf("expected one wait before the second pull, got %v", waited)
	}
}

func TestSyncCallsAPullRefusedOnAFileNobodyCommitsBlocked(t *testing.T) {
	sync, files, _ := syncing(t)
	files.PullErr = aPullRefusedForUncommittedChanges()
	// The file is still dirty when the sync looks again after waiting.
	sync.Sleep = func(context.Context, time.Duration) error {
		files.Dirty = []string{".mayor-up.log"}
		return nil
	}

	_, err := sync.Run(context.Background())
	blocked, stopped := application.Blocked(err)
	if !stopped {
		t.Fatalf("expected a vault still dirty after the waits to be blocked as today, got %T: %v", err, err)
	}
	if len(blocked.Files) != 1 || blocked.Files[0] != ".mayor-up.log" {
		t.Fatalf("expected the blocked vault to name the file, got %v", blocked.Files)
	}
	if got := application.ExitStatus(err); got != application.VaultBlockedExit {
		t.Fatalf("expected the status of a blocked vault, got %d", got)
	}
}

func TestSyncGivesUpOnAPullRefusedEveryTimeWithTheVaultClean(t *testing.T) {
	sync, files, _ := syncing(t)
	files.PullErr = aPullRefusedForUncommittedChanges()

	_, err := sync.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Please commit or stash them") {
		t.Fatalf("expected the refusal to be said when waiting did not cure it, got %v", err)
	}
	if files.PullTries() < 2 {
		t.Fatalf("expected the pull to be tried again before giving up, got %d tries", files.PullTries())
	}
}

func TestSyncDoesNotRetryAPullRefusedForAnythingElse(t *testing.T) {
	sync, files, _ := syncing(t)
	files.PullErr = fmt.Errorf("the remote went away")

	if _, err := sync.Run(context.Background()); err == nil {
		t.Fatal("expected the pull to fail")
	}
	if files.PullTries() != 1 {
		t.Fatalf("expected no second try, got %d", files.PullTries())
	}
}

// A dispatch whose sync meets a sibling's close-out between its write and its
// commit does not exit with an error: it waits, pulls, and the tick claims.
func TestDispatchClaimsAfterWaitingForASiblingsCloseOutCommit(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	syncing, files, _ := syncing(t)
	files.PullErr, files.PullErrFor = aPullRefusedForUncommittedChanges(), 1
	syncing.Tracker = tracker
	syncing.Host = "vps"
	syncing.Sleep = func(context.Context, time.Duration) error { return nil }
	dispatch.Sync = syncing

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the tick to go on after the sibling committed, got %v", err)
	}
	if !report.Synced || files.PullTries() != 2 {
		t.Fatalf("expected a synced tick after a second pull, got synced=%v tries=%d", report.Synced, files.PullTries())
	}
}
