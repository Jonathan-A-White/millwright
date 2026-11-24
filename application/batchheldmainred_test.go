package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1704: lampas batch 674 (six stories) was held on main's OWN red, carried
// by a bug, and the follower at once landed batch 675 (three of them), a
// 40-minute gate on the same red. No story is at fault when main is red, so
// splitting cannot help, and the rig is not gated again until main moves.

// mainRedBatch is the line batch 674 left: held red, on main's own red, at head.
func mainRedBatch(at time.Time, number int, head string, ids ...string) application.BatchMetric {
	line := heldBatch(at, number, ids...)
	line.MainRed, line.MainHead, line.MainBranch = true, head, "main"
	return line
}

func TestABatchHeldOnMainsOwnRedIsNotLandedAgainInParts(t *testing.T) {
	story := heldBatch(time.Time{}, 5, "lp-1", "lp-2", "lp-3")
	if _, ok := application.HeldRedTogether([]application.BatchMetric{story}); !ok {
		t.Fatal("expected a batch held red by a story to be landed again in parts")
	}
	main := mainRedBatch(time.Time{}, 5, "abc123", "lp-1", "lp-2", "lp-3")
	if got, ok := application.HeldRedTogether([]application.BatchMetric{main}); ok {
		t.Fatalf("expected a batch held on main's own red not to be split, got %+v", got)
	}
}

// A one-story line held on main's red is nobody's fault: it ends the drain of an
// older batch held red by a story, and it blames no story held red alone.
func TestAOneStoryLineHeldOnMainsRedBlamesNoStoryAndEndsTheDrain(t *testing.T) {
	older := heldBatch(time.Time{}, 4, "lp-1", "lp-2", "lp-3")
	alone := mainRedBatch(time.Time{}, 5, "abc123", "lp-1")
	if got, ok := application.HeldRedTogether([]application.BatchMetric{older, alone}); ok {
		t.Fatalf("expected the hold on main's red to end the drain, got %+v", got)
	}
	if got := application.HeldRedAlone([]application.BatchMetric{alone}); len(got) != 0 {
		t.Fatalf("expected no story to be held red alone for main's red, got %v", got)
	}
}

func TestABatchLineRecordsThatItWasHeldOnMainsRedAndMainsHead(t *testing.T) {
	at := time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)
	report := application.BatchReport{Rig: "lampas", Number: 674, Base: "abc123", Target: "main", Tested: true, RedAt: at.Add(-time.Hour),
		Queue: []application.BatchStory{{ID: "lp-1"}, {ID: "lp-2"}}, HeldOnMainRed: true}
	m := report.Metric(at)
	if !m.MainRed || m.MainHead != "abc123" || m.MainBranch != "main" {
		t.Fatalf("expected the line to say main's red at abc123 on main, got %+v", m)
	}
	report.HeldOnMainRed = false
	if m := report.Metric(at); m.MainRed || m.MainHead != "" || m.MainBranch != "" {
		t.Fatalf("expected a batch held red by a story to say nothing of main, got %+v", m)
	}
	// A line written before the field was read as not main-red.
	var old application.BatchMetric
	mustDo(t, json.Unmarshal([]byte(`{"kind":"batch","rig":"lampas","batch":5,"stories":3,"ids":["a","b","c"],"outcome":"held","red":true}`), &old))
	if old.MainRed || old.MainHead != "" {
		t.Fatalf("expected a line without the field to read as not main-red, got %+v", old)
	}
	if _, ok := application.HeldRedTogether([]application.BatchMetric{old}); !ok {
		t.Fatal("expected a line without the field to be split as before")
	}
}

type aMainRedRig struct {
	j        *aLandingJob
	landings *application.BatchLandings
	head     string
	headErr  error
	asked    [][2]string
}

func newAMainRedRig(t *testing.T, head string) *aMainRedRig {
	t.Helper()
	r := &aMainRedRig{j: newLandingJob(t), head: head}
	r.j.rule("lampas", domain.LandingBatch)
	ids := []string{"lp-1", "lp-2", "lp-3", "lp-4", "lp-5", "lp-6"}
	for i, id := range ids {
		r.j.queued("lampas", id, 300-i*10)
	}
	history := &apptest.FakeBatchMetrics{}
	mustDo(t, history.AppendBatch(context.Background(), mainRedBatch(r.j.now.Add(-10*time.Minute), 674, "abc123", ids...)))
	r.landings = r.j.landingsOn(r.j.tracker)
	r.landings.History, r.landings.LandScoped = history, r.j.landScoped
	r.landings.MainHead = func(_ context.Context, rig, branch string) (string, error) {
		r.asked = append(r.asked, [2]string{rig, branch})
		return r.head, r.headErr
	}
	return r
}

func TestARigHeldOnMainsRedIsNotDueWhileMainsHeadIsUnchanged(t *testing.T) {
	r := newAMainRedRig(t, "abc123")
	if reason, due := r.landings.Job().Probe(context.Background()); due {
		t.Fatalf("expected no landing while main's head is where the batch was held, got %q", reason)
	}
	mustDo(t, r.landings.Job().Run(context.Background()))
	if len(r.j.asked) != 0 {
		t.Fatalf("expected nothing landed, got %v", r.j.asked)
	}
	if want := [][2]string{{"lampas", "main"}}; !reflect.DeepEqual(r.asked[:1], want) {
		t.Errorf("expected main's head read for lampas on main, got %v", r.asked)
	}
}

func TestARigHeldOnMainsRedIsDueAsAWholeBatchOnceMainsHeadMoves(t *testing.T) {
	r := newAMainRedRig(t, "def456") // the carrier's fix landed
	reason, due := r.landings.Job().Probe(context.Background())
	if !due {
		t.Fatal("expected the rig to be due once main's head differs")
	}
	if strings.Contains(reason, "in parts") {
		t.Errorf("expected the whole batch, not parts, got %q", reason)
	}
	mustDo(t, r.landings.Job().Run(context.Background()))
	if want := []string{"lampas one=false"}; !reflect.DeepEqual(r.j.asked, want) {
		t.Fatalf("expected one whole batch, got %v", r.j.asked)
	}
}

// A head that cannot be read is no reason to leave a rig unlanded: it is landed
// as before, and the landing meets the same read.
func TestARigHeldOnMainsRedIsLandedWhenMainsHeadCannotBeRead(t *testing.T) {
	r := newAMainRedRig(t, "")
	r.headErr = errors.New("the origin could not be asked")
	if _, due := r.landings.Job().Probe(context.Background()); !due {
		t.Fatal("expected the rig to be due when main's head cannot be read")
	}
}

// Only the rig's last line counts: a batch that landed since ended the hold.
func TestARigWhoseLastBatchLandedAfterAMainRedHoldIsNotWaitingOnMain(t *testing.T) {
	r := newAMainRedRig(t, "abc123")
	history := &apptest.FakeBatchMetrics{}
	mustDo(t, history.AppendBatch(context.Background(), mainRedBatch(r.j.now.Add(-20*time.Minute), 674, "abc123", "lp-1", "lp-2")))
	mustDo(t, history.AppendBatch(context.Background(), application.BatchMetric{Kind: application.BatchKind, At: r.j.now.Add(-5 * time.Minute),
		Rig: "lampas", Batch: 675, Stories: 2, IDs: []string{"lp-1", "lp-2"}, Outcome: application.BatchGreen}))
	r.landings.History = history
	if _, due := r.landings.Job().Probe(context.Background()); !due {
		t.Fatal("expected a rig that landed since the hold to be due")
	}
}

// A part of a batch held red by a story that is itself held on main's red (main
// went red meanwhile) says the next part would meet it too: it is not landed.
func TestAPartHeldOnMainsRedStopsTheOtherPartsOfTheDrain(t *testing.T) {
	h := newAHeldBatchOfSix(t, "")
	h.landings.LandScoped = func(ctx context.Context, rig string, scope application.LandScope) error {
		h.j.asked = append(h.j.asked, rig+" only="+strings.Join(scope.Only, ","))
		h.batch++
		mustDo(t, h.history.AppendBatch(ctx, mainRedBatch(h.j.now, h.batch, "abc123", scope.Only...)))
		return errors.New("held: main is red")
	}
	_ = h.landings.Job().Run(context.Background())
	if want := []string{"lampas only=lp-1,lp-2,lp-3"}; !reflect.DeepEqual(h.j.asked, want) {
		t.Fatalf("expected the first half alone to be tried and the second left queued, got %v", h.j.asked)
	}
}
