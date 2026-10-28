package application_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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

// mw-gq6.492: what this host and the others have in flight is one list of the
// tracker's in-progress stories narrowed twice, so a tick reads it once and not
// once for RunningStories and again for RunningElsewhere.
func TestADispatchTickReadsTheInProgressListOnce(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddEpic("mw-away", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "desktop"})
	tracker.AddStory("mw-away", domain.Story{ID: "mw-away.1", Title: "Held by the desktop"})
	if err := tracker.ClaimStory(ctx, "mw-away.1"); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Ready here"})

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := tracker.InProgressReads(); got != 1 {
		t.Errorf("expected the in-progress list read once in a tick, got %d: %v", got, tracker.Asked())
	}
}

// mw-gq6.826: a pass with N candidates read each one's run state, comments and
// open molecule with a bd of its own (about 50 one-shot calls at 15
// candidates, on a host already at load 23-28). A tracker that can read them
// together does so once for the whole pass: one read of the states, one of the
// comments and one of the molecules, however many candidates there are.

// onlyTheTracker is a tracker as it is without the reads it can make together:
// the WorkTracker alone, none of what a tracker may offer besides.
type onlyTheTracker struct{ application.WorkTracker }

// perStory is how many reads of one story at a time the pass has made.
func perStory(tracker *apptest.FakeTracker) int {
	return tracker.Reads("StoryState") + tracker.Reads("StoryComments") + tracker.Reads("OpenMolecule")
}

// aPassOfDeadPanes puts n claimed stories here, each with a session that has
// died, and returns the ids.
func aPassOfDeadPanes(t *testing.T, tracker *apptest.FakeTracker, runner *apptest.FakeRunner, n int) []string {
	t.Helper()
	var ids []string
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("mw-gq6.%d", i)
		claimedStory(t, tracker, id, 1)
		runner.Exit(startOldSession(t, runner, id), 1)
		ids = append(ids, id)
	}
	return ids
}

func TestADryRunReadsTheStatesOfWhatIsClaimedInOneRead(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	dispatch.DryRun = true
	dispatch.Cap = 6
	ids := aPassOfDeadPanes(t, tracker, runner, 5)
	if err := tracker.SetStoryState(ctx, ids[1], application.RunState, application.RunBlocked, "(tests-fail) the tests fail"); err != nil {
		t.Fatal(err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := tracker.Reads("StoryState"); got != 0 {
		t.Errorf("expected no state read one story at a time, got %d", got)
	}
	if got := tracker.Reads("StoriesState"); got != 1 {
		t.Errorf("expected the states of %d claims read in one read, got %d", len(ids), got)
	}
	if len(report.HeldRefused) != 1 || report.HeldRefused[0].StoryID != ids[1] || report.Running != 4 {
		t.Errorf("expected only %s held refused and 4 running, got %+v", ids[1], report)
	}
}

func TestAPassReadsTheStatesOfWhatIsClaimedInOneReadAndReclaimsTheDead(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Cap = 6
	ids := aPassOfDeadPanes(t, tracker, runner, 5)
	for _, id := range ids {
		if err := tracker.SetLeaseExpires(id, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tracker.SetStoryState(ctx, ids[1], application.RunState, application.RunBlocked, "(tests-fail) the tests fail"); err != nil {
		t.Fatal(err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := tracker.Reads("StoryState"); got != 0 {
		t.Errorf("expected no state read one story at a time, got %d", got)
	}
	if got := tracker.Reads("StoriesState"); got != 1 {
		t.Errorf("expected the states read in one read, got %d", got)
	}
	if len(report.Reclaimed) != 4 || len(report.HeldRefused) != 1 {
		t.Errorf("expected four reclaimed and %s held, got reclaimed %+v held %+v", ids[1], report.Reclaimed, report.HeldRefused)
	}
}

func TestAPassReadsTheCommentsOfWhatItRefusesInOneRead(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddEpic("mw-ghost", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "ghost-formula", Host: "vps"})
	var ids []string
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("mw-ghost.%d", i)
		tracker.AddStory("mw-ghost", domain.Story{ID: id, Title: "The story " + id})
		if err := tracker.CommentOnStory(ctx, id, "a word from the Mayor"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	// One of them was told already, on an earlier pass.
	if err := tracker.CommentOnStory(ctx, ids[2], "did not claim this story ("+application.ReasonFormulaNotInstalled+")"); err != nil {
		t.Fatal(err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := tracker.Reads("StoryComments"); got != 0 {
		t.Errorf("expected no comments read one story at a time, got %d", got)
	}
	if got := tracker.Reads("StoriesComments"); got != 1 {
		t.Errorf("expected the comments of the %d refused read in one read, got %d", len(ids), got)
	}
	if len(report.Passed) != len(ids) {
		t.Fatalf("expected all %d passed over, got %+v", len(ids), report.Passed)
	}
	for i, id := range ids {
		told := 0
		for _, c := range tracker.Comments(id) {
			if strings.Contains(c, application.ReasonFormulaNotInstalled) {
				told++
			}
		}
		if told != 1 {
			t.Errorf("expected %s (index %d) told once its formula is not installed, got %d times", id, i, told)
		}
	}
}

func TestAPassReadsTheOpenMoleculesOfWhatItClaimsInOneRead(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	dispatch.Cap = 4
	tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "One"}, application.FormulaStep{Title: "Two"})
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("mw-gq6.%d", i)
		tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "The story " + id})
		poured, err := tracker.PourFormula(ctx, "tdd-feature", id, "The story "+id, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := tracker.SetStoryMetadata(ctx, id, map[string]string{application.MoleculeField: poured.RootID}); err != nil {
			t.Fatal(err)
		}
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 4 || len(runner.Started()) != 4 {
		t.Fatalf("expected all four started, got %v", startedIDs(report))
	}
	if got := tracker.Reads("OpenMolecule"); got != 0 {
		t.Errorf("expected no molecule read one at a time, got %d", got)
	}
	if got := tracker.Reads("OpenMolecules"); got != 1 {
		t.Errorf("expected the four molecules read in one read, got %d", got)
	}
	if tracker.Molecules() != 4 {
		t.Errorf("expected the four open molecules worked again and none poured, got %d molecules", tracker.Molecules())
	}
}

// A tracker that cannot read them together is read a story at a time, as it was.
func TestAPassOnATrackerThatCannotReadTogetherReadsAStoryAtATime(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	dispatch.Tracker = onlyTheTracker{tracker}
	dispatch.DryRun = true
	dispatch.Cap = 6
	ids := aPassOfDeadPanes(t, tracker, runner, 3)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := tracker.Reads("StoryState"); got != len(ids) {
		t.Errorf("expected the state of each of %d claims read on its own, got %d", len(ids), got)
	}
	if got := tracker.Reads("StoriesState"); got != 0 {
		t.Errorf("expected no read together on a tracker that offers none, got %d", got)
	}
	if report.Running != 3 {
		t.Errorf("expected three running, got %+v", report)
	}
}

// A read together that fails is no reason to read nothing: each story is read
// on its own, and one story's fault fails no other.
func TestAPassFallsBackToAStoryAtATimeWhenTheReadTogetherFails(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "One"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "The story"})
	poured, err := tracker.PourFormula(ctx, "tdd-feature", "mw-gq6.1", "The story", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{application.MoleculeField: poured.RootID}); err != nil {
		t.Fatal(err)
	}
	tracker.FailBatches(fmt.Errorf("the connection dropped"))

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 1 || len(runner.Started()) != 1 {
		t.Fatalf("expected the story started, got %+v", report)
	}
	if tracker.Reads("OpenMolecules") != 1 || tracker.Reads("OpenMolecule") != 1 {
		t.Errorf("expected the batch tried once and the molecule then read alone, got %d and %d",
			tracker.Reads("OpenMolecules"), tracker.Reads("OpenMolecule"))
	}
}

// mw --dry-run says the same whether the pass read the candidates together or
// a story at a time.
func TestADryRunSaysTheSameWhetherTheReadsCameTogetherOrNot(t *testing.T) {
	ctx := context.Background()
	say := func(together bool) string {
		dispatch, tracker, _, runner, _ := aFactory(t)
		dispatch.DryRun = true
		dispatch.Cap = 6
		if !together {
			dispatch.Tracker = onlyTheTracker{tracker}
		}
		ids := aPassOfDeadPanes(t, tracker, runner, 4)
		if err := tracker.SetStoryState(ctx, ids[2], application.RunState, application.RunBlocked, "(tests-fail) the tests fail"); err != nil {
			t.Fatal(err)
		}
		tracker.AddEpic("mw-ghost", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
			Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "ghost-formula", Host: "vps"})
		tracker.AddStory("mw-ghost", domain.Story{ID: "mw-ghost.1", Title: "Wants a formula nobody installed"})
		if err := tracker.CommentOnStory(ctx, "mw-ghost.1", "a word from the Mayor"); err != nil {
			t.Fatal(err)
		}
		tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.20", Title: "Ready"})
		report, err := dispatch.Run(ctx)
		if err != nil {
			t.Fatalf("dispatching (together %v): %v", together, err)
		}
		return report.String()
	}
	together, alone := say(true), say(false)
	if together != alone {
		t.Errorf("expected the same dry run either way.\ntogether:\n%s\nalone:\n%s", together, alone)
	}
	if !strings.Contains(together, "mw-ghost.1") || !strings.Contains(together, "mw-gq6.20") {
		t.Errorf("expected the dry run to name what it passed over and what it would start, got:\n%s", together)
	}
}
