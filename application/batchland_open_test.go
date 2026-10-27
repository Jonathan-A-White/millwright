package application_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A story queued for its batch whose status is open — a hold and un-hold while
// it ran, or an open-steps give-back, left it so — is landed as an in-progress
// one is (mw-bo813m2).

// queuedAs adds a story of the millwright epic to the batch rig, claimed and
// queued, and leaves it as how says: "in progress", "open claimed", "open
// unclaimed", "held" or "closed".
func queuedAs(t *testing.T, tracker *apptest.FakeTracker, id, how string) {
	t.Helper()
	ctx := context.Background()
	tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "The story " + id})
	mustDo(t, tracker.ClaimStory(ctx, id))
	queue(t, tracker, id)
	switch how {
	case "in progress":
	case "open claimed":
		// As the beads gateway lists it: still assigned to the host's claim.
		mustDo(t, tracker.ClaimAs(id, "mw@vps", time.Now().Add(time.Hour)))
		mustDo(t, tracker.SetStatus(id, apptest.StatusOpen))
	case "open unclaimed":
		mustDo(t, tracker.ReleaseClaim(ctx, id))
	case "held":
		mustDo(t, tracker.SetStatus(id, apptest.StatusDeferred))
	case "closed":
		mustDo(t, tracker.CloseStory(ctx, id, "done"))
	default:
		t.Fatalf("unknown way to leave a story: %s", how)
	}
}

func queueIDs(queue []application.BatchStory) []string {
	var ids []string
	for _, s := range queue {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestABatchTakesAQueuedStoryWhateverItsOpenStatusOrClaim(t *testing.T) {
	for _, how := range []string{"in progress", "open claimed", "open unclaimed"} {
		land, tracker, _ := aBatchRig(t)
		queuedAs(t, tracker, "mw-gq6.9", how)

		report, err := land.Run(context.Background(), "millwright")
		if err != nil {
			t.Fatalf("%s: a dry run: %v", how, err)
		}
		found := false
		for _, s := range report.Queue {
			found = found || s.ID == "mw-gq6.9"
		}
		if !found {
			t.Fatalf("expected the batch to take a queued story that is %s, got %v", how, queueIDs(report.Queue))
		}
	}
}

func TestABatchLeavesOutAQueuedStoryThatIsHeldAndSaysWhy(t *testing.T) {
	land, tracker, out := aBatchRig(t)
	queuedAs(t, tracker, "mw-gq6.9", "held")
	queuedAs(t, tracker, "mw-gq6.8", "closed")

	report, err := land.Run(context.Background(), "millwright")
	if err != nil {
		t.Fatalf("a dry run: %v", err)
	}
	if want := []string{"mw-gq6.2", "mw-gq6.4", "mw-gq6.1"}; !reflect.DeepEqual(queueIDs(report.Queue), want) {
		t.Fatalf("expected a held or closed story left out, the batch %v, got %v", want, queueIDs(report.Queue))
	}
	var named string
	for _, note := range report.Notes {
		if strings.Contains(note, "mw-gq6.9") {
			named = note
		}
		if strings.Contains(note, "mw-gq6.8") {
			t.Fatalf("a closed story is nobody's to name, got %q", note)
		}
	}
	if !strings.Contains(named, "held") {
		t.Fatalf("expected the batch report to name the held story and say it is held, got notes %v", report.Notes)
	}
	if !strings.Contains(out.String(), "mw-gq6.9") {
		t.Fatalf("expected the printed report to name it, got:\n%s", out.String())
	}
}

func TestADispatchNeverStartsAQueuedStoryThatIsOpen(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Its work is done"})
	mustDo(t, tracker.ClaimStory(ctx, "mw-gq6.1"))
	queue(t, tracker, "mw-gq6.1")
	mustDo(t, tracker.ReleaseClaim(ctx, "mw-gq6.1"))

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 0 || len(runner.Names()) != 0 {
		t.Fatalf("expected no new attempt on a queued story, got %+v", report.Started)
	}
	if len(report.Passed) != 1 || report.Passed[0].Gate != "queued" || !strings.Contains(report.Passed[0].Why, "mw land") {
		t.Fatalf("expected it passed over at the queued gate, saying mw land takes it, got %+v", report.Passed)
	}
}

// originWithout answers RefHead as a rig does: every branch is at the origin
// but the ones named.
type originWithout struct {
	application.BranchChanges
	missing map[string]bool
}

func (o originWithout) RefHead(_ context.Context, _, ref string) (string, error) {
	if o.missing[ref] {
		return "", fmt.Errorf("unknown revision %s", ref)
	}
	return "0123456789abcdef", nil
}

func TestLandRushCountsEveryQueuedStoryTheBatchTakesAndNamesTheOnesItLeavesOut(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 19, 0, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("lp-1", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "laptop"})
	for _, id := range []string{"lp-1.1", "lp-1.2", "lp-1.3", "lp-1.4", "lp-1.5"} {
		tracker.AddStory("lp-1", domain.Story{ID: id, Title: "The story " + id})
	}
	for id, how := range map[string]string{"lp-1.1": "in progress", "lp-1.2": "open claimed", "lp-1.3": "open unclaimed", "lp-1.4": "held", "lp-1.5": "in progress"} {
		mustDo(t, tracker.ClaimStory(ctx, id))
		queue(t, tracker, id)
		switch how {
		case "open claimed":
			mustDo(t, tracker.ClaimAs(id, "mw@laptop", now.Add(time.Hour)))
			mustDo(t, tracker.SetStatus(id, apptest.StatusOpen))
		case "open unclaimed":
			mustDo(t, tracker.ReleaseClaim(ctx, id))
		case "held":
			mustDo(t, tracker.SetStatus(id, apptest.StatusDeferred))
		}
	}
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{Landing: domain.LandingSettings{Mode: domain.LandingBatch}})
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder",
		Rigs:    map[string]string{"lampas": "/rigs/lampas"},
		Changes: originWithout{missing: map[string]bool{"origin/mw/lp-1.5": true}},
		Rules:   rules, Batches: &apptest.FakeBatchMetrics{},
		Now: func() time.Time { return now },
	}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := report.String()
	if !strings.Contains(out, "lampas: 3 queued") {
		t.Fatalf("expected the three stories the batch takes counted, in progress, open claimed and open unclaimed, in:\n%s", out)
	}
	for _, want := range []string{"left out: lp-1.4 is held", "left out: lp-1.5 has no branch"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in:\n%s", want, out)
		}
	}
	if section := out[strings.Index(out, "READY"):]; strings.Contains(strings.SplitN(section, "\n\n", 2)[0], "lp-1.3") {
		t.Fatalf("expected a queued story not to be ready in:\n%s", out)
	}
	if strings.Contains(out, application.OpenButClaimedHeading) {
		t.Fatalf("expected a queued story not to be listed open but claimed in:\n%s", out)
	}
}
