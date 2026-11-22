package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

// pastReasonFixture is two epics with held stories, one closed blocker and one
// open one.
func pastReasonFixture(t *testing.T) *stuckFixture {
	t.Helper()
	f := newStuckFixture()
	f.graph.Add("mw-e1", "Epic one", "open", "epic", nil)
	f.graph.Add("mw-e2", "Epic two", "open", "epic", nil)
	parent := func(epic string) []domain.BeadEdge {
		return []domain.BeadEdge{{To: epic, Kind: domain.EdgeParentChild}}
	}
	f.graph.Add("mw-e1.1", "done blocker", "closed", "task", nil, parent("mw-e1")...)
	f.graph.Add("mw-e1.2", "open blocker", "open", "task", nil, parent("mw-e1")...)
	// Held on 'HELD:', names the closed bead: past its reason.
	f.graph.Add("mw-e1.3", "past", "deferred", "task", nil, parent("mw-e1")...)
	f.tracker.AddStory("mw-s", domain.Story{ID: "mw-e1.3", Title: "past"})
	mustDo(t, f.tracker.CommentOnStory(t.Context(), "mw-e1.3", "HELD: waits on mw-e1.1 and the file it makes"))
	// 'HELD by <who>:', names the closed bead, in another epic.
	f.graph.Add("mw-e2.1", "past too", "deferred", "task", nil, parent("mw-e2")...)
	f.tracker.AddStory("mw-s", domain.Story{ID: "mw-e2.1", Title: "past too"})
	mustDo(t, f.tracker.CommentOnStory(t.Context(), "mw-e2.1", "HELD by the Mayor 921: waits for mw-e1.1"))
	// Names a bead still open: not listed.
	f.graph.Add("mw-e1.4", "still waiting", "deferred", "task", nil, parent("mw-e1")...)
	f.tracker.AddStory("mw-s", domain.Story{ID: "mw-e1.4", Title: "still waiting"})
	mustDo(t, f.tracker.CommentOnStory(t.Context(), "mw-e1.4", "HELD: waits on mw-e1.2"))
	// Names one closed and one open bead: still held behind the open one.
	f.graph.Add("mw-e1.5", "half", "deferred", "task", nil, parent("mw-e1")...)
	f.tracker.AddStory("mw-s", domain.Story{ID: "mw-e1.5", Title: "half"})
	mustDo(t, f.tracker.CommentOnStory(t.Context(), "mw-e1.5", "HELD: waits on mw-e1.1 and mw-e1.2"))
	// A hold that names no bead: not listed.
	f.graph.Add("mw-e1.6", "his word", "deferred", "task", nil, parent("mw-e1")...)
	f.tracker.AddStory("mw-s", domain.Story{ID: "mw-e1.6", Title: "his word"})
	mustDo(t, f.tracker.CommentOnStory(t.Context(), "mw-e1.6", "HELD: waits for his word on the colour"))
	// A comment that is not a hold's reason but names a closed bead: not listed.
	f.graph.Add("mw-e1.7", "chatter", "deferred", "task", nil, parent("mw-e1")...)
	f.tracker.AddStory("mw-s", domain.Story{ID: "mw-e1.7", Title: "chatter"})
	mustDo(t, f.tracker.CommentOnStory(t.Context(), "mw-e1.7", "see mw-e1.1"))
	// An open story that names a closed bead in a hold comment: only held ones count.
	f.graph.Add("mw-e1.8", "released", "open", "task", nil, parent("mw-e1")...)
	f.tracker.AddStory("mw-s", domain.Story{ID: "mw-e1.8", Title: "released"})
	mustDo(t, f.tracker.CommentOnStory(t.Context(), "mw-e1.8", "HELD: waits on mw-e1.1"))
	return f
}

func TestStuckPastReasonListsHeldStoriesWhoseNamedBeadClosedGroupedByEpic(t *testing.T) {
	report := pastReasonFixture(t).find(t)
	var got []string
	for _, group := range report.PastReason {
		for _, hold := range group.Holds {
			got = append(got, group.Epic+"/"+hold.ID+"/"+strings.Join(hold.Closed, ","))
		}
	}
	want := "mw-e1/mw-e1.3/mw-e1.1 mw-e2/mw-e2.1/mw-e1.1"
	if strings.Join(got, " ") != want {
		t.Fatalf("past reason = %v, want %s", got, want)
	}
	if report.PastReasonCount() != 2 {
		t.Errorf("count = %d, want 2", report.PastReasonCount())
	}
}

func TestStuckPastReasonIsPrintedWithItsCount(t *testing.T) {
	out := pastReasonFixture(t).find(t).String()
	for _, want := range []string{"past their reason: 2 held stories", "mw-e1 (1)", "mw-e2 (1)", "mw-e1.3", "mw-e2.1", "mw-e1.1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"mw-e1.4", "mw-e1.5", "mw-e1.6", "mw-e1.7", "mw-e1.8", "nothing is stuck"} {
		if strings.Contains(out, not) {
			t.Errorf("output has %q:\n%s", not, out)
		}
	}
}

func TestStuckPastReasonLeavesAnIcedStoryAndAnUnparentedOneInItsOwnGroup(t *testing.T) {
	f := newStuckFixture()
	f.graph.Add("mw-x.1", "closed", "closed", "task", nil)
	f.graph.Add("mw-x.2", "iced", "deferred", "task", []string{"icebox"})
	f.tracker.AddStory("mw-s", domain.Story{ID: "mw-x.2", Title: "iced"})
	mustDo(t, f.tracker.CommentOnStory(t.Context(), "mw-x.2", "HELD: Iced: after mw-x.1"))
	f.graph.Add("mw-x.3", "loose", "deferred", "task", nil)
	f.tracker.AddStory("mw-s", domain.Story{ID: "mw-x.3", Title: "loose"})
	mustDo(t, f.tracker.CommentOnStory(t.Context(), "mw-x.3", "HELD: after mw-x.1"))
	report := f.find(t)
	if report.PastReasonCount() != 1 || report.PastReason[0].Holds[0].ID != "mw-x.3" {
		t.Fatalf("past reason = %+v, want only mw-x.3", report.PastReason)
	}
	if report.PastReason[0].Epic != "" {
		t.Errorf("epic = %q, want none", report.PastReason[0].Epic)
	}
	if out := report.String(); !strings.Contains(out, "no epic") {
		t.Errorf("output does not say 'no epic':\n%s", out)
	}
}
