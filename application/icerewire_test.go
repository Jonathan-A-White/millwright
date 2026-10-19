package application_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// blockersNow is what the fake tracker says a bead waits on, closed beads
// included.
func blockersNow(t *testing.T, tracker *apptest.FakeTracker, id string) []string {
	t.Helper()
	graph, err := tracker.BeadGraph(context.Background())
	mustDo(t, err)
	var waits []string
	for _, b := range graph {
		if b.ID != id {
			continue
		}
		for _, e := range b.Edges {
			if e.Kind == "blocks" {
				waits = append(waits, e.To)
			}
		}
	}
	slices.Sort(waits)
	return waits
}

func iceRewiring(tracker *apptest.FakeTracker) application.Ice {
	return application.Ice{Tracker: tracker, Notes: tracker, Graph: tracker, Wires: tracker, Now: func() time.Time { return iceNow }}
}

// chainFixture: mw-c.1 waits on the open mw-c.0, mw-c.2 and mw-c.3 wait on
// mw-c.1, mw-c.4 is closed and waits on mw-c.1, and mw-c.5 waits on mw-c.1 and
// also on the closed mw-c.6, which itself waited on mw-c.1.
func chainFixture() *apptest.FakeTracker {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-c", domain.Path{})
	for _, id := range []string{"mw-c.0", "mw-c.1", "mw-c.2", "mw-c.3", "mw-c.4", "mw-c.5", "mw-c.6"} {
		tracker.AddStory("mw-c", domain.Story{ID: id, Title: "Story " + id})
	}
	tracker.Needs("mw-c.1", "mw-c.0")
	tracker.Needs("mw-c.2", "mw-c.1")
	tracker.Needs("mw-c.3", "mw-c.1")
	tracker.Needs("mw-c.4", "mw-c.1")
	tracker.Needs("mw-c.5", "mw-c.1", "mw-c.6")
	tracker.Needs("mw-c.6", "mw-c.1")
	_ = tracker.SetStatus("mw-c.4", apptest.StatusClosed)
	_ = tracker.SetStatus("mw-c.6", apptest.StatusClosed)
	return tracker
}

func TestIcingABeadMovesItBehindTheLiveBeadsThatWaitedOnIt(t *testing.T) {
	tracker := chainFixture()
	report, err := iceRewiring(tracker).Do(context.Background(), "mw-c.1", "not yet")
	if err != nil {
		t.Fatalf("icing: %v", err)
	}

	// A live dependent drops the iced bead, takes its open blocker, and the
	// iced bead waits on it.
	for _, x := range []string{"mw-c.2", "mw-c.3"} {
		if got := blockersNow(t, tracker, x); !slices.Equal(got, []string{"mw-c.0"}) {
			t.Errorf("%s waits on %v, want only mw-c.0 (it dropped the iced bead and took its open blocker)", x, got)
		}
	}
	// The iced bead keeps no open blocker of its own but waits on the live ones.
	if got := blockersNow(t, tracker, "mw-c.1"); !slices.Contains(got, "mw-c.2") || !slices.Contains(got, "mw-c.3") {
		t.Errorf("mw-c.1 waits on %v, want it behind mw-c.2 and mw-c.3", got)
	}
	// A closed dependent is left alone.
	if got := blockersNow(t, tracker, "mw-c.4"); !slices.Equal(got, []string{"mw-c.1"}) {
		t.Errorf("closed mw-c.4 waits on %v, want it untouched", got)
	}
	// mw-c.5 reaches the iced bead through a closed bead, so the iced bead
	// cannot wait on it: it is freed and the report says so.
	if got := blockersNow(t, tracker, "mw-c.5"); !slices.Equal(got, []string{"mw-c.0", "mw-c.6"}) {
		t.Errorf("mw-c.5 waits on %v, want mw-c.0 and mw-c.6", got)
	}
	if slices.Contains(blockersNow(t, tracker, "mw-c.1"), "mw-c.5") {
		t.Error("the iced bead waits on mw-c.5, which would be a cycle through the closed mw-c.6")
	}

	said := report.String()
	for _, want := range []string{"mw-c.2", "mw-c.3", "mw-c.5", "closed", "mw-c.0"} {
		if !strings.Contains(said, want) {
			t.Errorf("the report %q does not say %q", said, want)
		}
	}
	// The icing itself is as before.
	d, err := tracker.ShowStory(context.Background(), "mw-c.1")
	mustDo(t, err)
	if !slices.Contains(d.Labels, application.LabelIcebox) {
		t.Errorf("mw-c.1 labels = %v, want icebox", d.Labels)
	}
}

func TestIcingABeadNothingWaitsOnRewiresNothing(t *testing.T) {
	tracker := chainFixture()
	report, err := iceRewiring(tracker).Do(context.Background(), "mw-c.2", "later")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Moves) != 0 || report.String() != "" {
		t.Errorf("report = %+v %q, want nothing moved", report.Moves, report.String())
	}
}

func TestIcingWithoutAGraphOnlyIcesAsBefore(t *testing.T) {
	tracker := chainFixture()
	if err := ice(t, tracker, "mw-c.1", "later"); err != nil {
		t.Fatal(err)
	}
	if got := blockersNow(t, tracker, "mw-c.2"); !slices.Equal(got, []string{"mw-c.1"}) {
		t.Errorf("mw-c.2 waits on %v, want it untouched", got)
	}
}
