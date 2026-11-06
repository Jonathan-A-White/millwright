package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

func retireHands(tracker interface {
	application.WorkTracker
	application.PosternNotes
}, out *bytes.Buffer, bead, id, reason string) error {
	return application.HandsRetire{Tracker: tracker, Notes: tracker, Out: out}.Run(context.Background(), application.HandsRetireRequest{Bead: bead, ID: id, Reason: reason})
}

// A retired step is one he no longer needs: it keeps its text, but the note
// holds why, the bead says so, and the step stays kept.
func TestHandsRetireMarksTheStepAndCommentsWhy(t *testing.T) {
	tracker := handsTracker()
	var out bytes.Buffer
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep()}); err != nil {
		t.Fatal(err)
	}

	if err := retireHands(tracker, &out, "mw-f758y.8", "linger", "done by hand at the Laptop"); err != nil {
		t.Fatalf("retiring: %v", err)
	}

	note, _ := tracker.Note(context.Background(), application.HandsRetiredKey("mw-f758y.8", "linger"))
	if note != "done by hand at the Laptop" {
		t.Fatalf("expected the reason kept, got %q", note)
	}
	comments := tracker.Comments("mw-f758y.8")
	last := comments[len(comments)-1]
	if !strings.Contains(last, "RETIRED") || !strings.Contains(last, "linger") || !strings.Contains(last, "done by hand at the Laptop") {
		t.Fatalf("expected the bead to say the step is retired and why, got %q", last)
	}
	if len(handsSteps(t, tracker, "mw-f758y.8")) != 1 {
		t.Fatalf("expected the step itself kept")
	}
}

func TestHandsRetireRefusesWithoutAReasonAndForAnUnknownStepOrBead(t *testing.T) {
	tracker := handsTracker()
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep()}); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string][3]string{
		"no reason":    {"mw-f758y.8", "linger", "  "},
		"unknown step": {"mw-f758y.8", "nope", "why"},
		"unknown bead": {"mw-none", "linger", "why"},
	} {
		if err := retireHands(tracker, &bytes.Buffer{}, c[0], c[1], c[2]); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
}

// A retired step is listed as retired, with its reason, and the list of every
// open step leaves it out.
func TestHandsListShowsARetiredStepAsRetiredAndLeavesItOutOfTheOpenList(t *testing.T) {
	tracker := handsTracker()
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep()}); err != nil {
		t.Fatal(err)
	}
	mustDo(t, retireHands(tracker, &bytes.Buffer{}, "mw-f758y.8", "linger", "done by hand"))

	var out bytes.Buffer
	if _, err := (application.HandsList{Notes: tracker, Out: &out}).Run(context.Background(), "mw-f758y.8"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "retired: done by hand (cannot be approved)") {
		t.Errorf("expected the step listed as retired, got\n%s", out.String())
	}
	out.Reset()
	steps, err := application.HandsList{Notes: tracker, Tracker: tracker, Out: &out}.Run(context.Background(), "")
	if err != nil || len(steps) != 0 || out.String() != "no open hands steps\n" {
		t.Errorf("expected no open steps, got %+v %v %q", steps, err, out.String())
	}
}

// --replace brings a retired step back: the new step is a new thing to approve.
func TestHandsAddReplaceBringsARetiredStepBack(t *testing.T) {
	tracker := handsTracker()
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep()}); err != nil {
		t.Fatal(err)
	}
	mustDo(t, retireHands(tracker, &bytes.Buffer{}, "mw-f758y.8", "linger", "done by hand"))
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep(), Replace: true}); err != nil {
		t.Fatal(err)
	}
	if note, _ := tracker.Note(context.Background(), application.HandsRetiredKey("mw-f758y.8", "linger")); note != "" {
		t.Fatalf("expected the retirement forgotten, got %q", note)
	}
}

// A hands bead whose every step ran clean or was retired leaves Needs you; a
// retired step is left off the need while another step still waits.
func TestPosternViewHandsNeedLeavesOnceEveryStepRanCleanOrWasRetired(t *testing.T) {
	ctx := context.Background()
	setup := func(retired ...string) (doc application.PosternViewDoc) {
		tracker := handsTracker()
		mustDo(t, tracker.SetLabels("mw-f758y.8", "hitl"))
		var records []application.HandsStepRecord
		for _, id := range []string{"a", "b"} {
			records = append(records, application.HandsStepRecord{HandsStep: domain.HandsStep{ID: id, Host: "desktop", As: "user", Run: "true"}})
		}
		raw, _ := json.Marshal(records)
		mustDo(t, tracker.SetNote(ctx, application.HandsStepsKey("mw-f758y.8"), string(raw)))
		mustDo(t, tracker.SetNote(ctx, application.HandsRanKey("mw-f758y.8", "a"), `{"at":"2026-09-28T12:03:00Z","exit":0,"host":"desktop"}`))
		for _, id := range retired {
			mustDo(t, tracker.SetNote(ctx, application.HandsRetiredKey("mw-f758y.8", id), "done by hand"))
		}
		return viewDoc(t, tracker)
	}
	hasHands := func(doc application.PosternViewDoc) bool {
		for _, n := range doc.Needs {
			if n.Kind == "hands" && n.Bead == "mw-f758y.8" {
				return true
			}
		}
		return false
	}
	if hasHands(setup("b")) {
		t.Errorf("expected no hands need when one step ran clean and the other was retired")
	}
	if !hasHands(setup()) {
		t.Errorf("expected the need while step b waits")
	}
	// retired alone does not clear a need with a step still to run
	tracker := handsTracker()
	mustDo(t, tracker.SetLabels("mw-f758y.8", "hitl"))
	records := []application.HandsStepRecord{
		{HandsStep: domain.HandsStep{ID: "a", Host: "desktop", As: "user", Run: "true"}},
		{HandsStep: domain.HandsStep{ID: "b", Host: "desktop", As: "user", Run: "true"}},
	}
	raw, _ := json.Marshal(records)
	mustDo(t, tracker.SetNote(ctx, application.HandsStepsKey("mw-f758y.8"), string(raw)))
	mustDo(t, tracker.SetNote(ctx, application.HandsRetiredKey("mw-f758y.8", "a"), "done by hand"))
	need := viewNeed(t, viewDoc(t, tracker), "hands", "mw-f758y.8")
	if len(need.Steps) != 1 || need.Steps[0].ID != "b" {
		t.Errorf("expected only the step still to run on the need, got %+v", need.Steps)
	}
}

// A tap on a retired step runs nothing and says why; so does a tap on a step
// of a closed bead (mw-gq6.571).
func TestApplyRefusesATapOnARetiredStepOrAClosedBead(t *testing.T) {
	ctx := context.Background()
	f := newRunFixture(t)
	mustDo(t, f.tracker.SetNote(ctx, application.HandsRetiredKey("mw-e.3", "echo"), "done by hand at the Laptop"))
	f.approve(t, "tx-retired", "echo", runNow, "")
	f.apply(t)
	if jobs := f.runner.Jobs(); len(jobs) != 0 {
		t.Fatalf("expected a retired step never run, got %+v", jobs)
	}
	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 || !strings.Contains(got[0], "retired") || !strings.Contains(got[0], "done by hand at the Laptop") {
		t.Fatalf("expected the NOT RUN comment to say it is retired and why, got %q", got)
	}

	g := newRunFixture(t)
	mustDo(t, g.tracker.CloseStory(ctx, "mw-e.3", "done"))
	g.approve(t, "tx-closed", "echo", runNow, "")
	g.apply(t)
	if jobs := g.runner.Jobs(); len(jobs) != 0 {
		t.Fatalf("expected a closed bead's step never run, got %+v", jobs)
	}
	if got := g.tracker.Comments("mw-e.3"); len(got) != 1 || !strings.Contains(got[0], "closed") {
		t.Fatalf("expected the NOT RUN comment to say the bead is closed, got %q", got)
	}
}

// A closed bead shows no hands need in the view.
func TestPosternViewAClosedBeadShowsNoHandsNeed(t *testing.T) {
	ctx := context.Background()
	tracker := handsTracker()
	mustDo(t, tracker.SetLabels("mw-f758y.8", "hitl"))
	raw, _ := json.Marshal([]application.HandsStepRecord{{HandsStep: domain.HandsStep{ID: "a", Host: "desktop", As: "user", Run: "true"}}})
	mustDo(t, tracker.SetNote(ctx, application.HandsStepsKey("mw-f758y.8"), string(raw)))
	mustDo(t, tracker.CloseStory(ctx, "mw-f758y.8", "done"))
	for _, n := range viewDoc(t, tracker).Needs {
		if n.Kind == "hands" && n.Bead == "mw-f758y.8" {
			t.Fatalf("expected no hands need for a closed bead, got %+v", n)
		}
	}
}

func hasHitl(t *testing.T, tracker interface {
	ShowStory(ctx context.Context, id string) (application.StoryDetail, error)
}, bead string) bool {
	t.Helper()
	detail, err := tracker.ShowStory(context.Background(), bead)
	mustDo(t, err)
	for _, l := range detail.Labels {
		if l == application.LabelHitl {
			return true
		}
	}
	return false
}

func secondStep() domain.HandsStep {
	return domain.HandsStep{ID: "second", Host: "desktop", As: "user", Run: "true"}
}

// Retiring the only live step takes hitl off the bead, so a child filed by
// copying the bead's labels does not come up parked on a human step
// (mw-gq6.750).
func TestHandsRetireOfTheOnlyStepDropsHitl(t *testing.T) {
	tracker := handsTracker()
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep()}); err != nil {
		t.Fatal(err)
	}
	if !hasHitl(t, tracker, "mw-f758y.8") {
		t.Fatal("expected hands add to have labelled the bead hitl")
	}

	mustDo(t, retireHands(tracker, &bytes.Buffer{}, "mw-f758y.8", "linger", "done by hand"))

	if hasHitl(t, tracker, "mw-f758y.8") {
		t.Fatal("expected hitl dropped once no live step remained")
	}
}

// Retiring one of two live steps leaves hitl: the other still waits for him.
func TestHandsRetireOfOneOfTwoLiveStepsKeepsHitl(t *testing.T) {
	tracker := handsTracker()
	for _, step := range []domain.HandsStep{lingerStep(), secondStep()} {
		if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: step}); err != nil {
			t.Fatal(err)
		}
	}

	mustDo(t, retireHands(tracker, &bytes.Buffer{}, "mw-f758y.8", "linger", "done by hand"))
	if !hasHitl(t, tracker, "mw-f758y.8") {
		t.Fatal("expected hitl kept while a step is still live")
	}

	mustDo(t, retireHands(tracker, &bytes.Buffer{}, "mw-f758y.8", "second", "done by hand"))
	if hasHitl(t, tracker, "mw-f758y.8") {
		t.Fatal("expected hitl dropped once the last live step was retired")
	}
}

// A step that ran clean or was superseded is not live, so retiring the one
// that was left drops hitl; one that ran and failed is still live.
func TestHandsRetireCountsOnlyUnrunUnsupersededUnretiredStepsAsLive(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		note, value string
		wantHitl    bool
	}{
		"ran clean":  {application.HandsRanKey("mw-f758y.8", "second"), `{"at":"2026-09-28T12:03:00Z","exit":0,"host":"desktop"}`, false},
		"superseded": {application.HandsSupersededKey("mw-f758y.8", "second"), "mw-other", false},
		"ran failed": {application.HandsRanKey("mw-f758y.8", "second"), `{"at":"2026-09-28T12:03:00Z","exit":1,"host":"desktop"}`, true},
	} {
		t.Run(name, func(t *testing.T) {
			tracker := handsTracker()
			for _, step := range []domain.HandsStep{lingerStep(), secondStep()} {
				if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: step}); err != nil {
					t.Fatal(err)
				}
			}
			mustDo(t, tracker.SetNote(ctx, c.note, c.value))
			mustDo(t, retireHands(tracker, &bytes.Buffer{}, "mw-f758y.8", "linger", "done by hand"))
			if got := hasHitl(t, tracker, "mw-f758y.8"); got != c.wantHitl {
				t.Fatalf("expected hitl=%v, got %v", c.wantHitl, got)
			}
		})
	}
}

// The Governor's 'I ran step <id> myself.' retires the step through
// HandsRetire, so it drops hitl the same way once it was the last live step.
func TestRanItMyselfDropsHitlWhenItWasTheLastLiveStep(t *testing.T) {
	ctx := context.Background()
	f := newRunFixture(t)
	mustDo(t, f.tracker.SetLabels("mw-e.3", application.LabelHitl))
	for _, id := range []string{"linger", "far", "nowhere"} {
		mustDo(t, f.tracker.SetNote(ctx, application.HandsRetiredKey("mw-e.3", id), "moot"))
	}
	f.say(t, "tx-ran", "mw-e.3", "I ran step echo myself.")

	if _, err := f.inbox().Apply(ctx); err != nil {
		t.Fatalf("applying: %v", err)
	}

	if hasHitl(t, f.tracker, "mw-e.3") {
		t.Fatal("expected hitl dropped once his word retired the last live step")
	}
}
