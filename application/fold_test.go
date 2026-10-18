package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// foldTracker is a rig lampas with bugs of two classes, three on one screen,
// one running, one held, a closed one, an iced one, a plain story and a bug of
// another rig.
func foldTracker(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "laptop", Formula: "tdd-feature"})
	tracker.AddEpic("mw-o", domain.Path{Rig: "other", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "laptop", Formula: "tdd-feature"})
	bug := func(epic, id, title, status string, labels ...string) {
		tracker.AddStory(epic, domain.Story{ID: id, Title: title})
		if err := tracker.SetLabels(id, append([]string{application.LabelBug}, labels...)...); err != nil {
			t.Fatal(err)
		}
		if status != "" {
			if err := tracker.SetStatus(id, status); err != nil {
				t.Fatal(err)
			}
		}
	}
	bug("mw-e", "mw-e.1", "[bug] lampas: Mic stays red", "", "class:stale-state")
	bug("mw-e", "mw-e.2", "[bug] lampas: Page lost after rotate", apptest.StatusDeferred, "class:stale-state")
	bug("mw-e", "mw-e.3", "[bug] lampas: Title is stale", apptest.StatusInProgress, "class:stale-state")
	bug("mw-e", "mw-e.4", "[bug] lampas: Lampas Review: card overflows", "")
	bug("mw-e", "mw-e.5", "[bug] lampas: Lampas Review: score hidden", apptest.StatusDeferred)
	bug("mw-e", "mw-e.6", "Lampas Review: stats clipped", "")
	bug("mw-e", "mw-e.7", "[bug] lampas: Settings: toggle sticks", "")
	bug("mw-e", "mw-e.8", "[bug] lampas: Mic stays red again", apptest.StatusClosed, "class:stale-state")
	bug("mw-e", "mw-e.9", "[bug] lampas: Iced thing", apptest.StatusDeferred, "class:stale-state", application.LabelIcebox)
	bug("mw-o", "mw-o.1", "[bug] other: Mic stays red", "", "class:stale-state")
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.10", Title: "Lampas Review: a feature"})
	return tracker
}

func runFold(t *testing.T, tracker *apptest.FakeTracker, min int) (application.FoldReport, string) {
	t.Helper()
	var out bytes.Buffer
	report, err := application.Fold{Tracker: tracker, Rig: "lampas", Min: min, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report, out.String()
}

func groupIDs(g application.FoldGroup) []string {
	var ids []string
	for _, b := range g.Bugs {
		ids = append(ids, b.ID)
	}
	return ids
}

func TestFoldGroupsByClassAndByScreenLargestFirst(t *testing.T) {
	report, out := runFold(t, foldTracker(t), 3)
	if len(report.Groups) != 2 {
		t.Fatalf("expected two groups of 3, got %+v", report.Groups)
	}
	first, second := report.Groups[0], report.Groups[1]
	if first.Key != "class:stale-state" || strings.Join(groupIDs(first), ",") != "mw-e.1,mw-e.2,mw-e.3" {
		t.Errorf("expected the class group first, got %+v", first)
	}
	if second.Key != "screen:Lampas Review" || strings.Join(groupIDs(second), ",") != "mw-e.4,mw-e.5,mw-e.6" {
		t.Errorf("expected the screen group second, got %+v", second)
	}
	for _, want := range []string{"class:stale-state (3)", "Lampas Review (3)", "mw-e.2", "Page lost after rotate", "mw-e.6"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"mw-e.8", "mw-e.9", "mw-o.1", "mw-e.10", "Settings"} {
		if strings.Contains(out, not) {
			t.Errorf("output must not name %q:\n%s", not, out)
		}
	}
}

func TestFoldMarksTheBugInProgress(t *testing.T) {
	_, out := runFold(t, foldTracker(t), 3)
	var marked, others int
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "mw-e.3"):
			if !strings.Contains(line, "in progress: do not fold") {
				t.Errorf("the running bug is not marked: %q", line)
			}
			marked++
		case strings.Contains(line, "in progress"):
			others++
		}
	}
	if marked != 1 || others != 0 {
		t.Errorf("expected only mw-e.3 marked, got %d marked and %d others:\n%s", marked, others, out)
	}
}

func TestFoldHonoursMin(t *testing.T) {
	report, _ := runFold(t, foldTracker(t), 4)
	if len(report.Groups) != 0 {
		t.Errorf("expected no group of 4, got %+v", report.Groups)
	}
	tracker := foldTracker(t)
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.11", Title: "[bug] lampas: Settings: second toggle"})
	if err := tracker.SetLabels("mw-e.11", application.LabelBug); err != nil {
		t.Fatal(err)
	}
	report, out := runFold(t, tracker, 2)
	if len(report.Groups) != 3 {
		t.Fatalf("expected three groups of 2 or more, got %+v", report.Groups)
	}
	if report.Groups[2].Key != "screen:Settings" || !strings.Contains(out, "Settings (2)") {
		t.Errorf("expected Settings last, got %+v", report.Groups)
	}
}

func TestFoldWritesNothingAndDefaultsMinToThree(t *testing.T) {
	tracker := foldTracker(t)
	before, _ := tracker.ShowStory(context.Background(), "mw-e.1")
	report, _ := runFold(t, tracker, 0)
	if len(report.Groups) != 2 {
		t.Errorf("expected min to default to 3, got %+v", report.Groups)
	}
	after, _ := tracker.ShowStory(context.Background(), "mw-e.1")
	if before.Status != after.Status || len(before.Labels) != len(after.Labels) {
		t.Errorf("fold changed a story: %+v -> %+v", before, after)
	}
}

func TestFoldNamesTheRuleAndAnEmptyResult(t *testing.T) {
	_, out := runFold(t, foldTracker(t), 9)
	if !strings.Contains(out, "no group of 9 or more") {
		t.Errorf("expected a plain empty answer, got:\n%s", out)
	}
	if !strings.Contains(application.FoldRule, "copied into the fold's acceptance criteria") {
		t.Errorf("the fold rule is not stated: %q", application.FoldRule)
	}
}

// A fold names itself '<screen> fold:' after the app, as 'Lampas fold' does; it
// carries its folded bugs as blockers and is not a bug of their screen.
func TestFoldLeavesOutAStoryThatIsItselfAFold(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "laptop", Formula: "tdd-feature"})
	bug := func(id, title string) {
		tracker.AddStory("mw-e", domain.Story{ID: id, Title: title})
		if err := tracker.SetLabels(id, application.LabelBug); err != nil {
			t.Fatal(err)
		}
	}
	bug("mw-e.1", "[bug] lampas: Lampas Review: card overflows")
	bug("mw-e.2", "[bug] lampas: Lampas Review: score hidden")
	bug("mw-e.3", "[bug] lampas: Lampas Review: stats clipped")
	bug("mw-e.4", "[bug] lampas: Lampas Review fold: card, score and stats")
	bug("mw-e.5", "Lampas fold: three more")
	bug("mw-e.6", "[bug] lampas: Lampas fold: Review layout")
	report, out := runFold(t, tracker, 3)
	if len(report.Groups) != 1 {
		t.Fatalf("expected one group, got %+v", report.Groups)
	}
	if got := strings.Join(groupIDs(report.Groups[0]), ","); got != "mw-e.1,mw-e.2,mw-e.3" {
		t.Errorf("expected the three bugs and no fold, got %s", got)
	}
	if report.Bugs != 3 {
		t.Errorf("a fold is not counted among the bugs, got %d", report.Bugs)
	}
	if strings.Contains(out, "fold:") || strings.Contains(out, "screen:Lampas fold") {
		t.Errorf("output lists a fold:\n%s", out)
	}
}

// A bug that already waits on a fold story is folded: the screen it names is
// not listed as a group while two of its three bugs wait on a fold (mw-gq6.591).
func TestFoldLeavesOutABugThatAlreadyWaitsOnAFold(t *testing.T) {
	build := func(waiting bool) *apptest.FakeTracker {
		tracker := apptest.NewFakeTracker()
		tracker.AddEpic("mw-e", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "laptop", Formula: "tdd-feature"})
		bug := func(id, title string) {
			tracker.AddStory("mw-e", domain.Story{ID: id, Title: title})
			if err := tracker.SetLabels(id, application.LabelBug); err != nil {
				t.Fatal(err)
			}
		}
		bug("mw-e.1", "[bug] lampas: Talk: mic stays red")
		bug("mw-e.2", "[bug] lampas: Talk: page lost")
		bug("mw-e.3", "[bug] lampas: Talk: title stale")
		bug("mw-e.4", "[bug] lampas: Lampas fold: Talk")
		bug("mw-e.5", "[bug] lampas: Settings: toggle sticks")
		if waiting {
			tracker.Needs("mw-e.1", "mw-e.4")
			tracker.Needs("mw-e.2", "mw-e.4")
			// a link to a story that is no fold is not a fold
			tracker.Needs("mw-e.3", "mw-e.5")
		}
		return tracker
	}
	report, out := runFold(t, build(true), 3)
	if len(report.Groups) != 0 {
		t.Errorf("expected no group while two of three bugs wait on a fold, got %+v\n%s", report.Groups, out)
	}
	report, _ = runFold(t, build(false), 3)
	if len(report.Groups) != 1 || report.Groups[0].Key != "screen:Talk" || len(report.Groups[0].Bugs) != 3 {
		t.Errorf("expected Talk as a group of 3 with all three free, got %+v", report.Groups)
	}
	// a bug that waits on a fold already closed is free again
	closed := build(true)
	if err := closed.SetStatus("mw-e.4", apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	report, _ = runFold(t, closed, 3)
	if len(report.Groups) != 1 || len(report.Groups[0].Bugs) != 3 {
		t.Errorf("expected Talk back as a group once its fold is closed, got %+v", report.Groups)
	}
}
