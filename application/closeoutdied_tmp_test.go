package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1677: the result is written to result-<n>.json.tmp and renamed when the
// harness has exited. A pane killed between the two leaves only the .tmp; a
// whole one is a session that ended, so a dead pane over it is a close-out that
// died (named tooling, not refunded), not a dead session (refunded). One cut off
// mid-JSON is a session that died, as before.

const wholeResult = `{"type":"result","subtype":"success","is_error":false,"num_turns":45,"duration_ms":900000,` +
	`"session_id":"s-tmp","total_cost_usd":1.27,"usage":{"input_tokens":80,"output_tokens":26074}}`

// reclaimedWithOnlyATmp dispatches over a dead pane whose run holds only
// result.json.tmp with the given text, and returns the report and the story's
// detail afterwards.
func reclaimedWithOnlyATmp(t *testing.T, tmp string) (application.DispatchReport, application.StoryDetail) {
	t.Helper()
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	claimedStory(t, tracker, "mw-gq6.9", 1)
	dir := filepath.Join(vaultDir, application.RunsDir, "mw-gq6.9")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, application.ResultFileNameForAttempt(1)+".tmp"), []byte(tmp), 0o644); err != nil {
		t.Fatal(err)
	}
	runner.Exit(startOldSession(t, runner, "mw-gq6.9"), 1)
	if err := tracker.SetLeaseExpires("mw-gq6.9", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatal(err)
	}
	return report, detail
}

func TestAWholeResultTmpWithNoResultIsACloseOutThatDiedNotADeadSession(t *testing.T) {
	report, detail := reclaimedWithOnlyATmp(t, wholeResult)
	if len(report.Reclaimed) != 1 || !report.Reclaimed[0].CloseOutDied {
		t.Fatalf("expected a reclaim of a close-out that died, got %+v", report.Reclaimed)
	}
	if detail.AttemptsRefunded != 0 {
		t.Errorf("expected no refund, got %d", detail.AttemptsRefunded)
	}
}

func TestAResultTmpCutOffMidJSONIsADeadSessionAsBefore(t *testing.T) {
	cut := wholeResult[:len(wholeResult)/2]
	if !strings.HasPrefix(wholeResult, cut) {
		t.Fatal("test setup")
	}
	report, detail := reclaimedWithOnlyATmp(t, cut)
	if len(report.Reclaimed) != 1 || report.Reclaimed[0].CloseOutDied {
		t.Fatalf("expected a reclaim of a dead session, got %+v", report.Reclaimed)
	}
	if detail.AttemptsRefunded != 1 {
		t.Errorf("expected the attempt refunded, got %d refunds", detail.AttemptsRefunded)
	}
}
