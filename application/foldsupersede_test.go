package application_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// loggedFoldTracker records the order of the writes a supersede makes.
type loggedFoldTracker struct {
	*apptest.FakeTracker
	log []string
}

func (l *loggedFoldTracker) HoldStory(ctx context.Context, id string) error {
	l.log = append(l.log, "hold "+id)
	return l.FakeTracker.HoldStory(ctx, id)
}

func (l *loggedFoldTracker) ReleaseStory(ctx context.Context, id string) error {
	l.log = append(l.log, "release "+id)
	return l.FakeTracker.ReleaseStory(ctx, id)
}

func (l *loggedFoldTracker) AddBlocker(ctx context.Context, blocked, blocker string) error {
	l.log = append(l.log, "add "+blocked+" "+blocker)
	return l.FakeTracker.AddBlocker(ctx, blocked, blocker)
}

func (l *loggedFoldTracker) RemoveBlocker(ctx context.Context, blocked, blocker string) error {
	l.log = append(l.log, "remove "+blocked+" "+blocker)
	return l.FakeTracker.RemoveBlocker(ctx, blocked, blocker)
}

func (l *loggedFoldTracker) SupersedeStory(ctx context.Context, id, with string) error {
	l.log = append(l.log, "supersede "+id+" "+with)
	return l.FakeTracker.SupersedeStory(ctx, id, with)
}

// supersedeTracker is a rig lampas with the fold mw-f carrying six bugs: b.1 and
// b.2 are eligible (b.2 has an outside story d.1 waiting on it, and b.1 waits on
// the fold as the fold rule makes it), b.3 is running, b.4 is closed, b.5 is
// missing from the fold's acceptance criteria, and b.6 has b.1 waiting on it.
func supersedeTracker(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "laptop", Formula: "tdd-feature"})
	add := func(id, title string) {
		tracker.AddStory("mw-e", domain.Story{ID: id, Title: title})
	}
	add("mw-f", "[bug] lampas: Lampas fold: Talk")
	for i := 1; i <= 6; i++ {
		add(fmt.Sprintf("mw-b.%d", i), fmt.Sprintf("[bug] lampas: Talk: fault %d", i))
	}
	add("mw-d.1", "Lampas: a story that waits on b.2")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(tracker.SetDescription("mw-f", "Folded bugs (each keeps its own check):\n\n"+
		"• mw-b.1: [bug] lampas: Talk: fault 1\n• mw-b.2: [bug] lampas: Talk: fault 2\n• mw-b.3: [bug] lampas: Talk: fault 3\n"+
		"• mw-b.4: [bug] lampas: Talk: fault 4\n• mw-b.5: [bug] lampas: Talk: fault 5\n• mw-b.6: [bug] lampas: Talk: fault 6\n\n"+
		"Folded on the Governor's word."))
	must(tracker.SetAcceptance("mw-f", "1. Each folded bug's own check passes:\n• mw-b.1: npx vitest run a\n• mw-b.2: npx vitest run b\n"+
		"• mw-b.3: npx vitest run c\n• mw-b.4: npx vitest run d\n• mw-b.6: npx vitest run f\n"))
	must(tracker.SetStatus("mw-b.3", apptest.StatusInProgress))
	must(tracker.SetStatus("mw-b.4", apptest.StatusClosed))
	tracker.Needs("mw-b.1", "mw-f", "mw-b.6")
	tracker.Needs("mw-b.2", "mw-f")
	tracker.Needs("mw-d.1", "mw-b.2")
	return tracker
}

func runSupersede(t *testing.T, tracker application.FoldWriter, dry bool) (application.FoldSupersedeReport, string) {
	t.Helper()
	var out bytes.Buffer
	report, err := application.FoldSupersede{Tracker: tracker, Fold: "mw-f", DryRun: dry, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return report, out.String()
}

func lineWith(out, needle string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func status(t *testing.T, tracker *apptest.FakeTracker, id string) string {
	t.Helper()
	d, err := tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d.Status
}

func TestFoldSupersedeClosesTheEligibleBugsAndMovesTheirDependents(t *testing.T) {
	tracker := supersedeTracker(t)
	_, out := runSupersede(t, tracker, false)
	for _, id := range []string{"mw-b.1", "mw-b.2", "mw-b.6"} {
		if status(t, tracker, id) != apptest.StatusClosed || tracker.SupersededBy(id) != "mw-f" {
			t.Errorf("%s is not closed as superseded by the fold (status %s, by %q)\n%s", id, status(t, tracker, id), tracker.SupersededBy(id), out)
		}
	}
	for id, want := range map[string]string{"mw-b.3": apptest.StatusInProgress, "mw-b.4": apptest.StatusClosed, "mw-b.5": apptest.StatusOpen} {
		if got := status(t, tracker, id); got != want {
			t.Errorf("%s changed: status %s, want %s", id, got, want)
		}
		if tracker.SupersededBy(id) != "" {
			t.Errorf("%s was superseded", id)
		}
	}
	d, _ := tracker.ShowStory(context.Background(), "mw-d.1")
	if d.Status != apptest.StatusOpen {
		t.Errorf("the dependent was left %s, want it put back to open", d.Status)
	}
	all, _ := tracker.StoriesSince(context.Background(), time.Time{})
	for _, s := range all {
		if s.Story.ID == "mw-d.1" && strings.Join(s.Needs, ",") != "mw-b.2,mw-f" {
			t.Errorf("the dependent waits on %v, want mw-b.2 and mw-f", s.Needs)
		}
	}
	for needle, want := range map[string]string{"mw-b.3": "in progress", "mw-b.4": "already closed", "mw-b.5": "not in the fold's acceptance criteria"} {
		if line := lineWith(out, needle); !strings.Contains(line, want) {
			t.Errorf("the line for %s lacks %q: %q\n%s", needle, want, line, out)
		}
	}
	if line := lineWith(out, "mw-b.2"); !strings.Contains(line, "mw-d.1") {
		t.Errorf("the line for mw-b.2 does not name the dependent it moved: %q", line)
	}
	if line := lineWith(out, "mw-b.1"); strings.Contains(line, "mw-b.6") {
		t.Errorf("a bug of the same fold waiting on b.6 is moved, nothing to say: %q", line)
	}
}

func TestFoldSupersedeHoldsAnOpenDependentWhileItMovesIt(t *testing.T) {
	tracker := &loggedFoldTracker{FakeTracker: supersedeTracker(t)}
	runSupersede(t, tracker, false)
	log := strings.Join(tracker.log, "|")
	if !strings.Contains(log, "hold mw-d.1|add mw-d.1 mw-f|release mw-d.1") {
		t.Errorf("the dependent is not held around the move: %s", log)
	}
	// a bug that already waits on the fold has the link removed before it is superseded
	if !strings.Contains(log, "remove mw-b.2 mw-f|supersede mw-b.2 mw-f") {
		t.Errorf("the link to the fold is not removed before the supersede: %s", log)
	}
}

func TestFoldSupersedeMovesABugOfAnotherFoldAsItsFoldWaiting(t *testing.T) {
	tracker := supersedeTracker(t)
	tracker.AddStory("mw-e", domain.Story{ID: "mw-g", Title: "[bug] lampas: Lampas fold: Settings"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-c.1", Title: "[bug] lampas: Settings: fault"})
	if err := tracker.SetDescription("mw-g", "Folded bugs:\n\n• mw-c.1: [bug] lampas: Settings: fault\n"); err != nil {
		t.Fatal(err)
	}
	tracker.Needs("mw-c.1", "mw-g", "mw-b.2")
	_, out := runSupersede(t, tracker, false)
	all, _ := tracker.StoriesSince(context.Background(), time.Time{})
	needs := map[string][]string{}
	for _, s := range all {
		needs[s.Story.ID] = s.Needs
	}
	if strings.Join(needs["mw-g"], ",") != "mw-f" {
		t.Errorf("the other fold waits on %v, want mw-f\n%s", needs["mw-g"], out)
	}
	if strings.Join(needs["mw-c.1"], ",") != "mw-g,mw-b.2" {
		t.Errorf("a bug of another fold was moved itself: %v", needs["mw-c.1"])
	}
}

func TestFoldSupersedeLeavesOutAnEdgeThatWouldMakeACycle(t *testing.T) {
	tracker := supersedeTracker(t)
	tracker.Needs("mw-f", "mw-d.1")
	_, out := runSupersede(t, tracker, false)
	if status(t, tracker, "mw-b.2") != apptest.StatusClosed {
		t.Errorf("the bug is not superseded for want of one edge\n%s", out)
	}
	all, _ := tracker.StoriesSince(context.Background(), time.Time{})
	for _, s := range all {
		if s.Story.ID == "mw-d.1" && strings.Contains(strings.Join(s.Needs, ","), "mw-f") {
			t.Errorf("the edge was forced: %v", s.Needs)
		}
	}
	if line := lineWith(out, "mw-b.2"); !strings.Contains(line, "cycle") || !strings.Contains(line, "mw-d.1") {
		t.Errorf("the line does not say the edge was left out: %q", line)
	}
}

func TestFoldSupersedeDryRunWritesNothingAndPrintsThePlan(t *testing.T) {
	tracker := supersedeTracker(t)
	before := tracker.Writes()
	_, dryOut := runSupersede(t, tracker, true)
	if tracker.Writes() != before {
		t.Errorf("a dry run wrote %d times", tracker.Writes()-before)
	}
	for _, id := range []string{"mw-b.1", "mw-b.2", "mw-b.6"} {
		if status(t, tracker, id) != apptest.StatusOpen {
			t.Errorf("%s was changed by a dry run", id)
		}
	}
	_, realOut := runSupersede(t, supersedeTracker(t), false)
	norm := func(s string) string {
		s = strings.ReplaceAll(s, "would be superseded", "superseded")
		s = strings.ReplaceAll(s, "would move", "moved")
		if strings.HasPrefix(s, "dry run:") {
			_, s, _ = strings.Cut(s, "\n")
		}
		return s
	}
	if norm(dryOut) != norm(realOut) {
		t.Errorf("the dry run does not print the same plan:\n--- dry\n%s\n--- real\n%s", dryOut, realOut)
	}
	if !strings.Contains(dryOut, "would be superseded") {
		t.Errorf("a dry run does not say it would: %s", dryOut)
	}
}

func TestFoldSupersedeNeedsAFoldWithFoldedBugs(t *testing.T) {
	tracker := supersedeTracker(t)
	if err := tracker.SetDescription("mw-f", "nothing folded here"); err != nil {
		t.Fatal(err)
	}
	_, err := application.FoldSupersede{Tracker: tracker, Fold: "mw-f", Out: &bytes.Buffer{}}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Folded bugs") {
		t.Errorf("expected a refusal naming Folded bugs, got %v", err)
	}
}

func TestFoldRuleSaysTheBugIsClosedAsSuperseded(t *testing.T) {
	if !strings.Contains(application.FoldRule, "copied into the fold's acceptance criteria") || !strings.Contains(application.FoldRule, "superseded by the fold") {
		t.Errorf("the fold rule is out of date: %q", application.FoldRule)
	}
}
