package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
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
