package application_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// `bd ready --parent <epic>` asks Dolt for every descendant of the parent and
// then for each one's blockers; on an epic with over a thousand children it
// runs past Dolt's 20 s read timeout ('get parent descendants: descendant
// rows: invalid connection') while `bd list --parent`, which mw's reads use,
// answers in about a second (mw-gq6.994). A boot sheet reads the frontier of a
// big parent from a brief, not from `bd ready --parent`.

// noReadyQuery is a tracker that fails the test if it is asked the question
// that is slow on a big parent.
type noReadyQuery struct {
	*apptest.FakeTracker
	t *testing.T
}

func (n noReadyQuery) ReadyStories(ctx context.Context, epicID, host string) ([]application.StoryDetail, error) {
	n.t.Errorf("the brief asked the tracker for the ready stories of %s: that is bd ready --parent, which times out on a big parent", epicID)
	return n.FakeTracker.ReadyStories(ctx, epicID, host)
}

func TestABriefGivesTheFrontierOfAParentWithoutAskingForItsReadyStories(t *testing.T) {
	fake := apptest.NewFakeTracker()
	fake.AddEpic("mw-big", domain.Path{})
	for _, id := range []string{"mw-big.1", "mw-big.2", "mw-big.3", "mw-big.4"} {
		fake.AddStory("mw-big", domain.Story{ID: id, Title: "Story " + id})
	}
	fake.Needs("mw-big.2", "mw-big.1")
	if err := fake.SetStatus("mw-big.3", apptest.StatusInProgress); err != nil {
		t.Fatal(err)
	}

	report, err := application.Brief{Tracker: noReadyQuery{FakeTracker: fake, t: t}}.Run(context.Background(), "mw-big")
	if err != nil {
		t.Fatalf("briefing: %v", err)
	}

	var got []string
	for _, line := range report.Beads[0].Frontier() {
		got = append(got, line.Story.Story.ID)
	}
	want := []string{"mw-big.1", "mw-big.4"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the frontier of mw-big = %v, want %v (open and waiting on nothing; mw-big.2 waits, mw-big.3 is in progress)", got, want)
	}
}
