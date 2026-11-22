package application_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1620: a session that ran on another host pushed its result.json to the
// vault; this host stops 'no-result' on it until the vault is pulled. The
// close-out pulls once and reads again before it stops.

// pullingFiles is a vault clone whose Pull brings in the result file the other
// host pushed (when result is not empty) and counts the pulls.
type pullingFiles struct {
	apptest.FakeVaultFiles
	file, result string
	pulls        int
}

func (p *pullingFiles) Pull(ctx context.Context) (int, error) {
	p.pulls++
	if p.result != "" {
		if err := os.MkdirAll(filepath.Dir(p.file), 0o755); err != nil {
			return 0, err
		}
		if err := os.WriteFile(p.file, []byte(p.result), 0o644); err != nil {
			return 0, err
		}
	}
	return p.FakeVaultFiles.Pull(ctx)
}

func aNoResultCloseOut(t *testing.T, pulled string) (application.NextReport, *pullingFiles) {
	t.Helper()
	dispatch, tracker, _, _, vaultDir := aFactory(t)
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	claimedStory(t, tracker, "mw-gq6.9", 1)
	files := &pullingFiles{
		file:   filepath.Join(vaultDir, application.RunsDir, "mw-gq6.9", application.ResultFileName),
		result: pulled,
	}
	lands := &fakeRetryLanding{}
	next := application.Next{
		Tracker: tracker, Vault: dispatch.Boot.Vault, Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Files: files, Memory: tracker, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop", Rigs: dispatch.Rigs,
		Now: func() time.Time { return now },
		Out: io.Discard, Err: io.Discard,
	}
	report, _ := next.Run(context.Background(), "mw-gq6.9")
	return report, files
}

func TestNoResultInThisVaultIsLookedForAgainAfterOnePull(t *testing.T) {
	report, files := aNoResultCloseOut(t, "")
	if report.Reason != application.ReasonNoResult {
		t.Fatalf("expected %s when the pull brings nothing, got %q: %s", application.ReasonNoResult, report.Reason, report.Why)
	}
	if files.pulls != 1 {
		t.Errorf("expected the vault pulled once before the stop, got %d pulls", files.pulls)
	}
}

func TestNoResultIsNotAStopWhenTheResultComesInWithThePull(t *testing.T) {
	report, files := aNoResultCloseOut(t, "{\"is_error\":false,\"result\":\"done\"}")
	if files.pulls != 1 {
		t.Errorf("expected one pull, got %d", files.pulls)
	}
	if report.Reason == application.ReasonNoResult || strings.Contains(report.Why, "left no result") {
		t.Fatalf("expected the result pulled in to be read, not stopped as no-result: %q: %s", report.Reason, report.Why)
	}
}
