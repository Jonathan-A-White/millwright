package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// batchTakenAt is the moment a running batch's mark is read at in these tests.
var batchTakenAt = time.Date(2026, 11, 26, 19, 58, 0, 0, time.UTC)

// takenByBatch730 marks story id as the running batch 730 marks the stories it
// has taken (holdQueue): its host, when it began, and a lease still holding.
func takenByBatch730(t *testing.T, tracker *apptest.FakeTracker, id string, until time.Time) {
	t.Helper()
	mark := application.ClosingOut{By: "laptop", Since: batchTakenAt.Add(-time.Minute), Until: until, Batch: 730}
	mustDo(t, tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.ClosingOutField: mark.String()}))
}

// mw-gq6.1761: mw land --set-aside accepted a story a running batch had already
// taken (batch 730 of lampas, mw-fnvjof.17.336), and the batch landed it.
func TestSetAsideRefusesAStoryARunningBatchHasTakenAndChangesNothing(t *testing.T) {
	_, tracker, _ := aBatchRig(t)
	ctx := context.Background()
	takenByBatch730(t, tracker, "mw-gq6.4", batchTakenAt.Add(4*time.Minute))
	before, err := tracker.ShowStory(ctx, "mw-gq6.4")
	mustDo(t, err)

	set := application.SetAside{Tracker: tracker, Host: "desktop", Out: io.Discard, Now: func() time.Time { return batchTakenAt }}
	err = set.Run(ctx, "millwright", "mw-gq6.4", "its spec fails on main")
	if err == nil {
		t.Fatal("expected the set-aside of a story a running batch has taken to be refused")
	}
	for _, want := range []string{"mw-gq6.4", "batch 730", "laptop"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to name %q, got %q", want, err)
		}
	}
	after, err := tracker.ShowStory(ctx, "mw-gq6.4")
	mustDo(t, err)
	if after.Status != before.Status || after.Assignee != before.Assignee || strings.Join(after.Labels, ",") != strings.Join(before.Labels, ",") {
		t.Errorf("expected the story unchanged, was %+v now %+v", before, after)
	}
	if comments := tracker.Comments("mw-gq6.4"); len(comments) != 0 {
		t.Errorf("expected no comment, got %q", comments)
	}
}

// A mark whose lease has lapsed is a batch that died: the story is set aside.
func TestSetAsideTakesAStoryWhoseBatchMarkHasLapsed(t *testing.T) {
	_, tracker, _ := aBatchRig(t)
	ctx := context.Background()
	takenByBatch730(t, tracker, "mw-gq6.4", batchTakenAt.Add(-time.Second))

	set := application.SetAside{Tracker: tracker, Host: "desktop", Out: io.Discard, Now: func() time.Time { return batchTakenAt }}
	mustDo(t, set.Run(ctx, "millwright", "mw-gq6.4", "its spec fails on main"))
	detail, err := tracker.ShowStory(ctx, "mw-gq6.4")
	mustDo(t, err)
	if detail.Status != application.StatusHeld {
		t.Errorf("expected the story held, got %q", detail.Status)
	}
}

// mw-gq6.1761: mw retry gave the claim back of a story the Mayor had set aside
// after batch 730 took it, and bundled its branch and removed its worktree.
func TestRetryRefusesAHeldStoryARunningBatchHasTakenForceOrNot(t *testing.T) {
	for _, tc := range []struct {
		name        string
		force, hold bool
	}{{name: "plain"}, {name: "forced", force: true}, {name: "held", hold: true}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tracker := apptest.NewFakeTracker()
			aQueuedRetryStory(t, tracker, "mw-gq6.1", time.Date(2026, 11, 26, 19, 0, 0, 0, time.UTC))
			mustDo(t, tracker.HoldClaimHeldBy(ctx, "mw-gq6.1", "", time.Time{}))
			takenByBatch730(t, tracker, "mw-gq6.1", batchTakenAt.Add(4*time.Minute))
			before, err := tracker.ShowStory(ctx, "mw-gq6.1")
			mustDo(t, err)

			landing := &fakeRetryLanding{AheadCount: 2, BundleSHA: "bbb", OriginTip: "4656ca5d"}
			var out strings.Builder
			retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
			retry.Now = func() time.Time { return batchTakenAt }
			retry.Force, retry.Hold = tc.force, tc.hold

			report, err := retry.Run(ctx, "mw-gq6.1")
			if err == nil || !report.Refused {
				t.Fatalf("expected the retry to be refused, got report %+v, err %v", report, err)
			}
			for _, want := range []string{"batch 730", "laptop"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected the refusal to name %q, got %q", want, err)
				}
			}
			after, err := tracker.ShowStory(ctx, "mw-gq6.1")
			mustDo(t, err)
			if after.Status != before.Status || after.Assignee != before.Assignee || strings.Join(after.Labels, ",") != strings.Join(before.Labels, ",") || after.Attempts != before.Attempts {
				t.Errorf("expected the story unchanged, was %+v now %+v", before, after)
			}
			if landing.bundled || landing.removed || landing.branchDeleted {
				t.Errorf("expected nothing bundled, removed or deleted, got %+v", landing)
			}
		})
	}
}

// mw-gq6.1761: mw land --dry-run said of a story batch 730 had taken that it was
// "queued, but it is held, so it is left out", and nothing said the batch had it.
func TestLandDryRunNamesAQueuedStoryARunningBatchHoldsAsInThatBatch(t *testing.T) {
	land, tracker, out := aBatchRig(t)
	ctx := context.Background()
	set := application.SetAside{Tracker: tracker, Host: "desktop", Out: io.Discard, Now: func() time.Time { return batchTakenAt.Add(-time.Hour) }}
	mustDo(t, set.Run(ctx, "millwright", "mw-gq6.4", "its spec fails on main"))
	takenByBatch730(t, tracker, "mw-gq6.4", batchTakenAt.Add(4*time.Minute))
	land.Next.Now = func() time.Time { return batchTakenAt }

	_, err := land.Run(ctx, "millwright")
	mustDo(t, err)
	said := out.String()
	if !strings.Contains(said, "mw-gq6.4 is in batch 730") {
		t.Errorf("expected the dry run to say mw-gq6.4 is in batch 730, got:\n%s", said)
	}
	if strings.Contains(said, "mw-gq6.4 is queued, but it is held, so it is left out") {
		t.Errorf("expected no 'held, so it is left out' for a story a running batch holds, got:\n%s", said)
	}
}

// A batch's mark carries its number, and a close-out's own mark, which has none,
// is still read as it was.
func TestAClosingOutMarkKeepsTheNumberOfTheBatchThatMadeIt(t *testing.T) {
	since := time.Date(2026, 11, 26, 19, 57, 8, 0, time.UTC)
	for _, mark := range []application.ClosingOut{
		{By: "laptop", Since: since, Until: since.Add(5 * time.Minute), Batch: 730},
		{By: "laptop", Since: since, Until: since.Add(5 * time.Minute)},
	} {
		got, ok := application.ParseClosingOut(mark.String())
		if !ok || got != mark {
			t.Errorf("expected %q to be read back as %+v, got %+v (%v)", mark.String(), mark, got, ok)
		}
	}
	if _, ok := application.ParseClosingOut("laptop 2026-11-26T19:57:08Z 2026-11-26T20:02:08Z batch=x"); ok {
		t.Error("expected a batch word that is no number to be no mark")
	}
}
