package domain_test

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

// bead is a bead of the graph with its edges, as the tests below build them.
func bead(id, title, status, kind string, labels []string, edges ...domain.BeadEdge) domain.GraphBead {
	return domain.GraphBead{ID: id, Title: title, Status: status, Type: kind, Labels: labels, Edges: edges}
}

func childOf(parent string) domain.BeadEdge {
	return domain.BeadEdge{To: parent, Kind: domain.EdgeParentChild}
}

func linkedTo(other, kind string) domain.BeadEdge { return domain.BeadEdge{To: other, Kind: kind} }

func finished(t *testing.T, beads ...domain.GraphBead) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, f := range domain.FinishedStillOpen(beads) {
		got[f.ID] = f.Why
	}
	return got
}

func TestOpenEpicWithEveryChildClosedIsListed(t *testing.T) {
	got := finished(t,
		bead("e-1", "An epic", "open", "epic", nil),
		bead("e-1.1", "one", "closed", "task", nil, childOf("e-1")),
		bead("e-1.2", "two", "closed", "task", nil, childOf("e-1")),
	)
	want := map[string]string{"e-1": "all 2 children closed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
}

func TestOpenEpicWithAnOpenChildIsNotListed(t *testing.T) {
	got := finished(t,
		bead("e-1", "An epic", "open", "epic", nil),
		bead("e-1.1", "one", "closed", "task", nil, childOf("e-1")),
		bead("e-1.2", "two", "in_progress", "task", nil, childOf("e-1")),
	)
	if len(got) != 0 {
		t.Fatalf("listed %v, want nothing", got)
	}
}

func TestAHeldChildIsNotFinished(t *testing.T) {
	got := finished(t,
		bead("e-1", "An epic", "open", "epic", nil),
		bead("e-1.1", "one", "deferred", "task", nil, childOf("e-1")),
	)
	if len(got) != 0 {
		t.Fatalf("listed %v, want nothing", got)
	}
}

func TestAnEpicWithNoChildrenIsNotListed(t *testing.T) {
	if got := finished(t, bead("e-1", "An epic", "open", "epic", nil)); len(got) != 0 {
		t.Fatalf("listed %v, want nothing", got)
	}
}

func TestAClosedOrHeldEpicIsNotListed(t *testing.T) {
	got := finished(t,
		bead("e-1", "Closed", "closed", "epic", nil),
		bead("e-1.1", "one", "closed", "task", nil, childOf("e-1")),
		bead("e-2", "Held", "deferred", "epic", nil),
		bead("e-2.1", "one", "closed", "task", nil, childOf("e-2")),
	)
	if len(got) != 0 {
		t.Fatalf("listed %v, want nothing", got)
	}
}

func TestAMapByLabelOrTitleIsListedWhateverItsType(t *testing.T) {
	got := finished(t,
		bead("m-1", "Some map", "open", "task", []string{"wayfinder:map"}),
		bead("m-1.1", "one", "closed", "task", nil, childOf("m-1")),
		bead("m-2", "Map of the thing", "in_progress", "task", nil),
		bead("m-2.1", "one", "closed", "task", nil, childOf("m-2")),
	)
	want := map[string]string{"m-1": "all 1 children closed", "m-2": "all 1 children closed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
}

func TestAPlainTaskWithClosedChildrenIsNotListed(t *testing.T) {
	got := finished(t,
		bead("t-1", "A task", "open", "task", nil),
		bead("t-1.1", "one", "closed", "task", nil, childOf("t-1")),
	)
	if len(got) != 0 {
		t.Fatalf("listed %v, want nothing", got)
	}
}

func grilling(edges ...domain.BeadEdge) domain.GraphBead {
	return bead("g-1", "Grilling: what now?", "open", "task", []string{"wayfinder:grilling"}, edges...)
}

func TestGrillingRelatedToTwoClosedEpicsIsListed(t *testing.T) {
	got := finished(t,
		grilling(linkedTo("e-1", domain.EdgeRelated), linkedTo("e-2", domain.EdgeBlocks)),
		bead("e-1", "one", "closed", "epic", nil),
		bead("e-2", "two", "closed", "epic", nil),
	)
	want := map[string]string{"g-1": "its epics e-1, e-2 closed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
}

func TestGrillingRelatedToAClosedAndAnOpenEpicIsNotListed(t *testing.T) {
	got := finished(t,
		grilling(linkedTo("e-1", domain.EdgeRelated), linkedTo("e-2", domain.EdgeRelated)),
		bead("e-1", "one", "closed", "epic", nil),
		bead("e-2", "two", "open", "epic", nil),
	)
	if len(got) != 0 {
		t.Fatalf("listed %v, want nothing", got)
	}
}

func TestGrillingWithNoLinksIsNotListed(t *testing.T) {
	if got := finished(t, grilling()); len(got) != 0 {
		t.Fatalf("listed %v, want nothing", got)
	}
}

func TestATicketIsKnownByItsTitleToo(t *testing.T) {
	got := finished(t,
		bead("r-1", "Research: how?", "open", "task", nil, linkedTo("e-1", domain.EdgeDiscoveredFrom)),
		bead("e-1", "one", "closed", "epic", nil),
	)
	want := map[string]string{"r-1": "its epics e-1 closed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
}

func TestALinkFromTheEpicsSideCounts(t *testing.T) {
	got := finished(t,
		grilling(),
		bead("e-1", "one", "closed", "epic", nil, linkedTo("g-1", domain.EdgeBlocks)),
	)
	want := map[string]string{"g-1": "its epics e-1 closed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
}

func TestAnEpicFiledUnderATicketCountsButTheTicketsOwnMapDoesNot(t *testing.T) {
	// The map a ticket is filed under is open for as long as the map is
	// worked, so it is not one of the ticket's epics; an epic filed under the
	// ticket is.
	got := finished(t,
		grilling(childOf("map-1")),
		bead("map-1", "The map", "open", "epic", nil),
		bead("e-1", "one", "closed", "epic", nil, childOf("g-1")),
	)
	want := map[string]string{"g-1": "its epics e-1 closed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
}

func TestATicketLinkedOnlyToItsMapIsNotListed(t *testing.T) {
	got := finished(t,
		grilling(childOf("map-1")),
		bead("map-1", "The map", "closed", "epic", nil),
	)
	if len(got) != 0 {
		t.Fatalf("listed %v, want nothing", got)
	}
}

func TestLinksToBeadsThatAreNotEpicsAreNotItsEpics(t *testing.T) {
	got := finished(t,
		grilling(linkedTo("t-1", domain.EdgeRelated), linkedTo("e-1", domain.EdgeRelated)),
		bead("t-1", "a task", "open", "task", nil),
		bead("e-1", "one", "closed", "epic", nil),
	)
	want := map[string]string{"g-1": "its epics e-1 closed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
}

func TestAClosedTicketIsNotListed(t *testing.T) {
	g := grilling(linkedTo("e-1", domain.EdgeRelated))
	g.Status = "closed"
	if got := finished(t, g, bead("e-1", "one", "closed", "epic", nil)); len(got) != 0 {
		t.Fatalf("listed %v, want nothing", got)
	}
}

func TestManyEpicsAreNamedThreeAndACount(t *testing.T) {
	beads := []domain.GraphBead{grilling(
		linkedTo("e-1", domain.EdgeRelated), linkedTo("e-2", domain.EdgeRelated),
		linkedTo("e-3", domain.EdgeRelated), linkedTo("e-4", domain.EdgeRelated),
		linkedTo("e-5", domain.EdgeRelated),
	)}
	for _, id := range []string{"e-1", "e-2", "e-3", "e-4", "e-5"} {
		beads = append(beads, bead(id, id, "closed", "epic", nil))
	}
	got := finished(t, beads...)
	want := map[string]string{"g-1": "its epics e-1, e-2, e-3 and 2 more closed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
}

func TestTheListKeepsTheOrderTheBeadsCameIn(t *testing.T) {
	list := domain.FinishedStillOpen([]domain.GraphBead{
		bead("b", "B", "open", "epic", nil),
		bead("b.1", "x", "closed", "task", nil, childOf("b")),
		bead("a", "A", "open", "epic", nil),
		bead("a.1", "x", "closed", "task", nil, childOf("a")),
	})
	if len(list) != 2 || list[0].ID != "b" || list[1].ID != "a" || list[0].Title != "B" {
		t.Fatalf("listed %+v, want b then a", list)
	}
}
