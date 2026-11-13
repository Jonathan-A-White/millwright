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

// mw-gq6.1246: lampas stories that passed their gate waited hours to land.
// Batch 524 of lampas took nine queued stories; its tests were red because one
// of them (mw-qtu3zvz.92) broke a layer rule another added, its fixer mended
// that, and the second run was red again at load 26. All nine stayed queued,
// and the next batch would have taken the same nine again, so mw-82lb97i.27,
// queued 09:49Z, was still not landed at 17:37Z. The Mayor landed them by hand
// with mw land lampas --one, oldest first, stopping at the first red.

// heldBatch is the line batch 524 of lampas left: held, red, nine stories.
func heldBatch(at time.Time, number int, ids ...string) application.BatchMetric {
	return application.BatchMetric{Kind: application.BatchKind, At: at, Rig: "lampas", Host: "laptop",
		Batch: number, Stories: len(ids), IDs: ids, Outcome: application.BatchHeld, Red: true}
}

func TestAfterABatchHeldRedTheFollowerLandsItsStoriesOneAtATimeAtOnce(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingBatch)
	ids := []string{"lp-23", "lp-27", "lp-8"}
	for i, id := range ids {
		j.queued("lampas", id, 480-i*30) // queued hours ago, oldest first
	}
	history := &apptest.FakeBatchMetrics{}
	// The first landing is batch 524: held red, every story stays queued.
	held := false
	land := func(ctx context.Context, rig string, one bool) error {
		if !held {
			held = true
			j.asked = append(j.asked, fmt.Sprintf("%s one=%v", rig, one))
			mustDo(t, history.AppendBatch(ctx, heldBatch(j.now, 524, ids...)))
			return errors.New("batch 524 of lampas was held: the rig's tests on the batch failed")
		}
		return j.land(ctx, rig, one)
	}
	landings := j.landingsOn(j.tracker)
	landings.Land, landings.History = land, history

	if err := landings.Job().Run(context.Background()); err == nil {
		t.Fatal("expected the held batch to fail the pass")
	}
	// The next pass, at once: not batch_every later, and not the same batch.
	j.now = j.now.Add(time.Minute)
	reason, due := landings.Job().Probe(context.Background())
	if !due {
		t.Fatal("expected a rig whose batch was held red to be due again at once, its stories one at a time")
	}
	if !strings.Contains(reason, "one at a time") {
		t.Errorf("expected the probe to say the stories land one at a time, got %q", reason)
	}
	mustDo(t, landings.Job().Run(context.Background()))
	want := []string{"lampas one=false", "lampas one=true", "lampas one=true", "lampas one=true"}
	if !reflect.DeepEqual(j.asked, want) {
		t.Fatalf("expected the held batch's three stories landed one at a time after it, got %v", j.asked)
	}
}

// The follower is begun again after every landing of the factory rig: what a
// held batch was is read from the batch lines, not kept in its memory.
func TestAFollowerBegunAfterABatchWasHeldRedStillLandsItsStoriesOneAtATime(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingBatch)
	j.queued("lampas", "lp-1", 300)
	j.queued("lampas", "lp-2", 200)
	j.queued("lampas", "lp-3", 5) // queued after the batch was held
	history := &apptest.FakeBatchMetrics{}
	mustDo(t, history.AppendBatch(context.Background(), heldBatch(j.now.Add(-10*time.Minute), 524, "lp-1", "lp-2")))
	landings := j.landingsOn(j.tracker)
	landings.History = history

	mustDo(t, landings.Job().Run(context.Background()))
	// The two the held batch had, alone; lp-3 waits for the next batch.
	if want := []string{"lampas one=true", "lampas one=true"}; !reflect.DeepEqual(j.asked, want) {
		t.Fatalf("expected the held batch's two stories landed alone, got %v", j.asked)
	}
}

// A batch held for the host, not its tests (it never ran them red), and a
// held story landed alone, are not landed one at a time: the first is the
// load's, the second has no other story to free.
func TestABatchHeldWithoutARedOrAloneIsNotLandedOneAtATime(t *testing.T) {
	for name, line := range map[string]application.BatchMetric{
		"busy":  {Kind: application.BatchKind, Rig: "lampas", Batch: 9, Stories: 2, IDs: []string{"lp-1", "lp-2"}, Outcome: application.BatchHeld},
		"alone": heldBatch(time.Time{}, 9, "lp-1"),
		"green": {Kind: application.BatchKind, Rig: "lampas", Batch: 9, Stories: 2, IDs: []string{"lp-1", "lp-2"}, Outcome: application.BatchGreen},
	} {
		t.Run(name, func(t *testing.T) {
			j := newLandingJob(t)
			j.rule("lampas", domain.LandingBatch)
			j.queued("lampas", "lp-1", 300)
			j.queued("lampas", "lp-2", 200)
			line.At = j.now.Add(-10 * time.Minute)
			history := &apptest.FakeBatchMetrics{}
			mustDo(t, history.AppendBatch(context.Background(), line))
			landings := j.landingsOn(j.tracker)
			landings.History = history
			mustDo(t, landings.Job().Run(context.Background()))
			if want := []string{"lampas one=false"}; !reflect.DeepEqual(j.asked, want) {
				t.Fatalf("expected one batch, got %v", j.asked)
			}
		})
	}
}

// A story held red alone, after its batch was held, is the one that made the
// batch red: the next batch is made without it, so the rest land, and it is
// taken again once the rig has landed since.
func TestAStoryHeldRedAloneIsLeftOutOfTheBatchUntilTheRigLandsAgain(t *testing.T) {
	land, _, out := aBatchRig(t)
	at := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	history := &apptest.FakeBatchMetrics{}
	line := func(number int, outcome string, red bool, ids ...string) {
		mustDo(t, history.AppendBatch(context.Background(), application.BatchMetric{Kind: application.BatchKind,
			At: at.Add(time.Duration(number) * time.Minute), Rig: "millwright", Batch: number,
			Stories: len(ids), IDs: ids, Outcome: outcome, Red: red}))
	}
	line(6, application.BatchHeld, true, "mw-gq6.2", "mw-gq6.4", "mw-gq6.1")
	line(7, application.BatchHeld, true, "mw-gq6.2")                                                    // the oldest, alone: red
	mustDo(t, history.AppendBatch(context.Background(), heldBatch(at.Add(8*time.Minute), 8, "lp-1.1"))) // another rig's
	land.Metrics = history

	report, err := land.Run(context.Background(), "millwright")
	mustDo(t, err)
	var got []string
	for _, s := range report.Queue {
		got = append(got, s.ID)
	}
	if want := []string{"mw-gq6.4", "mw-gq6.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the batch made without mw-gq6.2, got %v", got)
	}
	if !strings.Contains(out.String(), "mw-gq6.2 was held red alone in batch 7 of millwright") {
		t.Errorf("expected the report to say why mw-gq6.2 is left out, got:\n%s", out.String())
	}

	line(9, application.BatchGreen, false, "mw-gq6.4", "mw-gq6.1")
	report, err = land.Run(context.Background(), "millwright")
	mustDo(t, err)
	if len(report.Queue) == 0 || report.Queue[0].ID != "mw-gq6.2" {
		t.Fatalf("expected mw-gq6.2 taken again once the rig landed, got %v", report.Queue)
	}
}

// The line a batch leaves names its stories and whether its tests were red,
// which is what the follower and the next batch read.
func TestABatchLineNamesItsStoriesAndWhetherItWentRed(t *testing.T) {
	at := time.Date(2026, 11, 13, 17, 37, 0, 0, time.UTC)
	held := application.BatchReport{Rig: "lampas", Number: 524, Tested: true, RedAt: at.Add(-time.Hour),
		Queue: []application.BatchStory{{ID: "lp-1"}, {ID: "lp-2"}}}
	m := held.Metric(at)
	if !reflect.DeepEqual(m.IDs, []string{"lp-1", "lp-2"}) || !m.Red || m.Outcome != application.BatchHeld {
		t.Fatalf("expected the held line to name lp-1 and lp-2, red, got %+v", m)
	}
	landed := application.BatchReport{Rig: "lampas", Number: 525, Tested: true, Landed: true,
		Queue:  []application.BatchStory{{ID: "lp-1"}, {ID: "lp-2"}},
		Merged: []application.BatchStory{{ID: "lp-2"}}}
	m = landed.Metric(at)
	if !reflect.DeepEqual(m.IDs, []string{"lp-2"}) || m.Red {
		t.Fatalf("expected the landed line to name only what it landed, not red, got %+v", m)
	}
}
