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

// mw-6ww.119.11.1.2: 28 one-story batches, 26 gate-hours in 4.3 days. A batch of
// several held red was landed again one story at a time, so the innocent
// stories of it each paid a gate. Now the suspect, when there is one, is left
// out and the rest land together, then it alone; with none the batch lands as
// two halves, and only a half red again goes one by one.

// aHeldBatchOfSix is a follower on lampas with six stories queued, oldest
// first, whose batch 600 was held red with line's suspect. land does what the
// follower asked for: red names the set of stories (joined by commas) whose
// batch is held red again.
type aHeldBatchOfSix struct {
	j        *aLandingJob
	history  *apptest.FakeBatchMetrics
	landings *application.BatchLandings
	red      map[string]bool
	batch    int
}

func newAHeldBatchOfSix(t *testing.T, suspect string) *aHeldBatchOfSix {
	t.Helper()
	h := &aHeldBatchOfSix{j: newLandingJob(t), history: &apptest.FakeBatchMetrics{}, red: map[string]bool{}, batch: 600}
	h.j.rule("lampas", domain.LandingBatch)
	ids := []string{"lp-1", "lp-2", "lp-3", "lp-4", "lp-5", "lp-6"}
	for i, id := range ids {
		h.j.queued("lampas", id, 300-i*10)
	}
	line := heldBatch(h.j.now.Add(-10*time.Minute), h.batch, ids...)
	line.Suspect = suspect
	mustDo(t, h.history.AppendBatch(context.Background(), line))
	h.landings = h.j.landingsOn(h.j.tracker)
	h.landings.History = h.history
	h.landings.LandScoped = func(ctx context.Context, rig string, scope application.LandScope) error {
		return h.land(ctx, rig, scope)
	}
	return h
}

func (h *aHeldBatchOfSix) land(ctx context.Context, rig string, scope application.LandScope) error {
	h.j.asked = append(h.j.asked, fmt.Sprintf("%s only=%s", rig, strings.Join(scope.Only, ",")))
	h.batch++
	if h.red[strings.Join(scope.Only, ",")] {
		mustDo(h.j.t, h.history.AppendBatch(ctx, heldBatch(h.j.now, h.batch, scope.Only...)))
		return errors.New("held: the rig's tests on the batch failed")
	}
	work, err := h.j.tracker.WorkInHand(ctx)
	mustDo(h.j.t, err)
	for _, d := range work.Running {
		if application.QueuedForBatch(d) && containsString(scope.Only, d.Story.ID) {
			mustDo(h.j.t, h.j.tracker.SetStoryState(ctx, d.Story.ID, application.RunState, application.RunLanded, "landed"))
		}
	}
	mustDo(h.j.t, h.history.AppendBatch(ctx, application.BatchMetric{Kind: application.BatchKind, At: h.j.now, Rig: "lampas",
		Batch: h.batch, Stories: len(scope.Only), IDs: scope.Only, Outcome: application.BatchGreen}))
	return nil
}

// landScoped is what mw land --only does, as the follower sees it: it asks, and
// the stories it names are landed.
func (j *aLandingJob) landScoped(ctx context.Context, rig string, scope application.LandScope) error {
	j.asked = append(j.asked, fmt.Sprintf("%s only=%s", rig, strings.Join(scope.Only, ",")))
	work, err := j.tracker.WorkInHand(ctx)
	mustDo(j.t, err)
	for _, d := range work.Running {
		if application.QueuedForBatch(d) && containsString(scope.Only, d.Story.ID) {
			mustDo(j.t, j.tracker.SetStoryState(ctx, d.Story.ID, application.RunState, application.RunLanded, "landed"))
		}
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func TestAfterABatchOfSixHeldRedWithASuspectTheOtherFiveLandTogetherThenTheSuspect(t *testing.T) {
	h := newAHeldBatchOfSix(t, "lp-3")
	mustDo(t, h.landings.Job().Run(context.Background()))
	want := []string{"lampas only=lp-1,lp-2,lp-4,lp-5,lp-6", "lampas only=lp-3"}
	if !reflect.DeepEqual(h.j.asked, want) {
		t.Fatalf("expected one batch of five and one of one, got %v", h.j.asked)
	}
}

func TestAfterABatchOfSixHeldRedWithNoSuspectItsStoriesLandAsTwoHalves(t *testing.T) {
	h := newAHeldBatchOfSix(t, "")
	mustDo(t, h.landings.Job().Run(context.Background()))
	want := []string{"lampas only=lp-1,lp-2,lp-3", "lampas only=lp-4,lp-5,lp-6"}
	if !reflect.DeepEqual(h.j.asked, want) {
		t.Fatalf("expected two batches of three, got %v", h.j.asked)
	}
}

func TestOnlyAHalfThatIsRedAgainGoesOneByOne(t *testing.T) {
	h := newAHeldBatchOfSix(t, "")
	h.red["lp-1,lp-2,lp-3"] = true
	mustDo(t, h.landings.Job().Run(context.Background()))
	want := []string{
		"lampas only=lp-1,lp-2,lp-3", // red again: one by one
		"lampas only=lp-1", "lampas only=lp-2", "lampas only=lp-3",
		"lampas only=lp-4,lp-5,lp-6", // the other half went on as a batch
	}
	if !reflect.DeepEqual(h.j.asked, want) {
		t.Fatalf("expected the red half landed one by one and the other as a batch, got %v", h.j.asked)
	}
}

// The drain stops at the first story of a red half held alone: it is the one
// that made the half red, and the rest of the queue lands without it.
func TestAStoryOfARedHalfHeldAloneStopsThatHalfsDrain(t *testing.T) {
	h := newAHeldBatchOfSix(t, "")
	h.red["lp-1,lp-2,lp-3"] = true
	h.red["lp-2"] = true
	if err := h.landings.Job().Run(context.Background()); err == nil {
		t.Fatal("expected the pass to fail")
	}
	want := []string{
		"lampas only=lp-1,lp-2,lp-3",
		"lampas only=lp-1", "lampas only=lp-2",
		"lampas only=lp-4,lp-5,lp-6",
	}
	if !reflect.DeepEqual(h.j.asked, want) {
		t.Fatalf("expected the half's drain to stop at lp-2, got %v", h.j.asked)
	}
}

// A suspect that is no longer queued, or a held batch of which one story is
// left, lands what is left with no halving.
func TestASuspectNoLongerQueuedIsNotLeftOutOfWhatIsLeft(t *testing.T) {
	h := newAHeldBatchOfSix(t, "lp-3")
	ctx := context.Background()
	mustDo(t, h.j.tracker.SetStoryState(ctx, "lp-3", application.RunState, application.RunLanded, "landed"))
	mustDo(t, h.landings.Job().Run(ctx))
	want := []string{"lampas only=lp-1,lp-2", "lampas only=lp-4,lp-5,lp-6"}
	if !reflect.DeepEqual(h.j.asked, want) {
		t.Fatalf("expected the five left landed as halves, got %v", h.j.asked)
	}
}

// The suspect is the story the fixer named as the cause; two named, or none, is
// no suspect; a batch that landed names none.
func TestABatchLineNamesTheSuspectTheFixerNamed(t *testing.T) {
	at := time.Date(2026, 11, 21, 3, 0, 0, 0, time.UTC)
	report := func(causes ...string) application.BatchReport {
		f := &application.BatchFixing{Started: true}
		for _, id := range causes {
			f.Causes = append(f.Causes, application.FixerWord{Story: id, Said: "its test is wrong"})
		}
		return application.BatchReport{Rig: "lampas", Number: 600, Tested: true, RedAt: at.Add(-time.Hour), Fixer: f,
			Queue: []application.BatchStory{{ID: "lp-1"}, {ID: "lp-2"}, {ID: "lp-3"}}}
	}
	if got := report("lp-3").Metric(at).Suspect; got != "lp-3" {
		t.Errorf("expected the fixer's one story as the suspect, got %q", got)
	}
	if got := report("lp-3", "lp-3").Metric(at).Suspect; got != "lp-3" {
		t.Errorf("expected one story named twice to be the suspect, got %q", got)
	}
	if got := report("lp-1", "lp-3").Metric(at).Suspect; got != "" {
		t.Errorf("expected two stories named to be no suspect, got %q", got)
	}
	if got := report("lp-9").Metric(at).Suspect; got != "" {
		t.Errorf("expected a story outside the batch to be no suspect, got %q", got)
	}
	if got := report().Metric(at).Suspect; got != "" {
		t.Errorf("expected no suspect when the fixer named none, got %q", got)
	}
	landed := report("lp-3")
	landed.Landed, landed.Merged = true, landed.Queue
	if got := landed.Metric(at).Suspect; got != "" {
		t.Errorf("expected no suspect for a batch that landed, got %q", got)
	}
}

// HeldRedTogether hands on the suspect with the line.
func TestHeldRedTogetherKeepsTheSuspect(t *testing.T) {
	line := heldBatch(time.Time{}, 5, "lp-1", "lp-2", "lp-3")
	line.Suspect = "lp-2"
	got, ok := application.HeldRedTogether([]application.BatchMetric{line})
	if !ok || got.Suspect != "lp-2" {
		t.Fatalf("expected the held line with its suspect, got %+v, %v", got, ok)
	}
}

// mw land --without leaves stories out of one batch, --only takes just them;
// both leave what they skip queued.
func TestMwLandWithoutAndOnlyNarrowTheQueue(t *testing.T) {
	ids := func(land application.BatchLand) []string {
		t.Helper()
		report, err := land.Run(context.Background(), "millwright")
		mustDo(t, err)
		var got []string
		for _, s := range report.Queue {
			got = append(got, s.ID)
		}
		return got
	}
	land, _, out := aBatchRig(t)
	land.Without = []string{"mw-gq6.4"}
	if got, want := ids(land), []string{"mw-gq6.2", "mw-gq6.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the batch made without mw-gq6.4, got %v", got)
	}
	if !strings.Contains(out.String(), "mw-gq6.4 is left out of this batch (--without)") {
		t.Errorf("expected the report to say why mw-gq6.4 is left out, got:\n%s", out.String())
	}
	land, _, out = aBatchRig(t)
	land.Only = []string{"mw-gq6.2", "mw-gq6.9"}
	if got, want := ids(land), []string{"mw-gq6.2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected a batch of only mw-gq6.2, got %v", got)
	}
	if !strings.Contains(out.String(), "mw-gq6.9 was asked for with --only, but it is not queued") {
		t.Errorf("expected the report to say mw-gq6.9 is not queued, got:\n%s", out.String())
	}
}

// mw-gq6.1541: a story taken out of the batch queue by hand keeps its branch,
// is given back held with a SET-ASIDE comment, and the next batch is made of
// the rest.
func TestSetAsideTakesAQueuedStoryOutOfTheBatchQueue(t *testing.T) {
	land, tracker, _ := aBatchRig(t)
	var out strings.Builder
	set := application.SetAside{Tracker: tracker, Host: "vps", Out: &out}
	ctx := context.Background()

	if err := set.Run(ctx, "millwright", "mw-gq6.4", " "); err == nil || !strings.Contains(err.Error(), "why") {
		t.Fatalf("expected a refusal with no why, got %v", err)
	}
	if err := set.Run(ctx, "millwright", "lp-1.1", "wrong rig"); err == nil {
		t.Fatal("expected a story of another rig to be refused")
	}
	if err := set.Run(ctx, "millwright", "mw-gq6.3", "not queued"); err == nil {
		t.Fatal("expected a story that is not queued to be refused")
	}
	mustDo(t, set.Run(ctx, "millwright", "mw-gq6.4", "its spec fails on main"))

	detail, err := tracker.ShowStory(ctx, "mw-gq6.4")
	mustDo(t, err)
	if detail.Status != application.StatusHeld || detail.Assignee != "" {
		t.Errorf("expected mw-gq6.4 held and given back, got %q held by %q", detail.Status, detail.Assignee)
	}
	if !application.QueuedForBatch(detail) {
		t.Errorf("expected the run state kept, so that releasing the story queues it again, got %v", detail.Labels)
	}
	comments, err := tracker.StoryComments(ctx, "mw-gq6.4")
	mustDo(t, err)
	if len(comments) == 0 || !strings.HasPrefix(comments[len(comments)-1].Text, "HELD: SET-ASIDE: its spec fails on main") ||
		!strings.Contains(comments[len(comments)-1].Text, "mw/mw-gq6.4") {
		t.Errorf("expected a SET-ASIDE comment carrying the why and naming the branch, got %+v", comments)
	}

	report, err := land.Run(ctx, "millwright")
	mustDo(t, err)
	var got []string
	for _, s := range report.Queue {
		got = append(got, s.ID)
	}
	if want := []string{"mw-gq6.2", "mw-gq6.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the next batch made of the rest, got %v", got)
	}
}
