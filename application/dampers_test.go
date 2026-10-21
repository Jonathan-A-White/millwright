package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func graphBead(id string, priority int, status string, labels []string, needs ...string) domain.GraphBead {
	return domain.GraphBead{ID: id, Title: "Bead " + id, Status: status, Type: "task", Priority: priority, Labels: labels, Edges: blocks(needs...)}
}

func dampersOf(graph []domain.GraphBead) string {
	facts := aCleanDamperFixture()
	facts.Graph = append(facts.Graph, graph...)
	var b strings.Builder
	application.ReadDampers(facts, application.DamperLimits{}).Write(&b)
	return strings.Join(strings.Fields(b.String()), " ") // the section wraps its lines
}

func TestDampersNameADependencyBelowWhatWaitsOnIt(t *testing.T) {
	// A P1 waits on a P2 which waits on a P3: both lower beads are named, each raised to P1.
	got := dampersOf([]domain.GraphBead{
		graphBead("mw-i.1", 1, "open", nil, "mw-i.2"),
		graphBead("mw-i.2", 2, "open", nil, "mw-i.3"),
		graphBead("mw-i.3", 3, "open", nil),
	})
	if !strings.Contains(got, "inversion") {
		t.Fatalf("DAMPERS has no inversion line:\n%s", got)
	}
	for _, want := range []string{"mw-i.2 P2→P1", "mw-i.3 P3→P1"} {
		if !strings.Contains(got, want) {
			t.Errorf("DAMPERS = %q, want it to hold %q", got, want)
		}
	}
	if strings.Contains(got, "mw-i.1 ") {
		t.Errorf("the P1 at the top is named: %q", got)
	}
}

func TestDampersSayNothingOfAChainWithNoInversion(t *testing.T) {
	got := dampersOf([]domain.GraphBead{
		graphBead("mw-i.1", 3, "open", nil, "mw-i.2"),
		graphBead("mw-i.2", 2, "open", nil, "mw-i.3"),
		graphBead("mw-i.3", 1, "open", nil),
		graphBead("mw-i.4", 2, "open", nil, "mw-i.5"),
		graphBead("mw-i.5", 2, "open", nil),
	})
	if strings.Contains(got, "inversion") {
		t.Errorf("an inversion line for a chain with none:\n%s", got)
	}
}

func TestDampersCountOnlyBeadsThatCanStillWait(t *testing.T) {
	// A closed P1 waiting on a P3 holds nothing up; an epic's priority is no one's.
	epic := graphBead("mw-i.5", 1, "open", nil, "mw-i.6")
	epic.Type = "epic"
	got := dampersOf([]domain.GraphBead{
		graphBead("mw-i.1", 1, "closed", nil, "mw-i.2"),
		graphBead("mw-i.2", 3, "open", nil),
		epic,
		graphBead("mw-i.6", 3, "open", nil),
	})
	if strings.Contains(got, "inversion") {
		t.Errorf("an inversion line with no live bead waiting:\n%s", got)
	}
}

func TestDampersNameAHisPriorityBeadAsHisAndProposeNoRaise(t *testing.T) {
	got := dampersOf([]domain.GraphBead{
		graphBead("mw-i.1", 1, "open", nil, "mw-i.2", "mw-i.3"),
		graphBead("mw-i.2", 3, "deferred", []string{application.HisPriorityLabel}),
		graphBead("mw-i.3", 2, "open", nil),
	})
	if !strings.Contains(got, "mw-i.2 P3 (his)") {
		t.Errorf("DAMPERS = %q, want the his-priority bead named as his", got)
	}
	if strings.Contains(got, "mw-i.2 P3→") {
		t.Errorf("a raise is proposed for his bead: %q", got)
	}
	if !strings.Contains(got, "mw-i.3 P2→P1") {
		t.Errorf("DAMPERS = %q, want the other bead raised", got)
	}
}

func TestTheInversionWalkIsTheOneMwStuckUses(t *testing.T) {
	// A bead held in the middle of a chain cuts it, as it does for mw stuck.
	got := dampersOf([]domain.GraphBead{
		graphBead("mw-i.1", 1, "open", nil, "mw-i.2"),
		graphBead("mw-i.2", 2, "deferred", nil, "mw-i.3"),
		graphBead("mw-i.3", 3, "open", nil),
	})
	if strings.Contains(got, "mw-i.3 P3") {
		t.Errorf("a bead behind a held one is named for a P1 above the hold: %q", got)
	}
	if !strings.Contains(got, "mw-i.2 P2→P1") {
		t.Errorf("DAMPERS = %q, want the held P2 named", got)
	}
}

func TestInversionListNamesEveryInversionOnALineWithWhatWaitsOnIt(t *testing.T) {
	// Twelve P3 beads each under its own P1; one of them is his.
	graph := &apptest.FakeBeadGraph{}
	for n := 1; n <= 12; n++ {
		low, top := fmt.Sprintf("mw-i.%02d", n), fmt.Sprintf("mw-t.%02d", n)
		var labels []string
		if n == 5 {
			labels = []string{application.HisPriorityLabel}
		}
		graph.Add(low, "low", "open", "task", labels)
		graph.SetPriority(low, 3)
		graph.Add(top, "top", "open", "task", nil, blocks(low)...)
		graph.SetPriority(top, 1)
	}
	got, err := application.InversionLister{Graph: graph}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 12 {
		t.Fatalf("%d lines, want 12:\n%s", len(lines), got)
	}
	for n, line := range lines {
		id := fmt.Sprintf("mw-i.%02d", n+1)
		top := fmt.Sprintf("mw-t.%02d", n+1)
		if !strings.HasPrefix(line, id+" ") || !strings.Contains(line, "P3") || !strings.Contains(line, top) {
			t.Errorf("line %d = %q, want %s, its priority and %s", n+1, line, id, top)
		}
		if n+1 == 5 {
			if !strings.Contains(line, "(his)") || strings.Contains(line, "→") {
				t.Errorf("his line = %q, want (his) and no raise", line)
			}
		} else if !strings.Contains(line, "P3→P1") || strings.Contains(line, "(his)") {
			t.Errorf("line = %q, want P3→P1 and no (his)", line)
		}
	}
}

func TestInversionListNamesTheHighestPriorityBeadThatWaitsEvenThroughAnother(t *testing.T) {
	graph := &apptest.FakeBeadGraph{}
	graph.Add("mw-i.1", "top", "open", "task", nil, blocks("mw-i.2")...)
	graph.SetPriority("mw-i.1", 0)
	graph.Add("mw-i.2", "mid", "open", "task", nil, blocks("mw-i.3")...)
	graph.SetPriority("mw-i.2", 2)
	graph.Add("mw-i.3", "low", "open", "task", nil)
	graph.SetPriority("mw-i.3", 4)
	got, err := application.InversionLister{Graph: graph}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "mw-i.3 P4→P0 waited on by mw-i.1") {
		t.Errorf("list = %q, want mw-i.3 raised to P0 for mw-i.1", got)
	}
}

func TestInversionListSaysSoWhenThereIsNone(t *testing.T) {
	graph := &apptest.FakeBeadGraph{}
	graph.Add("mw-i.1", "a", "open", "task", nil, blocks("mw-i.2")...)
	graph.Add("mw-i.2", "b", "open", "task", nil)
	got, err := application.InversionLister{Graph: graph}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "no priority inversion\n" {
		t.Errorf("list = %q, want the single line saying none", got)
	}
}

func TestInversionListReportsAGraphItCannotRead(t *testing.T) {
	graph := &apptest.FakeBeadGraph{Err: errors.New("no server")}
	if _, err := (application.InversionLister{Graph: graph}).List(context.Background()); err == nil || !strings.Contains(err.Error(), "no server") {
		t.Errorf("err = %v, want the read error", err)
	}
}

func TestDampersInversionLineEndsItsCutListWithAPointerToMwStuckInversions(t *testing.T) {
	var graph []domain.GraphBead
	for n := 1; n <= 10; n++ {
		graph = append(graph,
			graphBead(fmt.Sprintf("mw-i.%02d", n), 3, "open", nil),
			graphBead(fmt.Sprintf("mw-t.%02d", n), 1, "open", nil, fmt.Sprintf("mw-i.%02d", n)))
	}
	got := dampersOf(graph)
	if !strings.Contains(got, "+2 (all: mw stuck --inversions)") {
		t.Errorf("DAMPERS = %q, want the cut ending with a pointer to mw stuck --inversions", got)
	}
	if few := dampersOf(graph[:4]); strings.Contains(few, "mw stuck --inversions") {
		t.Errorf("a list not cut points to the command anyway: %q", few)
	}
}

// reviewFixture is the clean fixture with a hands step not run on each of the
// beads, a card on each of the cards, and the graph holding each bead as given.
func reviewFixture(t *testing.T, statuses map[string]string, steps, cards []string) application.DamperFacts {
	t.Helper()
	facts := aCleanDamperFixture()
	for _, bead := range steps {
		raw, _ := json.Marshal([]application.HandsStepRecord{{HandsStep: domain.HandsStep{ID: "a", Host: "desktop", As: "user", Run: "true"}}})
		facts.Notes[application.HandsStepsKey(bead)] = string(raw)
	}
	for _, bead := range cards {
		facts.Notes[application.PosternQuestionKey(bead)] = `{"q":"which?"}`
	}
	for bead, status := range statuses {
		facts.Graph = append(facts.Graph, domain.GraphBead{ID: bead, Title: "Bead " + bead, Status: status, Type: "task"})
	}
	return facts
}

func TestReviewQueueCountsAHandsStepOnlyWhileItsBeadIsOpen(t *testing.T) {
	facts := reviewFixture(t, map[string]string{"mw-h.closed": "closed", "mw-h.open": "open"}, []string{"mw-h.closed", "mw-h.open"}, nil)
	report := application.ReadDampers(facts, application.DamperLimits{ReviewQueue: 1})
	// the clean fixture holds 2 (a card and a demo); the open bead's step makes 3
	if report.Counts.ReviewQueue != 3 {
		t.Errorf("ReviewQueue = %d, want 3: the closed bead's step is not counted", report.Counts.ReviewQueue)
	}
	var review application.DamperAlarm
	for _, alarm := range report.Alarms {
		if alarm.Principle == application.DamperReview {
			review = alarm
		}
	}
	if slices.Contains(review.Beads, "mw-h.closed") || !slices.Contains(review.Beads, "mw-h.open") {
		t.Errorf("review alarm names %v, want mw-h.open and not mw-h.closed", review.Beads)
	}
	if !strings.Contains(review.Text, "1 hands steps") {
		t.Errorf("review alarm says %q, want 1 hands steps", review.Text)
	}
}

func TestReviewQueueCountsACardOnlyWhileItsBeadIsOpen(t *testing.T) {
	facts := reviewFixture(t, map[string]string{"mw-c.closed": "closed", "mw-c.open": "in_progress"}, nil, []string{"mw-c.closed", "mw-c.open"})
	report := application.ReadDampers(facts, application.DamperLimits{ReviewQueue: 1})
	if report.Counts.ReviewQueue != 3 {
		t.Errorf("ReviewQueue = %d, want 3: the closed bead's card is not counted", report.Counts.ReviewQueue)
	}
}

func TestReviewQueueCountsABeadTheGraphDoesNotHold(t *testing.T) {
	facts := reviewFixture(t, nil, []string{"mw-h.unknown"}, nil)
	if got := application.ReadDampers(facts, application.DamperLimits{}).Counts.ReviewQueue; got != 3 {
		t.Errorf("ReviewQueue = %d, want 3: a bead the graph does not show is counted", got)
	}
}

func TestDamperAlarmHintNamesACommandThatPrintsTheWholeList(t *testing.T) {
	var cards []string
	for n := 1; n <= 12; n++ {
		cards = append(cards, fmt.Sprintf("mw-q.%02d", n))
	}
	facts := reviewFixture(t, nil, nil, cards)
	report := application.ReadDampers(facts, application.DamperLimits{})
	var b strings.Builder
	report.Write(&b)
	got := strings.Join(strings.Fields(b.String()), " ")
	if !strings.Contains(got, "(all: mw stuck --dampers)") || strings.Contains(got, "mw stuck --inversions") {
		t.Errorf("DAMPERS = %q, want the cut ending with a pointer to mw stuck --dampers", got)
	}
	full := report.FullList()
	for _, bead := range cards {
		if !strings.Contains(full, bead+"\n") {
			t.Errorf("FullList lacks %s:\n%s", bead, full)
		}
	}
	if !strings.Contains(full, "review") {
		t.Errorf("FullList does not name the alarm:\n%s", full)
	}
	if got := (application.DampersReport{}).FullList(); !strings.Contains(got, "no damper alarm") {
		t.Errorf("FullList with none = %q, want it to say so", got)
	}
}
