package application_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.452: on a Boost every read of the tracker crosses the tunnel and takes
// two seconds, and a dispatch that read each candidate's comments and needs as
// it came to it cost a row of them (49 bd calls one after another, 73 s, on
// cloud1). A pass reads them for all its candidates together: one read of the
// comments and one of what they wait on, however many candidates there are.
func TestADispatchReadsItsCandidatesCommentsAndNeedsTogether(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "Done already"})
	if err := tracker.CloseStory(ctx, "mw-gq6.9", "done"); err != nil {
		t.Fatal(err)
	}
	ids := []string{"mw-gq6.1", "mw-gq6.2", "mw-gq6.3", "mw-gq6.4"}
	for _, id := range ids {
		tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "The story " + id})
		tracker.Needs(id, "mw-gq6.9")
		if err := tracker.CommentOnStory(ctx, id, "a word from the Mayor"); err != nil {
			t.Fatal(err)
		}
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 1 || len(report.Passed) != len(ids)-1 {
		t.Fatalf("expected one started and the rest passed for the cap, got started %v passed %+v", startedIDs(report), report.Passed)
	}
	if got := tracker.StoriesCommentsCalls(); got != 1 {
		t.Errorf("expected the candidates' comments read in one call, got %d", got)
	}
	for _, id := range ids {
		if got := tracker.CommentReads(id); got != 1 {
			t.Errorf("expected the comments of %s read once, got %d", id, got)
		}
	}
	var shows int
	for _, call := range tracker.Asked() {
		if call == "ShowBeads" {
			shows++
		}
	}
	if shows != 1 {
		t.Errorf("expected what the candidates wait on read in one ShowBeads, got %d: %v", shows, tracker.Asked())
	}
}

// A candidate still waiting on an open bead, and one handed back, are passed
// over for it when their reads came together, as when each was read alone.
func TestADispatchStillPassesOverTheBlockedAndTheHandedBackWhenReadTogether(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	dispatch.Cap = 3
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "Still open"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Waits"})
	tracker.Needs("mw-gq6.1", "mw-gq6.9")
	aHandedBackStory(t, tracker, "mw-gq6.2")
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.3", Title: "Free"})
	if err := tracker.CommentOnStory(ctx, "mw-gq6.3", "a word from the Mayor"); err != nil {
		t.Fatal(err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	why := map[string]string{}
	for _, p := range report.Passed {
		why[p.StoryID] = p.Why
	}
	if !strings.HasPrefix(why["mw-gq6.1"], "waits on mw-gq6.9 (") {
		t.Errorf("expected mw-gq6.1 passed over for its open need, got %q", why["mw-gq6.1"])
	}
	if !strings.Contains(why["mw-gq6.2"], "handed it back") {
		t.Errorf("expected mw-gq6.2 passed over as handed back, got %q", why["mw-gq6.2"])
	}
	started := startedIDs(report)
	if len(started) != 2 || len(runner.Started()) != 2 {
		t.Errorf("expected mw-gq6.9 and mw-gq6.3 started, got %v", started)
	}
	for _, id := range started {
		if id == "mw-gq6.1" || id == "mw-gq6.2" {
			t.Errorf("started %s, which was to be passed over", id)
		}
	}
}

// A failed grist smoke is read once a pass, not once for each rig a candidate
// is on: each read is a bd kv list.
func TestADispatchReadsTheGristSmokesOnceAPass(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.DryRun = true
	dispatch.Rigs = map[string]string{"millwright": "/rigs/millwright", "cairn": "/rigs/cairn", "lampas": "/rigs/lampas"}
	for i, rig := range []string{"millwright", "cairn", "lampas"} {
		epic := "mw-e" + rig
		tracker.AddEpic(epic, domain.Path{Rig: rig, Branch: "main", Harness: domain.HarnessClaude,
			Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "vps"})
		tracker.AddStory(epic, domain.Story{ID: epic + ".1", Title: "A story " + string(rune('a'+i))})
	}
	notes := &countedNotes{FakeTracker: tracker}
	book := application.GristSmokeBook{Notes: notes, Host: "vps"}
	dispatch.SmokeHolds = book
	failed := application.GristSmokeReport{App: "cairn", Examples: 1, Failures: []string{"cairn/sweep/one: no licence"}}
	if err := book.Record(ctx, "cairn", failed); err != nil {
		t.Fatalf("recording the smoke: %v", err)
	}
	notes.listed = 0

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if notes.listed != 1 {
		t.Errorf("expected the grist smokes read with one listing of the notes, got %d", notes.listed)
	}
	var held int
	for _, p := range report.Passed {
		if strings.Contains(p.Why, "grist smoke of cairn failed") {
			held++
			if p.StoryID != "mw-ecairn.1" {
				t.Errorf("expected only the cairn story held, got %s held", p.StoryID)
			}
		}
	}
	if held != 1 {
		t.Errorf("expected the cairn story held by its smoke, got %+v", report.Passed)
	}
}

// countedNotes is the fake tracker's notes, counting each listing of them.
type countedNotes struct {
	*apptest.FakeTracker
	mu     sync.Mutex
	listed int
}

func (n *countedNotes) NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	n.mu.Lock()
	n.listed++
	n.mu.Unlock()
	return n.FakeTracker.NotesWithPrefix(ctx, prefix)
}
