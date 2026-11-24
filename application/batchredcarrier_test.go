package application_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1708: lampas main was red, carried by mw-y1k16cd, whose fix was built
// and queued at 16:25Z and could not land: the rig waited for main's head to move
// (mw-gq6.1704), the check of the hold's tests on main alone held any landing
// while the carrier was open, the carrier's own too (mw-gq6.1706), and the batch
// of the oldest six under batch_max left the carrier, queued last, out of it. An
// open carrier queued on its rig lands first and alone.

const lampasCarrier = "lp-9"

// aRigWithItsCarrierQueued is lampas held on main's red at head abc123 by a batch
// of lp-1 to lp-6 (batch 674), carried by lp-9, whose fix is queued last.
type aRigWithItsCarrierQueued struct {
	*aMainRedRig
}

func newARigWithItsCarrierQueued(t *testing.T, carrierQueued bool) aRigWithItsCarrierQueued {
	t.Helper()
	r := &aMainRedRig{j: newLandingJob(t), head: "abc123"}
	r.j.rule("lampas", domain.LandingBatch)
	ids := []string{"lp-1", "lp-2", "lp-3", "lp-4", "lp-5", "lp-6", "lp-7", "lp-8"}
	for i, id := range ids {
		r.j.queued("lampas", id, 300-i*10)
	}
	if carrierQueued {
		r.j.queued("lampas", lampasCarrier, 5)
	}
	line := mainRedBatch(r.j.now.Add(-10*time.Minute), 674, "abc123", ids[:6]...)
	line.MainBug, line.MainTests = lampasCarrier, []string{"appearance.spec.ts:207"}
	history := &apptest.FakeBatchMetrics{}
	mustDo(t, history.AppendBatch(context.Background(), line))
	r.landings = r.j.landingsOn(r.j.tracker)
	r.landings.History, r.landings.LandScoped = history, r.j.landScoped
	r.landings.MainHead = func(_ context.Context, rig, branch string) (string, error) {
		r.asked = append(r.asked, [2]string{rig, branch})
		return r.head, r.headErr
	}
	return aRigWithItsCarrierQueued{r}
}

func TestARigHeldOnMainsRedIsDueWhenItsOpenCarrierIsQueuedAndLandsTheCarrierAlone(t *testing.T) {
	r := newARigWithItsCarrierQueued(t, true)
	reason, due := r.landings.Job().Probe(context.Background())
	if !due {
		t.Fatal("expected the rig to be due: its queue holds the carrier of the hold, though main's head is unchanged")
	}
	if !strings.Contains(reason, lampasCarrier) {
		t.Errorf("expected the reason to name the carrier, got %q", reason)
	}
	mustDo(t, r.landings.Job().Run(context.Background()))
	if want := []string{"lampas only=" + lampasCarrier}; !reflect.DeepEqual(r.j.asked, want) {
		t.Fatalf("expected the carrier landed alone, got %v", r.j.asked)
	}
}

func TestARigHeldOnMainsRedIsNotDueWithoutItsCarrierQueued(t *testing.T) {
	r := newARigWithItsCarrierQueued(t, false)
	if reason, due := r.landings.Job().Probe(context.Background()); due {
		t.Fatalf("expected no landing while main's head is unchanged and the carrier is not queued, got %q", reason)
	}
}

// A carrier that was in the batch the rig was held on, and main is red still, did
// not fix it: it is not landed again at the same head.
func TestARigHeldOnMainsRedByABatchHoldingTheCarrierWaitsForMainsHeadAsBefore(t *testing.T) {
	r := newARigWithItsCarrierQueued(t, true)
	line := mainRedBatch(r.j.now.Add(-5*time.Minute), 675, "abc123", lampasCarrier)
	line.MainBug, line.MainTests = lampasCarrier, []string{"appearance.spec.ts:207"}
	mustDo(t, r.landings.History.(*apptest.FakeBatchMetrics).AppendBatch(context.Background(), line))
	if reason, due := r.landings.Job().Probe(context.Background()); due {
		t.Fatalf("expected the carrier not landed again at the head its own batch was held at, got %q", reason)
	}
}

// Main's head moving is the old rule: the whole queue lands, as one batch.
func TestARigHeldOnMainsRedWhoseHeadMovedLandsTheWholeQueueNotTheCarrierAlone(t *testing.T) {
	r := newARigWithItsCarrierQueued(t, true)
	r.head = "def456"
	mustDo(t, r.landings.Job().Run(context.Background()))
	if want := []string{"lampas one=false"}; !reflect.DeepEqual(r.j.asked, want) {
		t.Fatalf("expected one whole batch, got %v", r.j.asked)
	}
}

// A rig that lands each story by itself lands the carrier first and alone too.
func TestARigThatLandsEachStoryLandsItsCarrierAloneWhileMainsHeadIsUnchanged(t *testing.T) {
	r := newARigWithItsCarrierQueued(t, true)
	r.j.rule("lampas", domain.LandingEach)
	mustDo(t, r.landings.Job().Run(context.Background()))
	if want := []string{"lampas only=" + lampasCarrier}; !reflect.DeepEqual(r.j.asked, want) {
		t.Fatalf("expected the carrier landed alone, got %v", r.j.asked)
	}
}

// A carrier landing held again does not go round at every pass: it is a landing
// that failed, and the rig waits as a failed landing does.
func TestACarrierLandingThatFailsIsNotBegunAgainAtOnce(t *testing.T) {
	r := newARigWithItsCarrierQueued(t, true)
	r.landings.LandScoped = func(ctx context.Context, rig string, scope application.LandScope) error {
		r.j.asked = append(r.j.asked, rig+" only="+strings.Join(scope.Only, ","))
		return errors.New("held: the rig's tests failed")
	}
	job := r.landings.Job()
	_ = job.Run(context.Background())
	_ = job.Run(context.Background())
	if len(r.j.asked) != 1 {
		t.Fatalf("expected the failed carrier landing to wait, got %v", r.j.asked)
	}
}

// aBatchOfTheQueue is a dry run of lampas with nine stories queued older than
// lp-1.1, which is the carrier of the rig's last hold on main's red.
func aBatchOfTheQueue(t *testing.T, carrier string) (*application.BatchReport, string) {
	t.Helper()
	land, tracker, out := aBatchRig(t)
	ctx := context.Background()
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("lp-1.%d", 10+i)
		tracker.AddStory("lp-1", domain.Story{ID: id, Title: "The story " + id})
		mustDo(t, tracker.ClaimStory(ctx, id))
		queue(t, tracker, id)
		mustDo(t, tracker.SetStoryMetadata(ctx, id, map[string]string{
			application.QueuedAtField: time.Date(2026, 10, 15, 11, 59-i, 0, 0, time.UTC).Format(time.RFC3339),
		}))
	}
	history := &apptest.FakeBatchMetrics{}
	line := mainRedBatch(time.Date(2026, 10, 15, 11, 0, 0, 0, time.UTC), 674, "abc123", "lp-1.10", "lp-1.11")
	line.MainBug, line.MainTests = carrier, []string{"appearance.spec.ts:207"}
	mustDo(t, history.AppendBatch(ctx, line))
	land.Metrics = history
	report, err := land.Run(ctx, "lampas")
	if err != nil {
		t.Fatalf("a dry run: %v", err)
	}
	return &report, out.String()
}

func TestTheBatchMadeForARigHeldOnMainsRedHoldsTheCarrierAloneThoughMoreThanBatchMaxAreOlder(t *testing.T) {
	report, out := aBatchOfTheQueue(t, "lp-1.1") // queued after all nine
	var got []string
	for _, s := range report.Queue {
		got = append(got, s.ID)
	}
	if want := []string{"lp-1.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the carrier alone, got %v", got)
	}
	if !strings.Contains(out, "carries main's red") {
		t.Errorf("expected a note that the carrier goes first and alone, got:\n%s", out)
	}
}

func TestTheBatchMadeForARigHeldOnMainsRedWhoseCarrierIsNotQueuedIsTheOldestFirstAsBefore(t *testing.T) {
	report, _ := aBatchOfTheQueue(t, "lp-1.77")
	if len(report.Queue) != domain.DefaultBatchMax || report.Queue[0].ID != "lp-1.18" {
		t.Fatalf("expected the six oldest, got %+v", report.Queue)
	}
}

// mainStillRed's check holds a batch that does not hold the carrier, and not one
// that does: the carrier's own batch runs the rig's tests, which prove its fix.
func TestARigHeldOnMainsRedGatesABatchThatHoldsTheCarrierWithoutTheCheckOfMain(t *testing.T) {
	a := newAMainRedRecheck(t, failingOnMain("--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n"))
	a.report.Queue = []application.BatchStory{{ID: mainRedCarrier, Title: "[bug] main is red: TestA, TestB", Branch: "origin/mw/" + mainRedCarrier}}
	a.tracker.AddStory("mw-e", domain.Story{ID: "mw-e.77", Title: "The story"})
	a.run(t)
	if len(a.named.asked) != 0 {
		t.Errorf("expected main's red not run again before the carrier's own gate, got %v", a.named.asked)
	}
	if a.checks.runs != 1 {
		t.Errorf("expected the rig's tests run once on the carrier's batch, got %d runs", a.checks.runs)
	}
	if !a.report.Landed {
		t.Errorf("expected the carrier's batch landed, got held %q", a.report.Held)
	}
}

func TestARigHeldOnMainsRedStillHoldsABatchThatDoesNotHoldTheCarrier(t *testing.T) {
	a := newAMainRedRecheck(t, failingOnMain("--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n"))
	a.run(t) // the batch is mw-e.1 and mw-e.2
	if a.checks.runs != 0 || a.report.Landed || !strings.Contains(a.report.Held, "main is red") {
		t.Fatalf("expected the batch held again without its gate, got %d runs, landed %v, held %q", a.checks.runs, a.report.Landed, a.report.Held)
	}
}
