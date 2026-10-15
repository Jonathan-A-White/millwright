package application_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw land (plans/0117, Land Rush 3): which stories a batch takes and in what
// order. The landing itself, on a real rig and origin, is
// features/batch_land.feature.

func TestSortOldestQueuedPutsTheOldestFirstAndUnrecordedTimesBeforeAll(t *testing.T) {
	at := func(minute int) time.Time { return time.Date(2026, 10, 15, 12, minute, 0, 0, time.UTC) }
	queue := []application.BatchStory{
		{ID: "mw-c", QueuedAt: at(30)},
		{ID: "mw-b", QueuedAt: at(5)},
		{ID: "mw-z"}, // queued by an mw that recorded no time
		{ID: "mw-a", QueuedAt: at(30)},
	}
	application.SortOldestQueued(queue)
	var got []string
	for _, s := range queue {
		got = append(got, s.ID)
	}
	if want := []string{"mw-z", "mw-b", "mw-a", "mw-c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the order %v, got %v", want, got)
	}
}

func TestConflictLinesAreGitsOwnLines(t *testing.T) {
	said := "git merge --no-edit origin/mw/mw-1 in /tmp/x: exit status 1: Auto-merging a.md\n" +
		"CONFLICT (content): Merge conflict in a.md\nCONFLICT (add/add): Merge conflict in b.md\n" +
		"Automatic merge failed; fix conflicts and then commit the result."
	got := application.ConflictLines(said)
	want := []string{"CONFLICT (content): Merge conflict in a.md", "CONFLICT (add/add): Merge conflict in b.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if got := application.ConflictLines("the merge has conflicts\nmore"); !reflect.DeepEqual(got, []string{"the merge has conflicts"}) {
		t.Fatalf("expected the first line when git named no conflict, got %q", got)
	}
}

// dryLanding answers what a dry run asks of the rig: every branch at the
// origin is there, two commits ahead.
type dryLanding struct {
	application.Landing
}

func (dryLanding) RefHead(_ context.Context, _, ref string) (string, error) {
	return "0123456789abcdef-" + ref, nil
}

func (dryLanding) Ahead(context.Context, string, string, string) (int, error) { return 2, nil }

type fetchOnly struct {
	application.Worktrees
	fetched int
}

func (f *fetchOnly) Fetch(context.Context, string) error { f.fetched++; return nil }

// aBatchRig is a tracker with stories of two rigs, some queued at the given
// minutes past noon, and the BatchLand of millwright over it, in a dry run.
func aBatchRig(t *testing.T) (application.BatchLand, *apptest.FakeTracker, *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps"})
	tracker.AddEpic("lp-1", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps"})
	for _, s := range []struct {
		epic, id string
		minute   int // when it was queued; negative is claimed and not queued
	}{
		{"mw-gq6", "mw-gq6.1", 40},
		{"mw-gq6", "mw-gq6.2", 10},
		{"mw-gq6", "mw-gq6.3", -1},
		{"lp-1", "lp-1.1", 1},
		{"mw-gq6", "mw-gq6.4", 25},
	} {
		tracker.AddStory(s.epic, domain.Story{ID: s.id, Title: "The story " + s.id})
		mustDo(t, tracker.ClaimStory(ctx, s.id))
		if s.minute < 0 {
			continue
		}
		queue(t, tracker, s.id)
		mustDo(t, tracker.SetStoryMetadata(ctx, s.id, map[string]string{
			application.QueuedAtField: time.Date(2026, 10, 15, 12, s.minute, 0, 0, time.UTC).Format(time.RFC3339),
		}))
	}
	var out bytes.Buffer
	land := application.BatchLand{
		Next: application.Next{
			Tracker: tracker, Vault: newFakeVault(), Worktrees: &fetchOnly{}, Landing: dryLanding{},
			Checks: failingChecks{t}, Slot: noSlot{t}, Host: "vps", Seat: "builder",
			Rigs: map[string]string{"millwright": "/rigs/millwright", "lampas": "/rigs/lampas"},
			Out:  &out,
		},
		Records: noRecords{t},
		DryRun:  true,
	}
	return land, tracker, &out
}

type failingChecks struct{ t *testing.T }

func (f failingChecks) Run(context.Context, string, string) (application.Checked, error) {
	f.t.Fatalf("a dry run ran the rig's tests")
	return application.Checked{}, nil
}

type noSlot struct{ t *testing.T }

func (s noSlot) Take(context.Context, string, string) (application.Holding, error) {
	s.t.Fatalf("a dry run took the merge slot")
	return nil, nil
}

type noRecords struct{ t *testing.T }

func (r noRecords) NewBatch(context.Context, string) (int, error) {
	r.t.Fatalf("a dry run began a batch's records")
	return 0, nil
}

func (r noRecords) PutBatchFile(context.Context, string, int, string, string) (string, error) {
	r.t.Fatalf("a dry run wrote a batch's records")
	return "", nil
}

func TestADryRunTakesOnlyTheRigsQueuedStoriesOldestFirstAndChangesNothing(t *testing.T) {
	land, tracker, out := aBatchRig(t)
	asked := len(tracker.Asked())

	report, err := land.Run(context.Background(), "millwright")
	if err != nil {
		t.Fatalf("a dry run: %v", err)
	}
	var got []string
	for _, s := range report.Queue {
		got = append(got, s.ID)
	}
	if want := []string{"mw-gq6.2", "mw-gq6.4", "mw-gq6.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the batch %v, oldest queued first and none of another rig or not queued, got %v", want, got)
	}
	if report.Queue[0].Branch != "origin/mw/mw-gq6.2" || report.Queue[0].Commits != 2 {
		t.Fatalf("expected each story's branch as the origin has it, with its commits, got %+v", report.Queue[0])
	}
	for _, call := range tracker.Asked()[asked:] {
		if call != "WorkInHand" {
			t.Fatalf("expected a dry run only to read the queue, got %v", tracker.Asked()[asked:])
		}
	}
	printed := out.String()
	if !strings.Contains(printed, "dry run") || strings.Index(printed, "mw-gq6.2") > strings.Index(printed, "mw-gq6.1") {
		t.Fatalf("expected the dry run printed in merge order, got:\n%s", printed)
	}
}

func TestANothingQueuedRigLandsNothing(t *testing.T) {
	land, _, out := aBatchRig(t)
	land.DryRun = false
	land.Next.Rigs["quiet"] = "/rigs/quiet"

	report, err := land.Run(context.Background(), "quiet")
	if err != nil {
		t.Fatalf("landing an empty queue: %v", err)
	}
	if len(report.Queue) != 0 || report.Landed || report.Held != "" {
		t.Fatalf("expected nothing taken, landed or held, got %+v", report)
	}
	if !strings.Contains(out.String(), "nothing is queued") {
		t.Fatalf("expected it to say nothing is queued, got:\n%s", out.String())
	}
}

func TestLandingARigThisHostHasNotCheckedOutIsRefused(t *testing.T) {
	land, _, _ := aBatchRig(t)
	if _, err := land.Run(context.Background(), "elsewhere"); err == nil || !strings.Contains(err.Error(), "not checked out") {
		t.Fatalf("expected a rig that is not checked out refused, got %v", err)
	}
}
