package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

var stuckNow = time.Date(2026, 10, 19, 6, 0, 0, 0, time.UTC)

func blocks(ids ...string) []domain.BeadEdge {
	var edges []domain.BeadEdge
	for _, id := range ids {
		edges = append(edges, domain.BeadEdge{To: id, Kind: "blocks"})
	}
	return edges
}

// stuckFixture is a graph and the tracker that holds the comments and claims
// of the same beads.
type stuckFixture struct {
	graph   *apptest.FakeBeadGraph
	tracker *apptest.FakeTracker
}

func newStuckFixture() *stuckFixture {
	f := &stuckFixture{graph: &apptest.FakeBeadGraph{}, tracker: apptest.NewFakeTracker()}
	f.tracker.AddEpic("mw-s", domain.Path{})
	return f
}

// bead adds one bead to both the graph and the tracker.
func (f *stuckFixture) bead(id, status string, labels []string, needs ...string) {
	f.graph.Add(id, "Bead "+id, status, "task", labels, blocks(needs...)...)
	f.tracker.AddStory("mw-s", domain.Story{ID: id, Title: "Bead " + id})
}

func (f *stuckFixture) find(t *testing.T) application.StuckReport {
	t.Helper()
	report, err := application.StuckSurvey{
		Graph: f.graph, Comments: f.tracker, Claims: f.tracker, Now: func() time.Time { return stuckNow },
	}.Find(context.Background())
	if err != nil {
		t.Fatalf("mw stuck: %v", err)
	}
	return report
}

func rootIDs(r application.StuckReport) []string {
	var ids []string
	for _, root := range r.Roots {
		ids = append(ids, root.ID)
	}
	return ids
}

func TestStuckListsAnIcedRootWithEveryLiveBeadBehindIt(t *testing.T) {
	f := newStuckFixture()
	f.bead("mw-s.1", "deferred", []string{"icebox"})
	f.bead("mw-s.2", "open", nil, "mw-s.1")
	f.bead("mw-s.3", "open", nil, "mw-s.2")
	f.bead("mw-s.4", "closed", nil, "mw-s.1")
	f.bead("mw-s.5", "open", nil)

	report := f.find(t)
	if got := rootIDs(report); len(got) != 1 || got[0] != "mw-s.1" {
		t.Fatalf("roots = %v, want only mw-s.1", got)
	}
	root := report.Roots[0]
	if strings.Join(root.Behind, " ") != "mw-s.2 mw-s.3" {
		t.Errorf("behind = %v, want mw-s.2 and mw-s.3 (the closed one and the free one are not behind it)", root.Behind)
	}
	if !strings.Contains(root.Why, "iced") {
		t.Errorf("why = %q, want it to say iced", root.Why)
	}
	if report.LiveBehind() != 2 {
		t.Errorf("live behind = %d, want 2", report.LiveBehind())
	}
}

func TestStuckListsAHeldRootWithNoReasonOnceEveryBlockerIsClosed(t *testing.T) {
	f := newStuckFixture()
	f.bead("mw-s.0", "closed", nil)
	f.bead("mw-s.1", "deferred", nil, "mw-s.0")
	f.bead("mw-s.2", "open", nil, "mw-s.1")

	report := f.find(t)
	if got := rootIDs(report); len(got) != 1 || got[0] != "mw-s.1" {
		t.Fatalf("roots = %v, want only the reasonless held mw-s.1", got)
	}
	if !strings.Contains(report.Roots[0].Why, "no reason") {
		t.Errorf("why = %q, want it to say there is no reason", report.Roots[0].Why)
	}
}

func TestStuckLeavesAHeldBeadWithAReasonAndARootWithNothingLiveBehindIt(t *testing.T) {
	f := newStuckFixture()
	for i, label := range []string{"grilling", "hitl", "demo", "hitl:card"} {
		id := "mw-s.l" + string(rune('a'+i))
		f.bead(id, "deferred", []string{label})
		f.bead(id+"x", "open", nil, id)
	}
	// Held on a written word, not a label.
	f.bead("mw-s.w", "deferred", nil)
	f.bead("mw-s.wx", "open", nil, "mw-s.w")
	mustDo(t, f.tracker.CommentOnStory(context.Background(), "mw-s.w", "HELD: waits for his word on the colour"))
	// Held with a blocker still open: waiting, not stuck.
	f.bead("mw-s.b", "open", nil)
	f.bead("mw-s.h", "deferred", nil, "mw-s.b")
	f.bead("mw-s.hx", "open", nil, "mw-s.h")
	// Iced with nothing live behind it, and held with only closed beads behind it.
	f.bead("mw-s.i", "deferred", []string{"icebox"})
	f.bead("mw-s.c", "deferred", nil)
	f.bead("mw-s.cx", "closed", nil, "mw-s.c")

	if got := rootIDs(f.find(t)); len(got) != 0 {
		t.Fatalf("roots = %v, want none", got)
	}
}

func TestStuckListsAClaimWhoseLeaseRanOutOverSixHoursAgo(t *testing.T) {
	f := newStuckFixture()
	f.bead("mw-s.1", "open", nil)
	f.bead("mw-s.2", "open", nil, "mw-s.1")
	f.bead("mw-s.3", "open", nil)
	f.bead("mw-s.4", "open", nil, "mw-s.3")
	mustDo(t, f.tracker.SetStatus("mw-s.1", apptest.StatusInProgress))
	mustDo(t, f.tracker.SetLeaseExpires("mw-s.1", stuckNow.Add(-7*time.Hour)))
	mustDo(t, f.tracker.SetStatus("mw-s.3", apptest.StatusInProgress))
	mustDo(t, f.tracker.SetLeaseExpires("mw-s.3", stuckNow.Add(-2*time.Hour)))

	report := f.find(t)
	if got := rootIDs(report); len(got) != 1 || got[0] != "mw-s.1" {
		t.Fatalf("roots = %v, want only mw-s.1 (mw-s.3's lease ran out 2 hours ago)", got)
	}
	if !strings.Contains(report.Roots[0].Why, "claim") {
		t.Errorf("why = %q, want it to say claim", report.Roots[0].Why)
	}
}

func TestStuckSaysWhatItFoundOnePerRoot(t *testing.T) {
	f := newStuckFixture()
	f.bead("mw-s.1", "deferred", []string{"icebox"})
	f.bead("mw-s.2", "open", nil, "mw-s.1")
	said := f.find(t).String()
	for _, want := range []string{"1 live bead behind 1 held root", "mw-s.1", "iced", "mw-s.2"} {
		if !strings.Contains(said, want) {
			t.Errorf("stuck said %q, want it to hold %q", said, want)
		}
	}
	if none := newStuckFixture().find(t).String(); !strings.Contains(none, "nothing is stuck") {
		t.Errorf("with nothing stuck, said %q", none)
	}
}

func TestTheDampersSectionNamesTheStuckRootsWhenSomeLiveBeadIsBehindOne(t *testing.T) {
	report := application.ReadDampers(application.DamperFacts{
		Now: stuckNow,
		Stuck: application.StuckReport{Roots: []application.StuckRoot{
			{ID: "mw-s.1", Why: "iced", Behind: []string{"mw-s.2", "mw-s.3", "mw-s.4"}},
			{ID: "mw-s.5", Why: "held, no reason", Behind: []string{"mw-s.6"}},
		}},
	}, application.DamperLimits{})
	var b strings.Builder
	report.Write(&b)
	if want := "stuck: 4 live beads behind 2 held roots: mw-s.1 mw-s.5"; !strings.Contains(b.String(), want) {
		t.Errorf("the DAMPERS section = %q, want the line %q", b.String(), want)
	}

	quiet := application.ReadDampers(application.DamperFacts{Now: stuckNow}, application.DamperLimits{})
	b.Reset()
	quiet.Write(&b)
	if strings.Contains(b.String(), "stuck") || len(quiet.Alarms) != 0 {
		t.Errorf("with nothing stuck, the DAMPERS section = %q", b.String())
	}
}

func TestTheDampersSurveyReadsTheStuckRootsItIsGiven(t *testing.T) {
	f := newStuckFixture()
	f.bead("mw-s.1", "deferred", []string{"icebox"})
	f.bead("mw-s.2", "open", nil, "mw-s.1")
	survey := application.DamperSurvey{
		Tracker: f.tracker,
		Stuck:   application.StuckSurvey{Graph: f.graph, Comments: f.tracker, Claims: f.tracker, Now: func() time.Time { return stuckNow }},
	}
	report, err := survey.Dampers(context.Background(), application.WorkInHand{}, stuckNow)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	report.Write(&b)
	if !strings.Contains(b.String(), "stuck: 1 live bead behind 1 held root: mw-s.1") {
		t.Errorf("the DAMPERS section = %q", b.String())
	}
}
