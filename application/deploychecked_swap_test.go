package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-t5shinh.36: a swap run by a tap writes DEPLOYED-CHECKED as the automatic swap
// does, and both write it on every story in the backend range the swap made live.

const swapHealthz = "https://postern.example.org/api/healthz"

// aStagedSwapForATap is a home whose staged swap is left as a hands step (the
// rig is not given a runner), with the live site answering healthz.
func aStagedSwapForATap(t *testing.T, healthzAnswer string) (backendRig, string, domain.HandsStep, *liveWeb) {
	t.Helper()
	r := aBackendRig(t, "laptop", "laptop")
	web := &liveWeb{pages: map[string]string{swapHealthz: healthzAnswer}}
	r.stage.Live = deployCheckAt(web, domain.EpicRequirements{HealthzURL: swapHealthz}, r.tracker)
	r.stage.Landed(context.Background(), presenceLanding())
	filed := filedUnder(t, r, "mw-j0f2d")
	if len(filed) != 1 {
		t.Fatalf("expected one swap bead filed, got %+v", filed)
	}
	bead := filed[0].Story.ID
	return r, bead, handsSteps(t, r.tracker, bead)[0].HandsStep, web
}

func TestATappedSwapWritesDeployedCheckedOnTheStoryItCarriedWhenHealthzHoldsTheCommit(t *testing.T) {
	r, bead, step, _ := aStagedSwapForATap(t, `{"ok":true,"commit":"4c9db71"}`)

	notes := r.stage.TappedSwap(context.Background(), bead, step, application.HandsOutcome{Exit: 0, Output: swapAnswered})

	found := deployedChecks(r.tracker.Comments("mw-j0f2d.28"))
	if len(found) != 1 {
		t.Fatalf("expected one DEPLOYED-CHECKED on mw-j0f2d.28, got %q (notes %q)", r.tracker.Comments("mw-j0f2d.28"), notes)
	}
	for _, want := range []string{"DEPLOYED-CHECKED by mw on desktop", "version 4c9db71 was found in the healthz answer"} {
		if !strings.Contains(found[0], want) {
			t.Errorf("expected %q in %q", want, found[0])
		}
	}
	if strings.Contains(strings.ToLower(found[0]), "verified") {
		t.Errorf("expected no VERIFIED in %q", found[0])
	}
	if own := deployedChecks(r.tracker.Comments(bead)); len(own) != 0 {
		t.Errorf("expected nothing written on the swap bead itself, got %q", own)
	}
}

func TestATappedSwapSaysTheVersionWasNotFoundWhenHealthzHoldsAnotherCommit(t *testing.T) {
	r, bead, step, _ := aStagedSwapForATap(t, `{"ok":true,"commit":"1234567"}`)

	r.stage.TappedSwap(context.Background(), bead, step, application.HandsOutcome{Exit: 0, Output: swapAnswered})

	found := deployedChecks(r.tracker.Comments("mw-j0f2d.28"))
	if len(found) != 1 || !strings.Contains(found[0], "version 4c9db71 was not found in the healthz answer") {
		t.Fatalf("expected a comment saying the version was not found, got %q", r.tracker.Comments("mw-j0f2d.28"))
	}
}

func TestATappedSwapThatFailedWritesNothingAndReadsNothing(t *testing.T) {
	r, bead, step, web := aStagedSwapForATap(t, `{"ok":true,"commit":"4c9db71"}`)

	r.stage.TappedSwap(context.Background(), bead, step, application.HandsOutcome{Exit: 1, Output: "curl said no\n"})

	if found := deployedChecks(r.tracker.Comments("mw-j0f2d.28")); len(found) != 0 {
		t.Errorf("expected no DEPLOYED-CHECKED after a failed swap, got %q", found)
	}
	if len(web.asked) != 0 {
		t.Errorf("expected the live site not read after a failed swap, got %v", web.asked)
	}
}

// The tap on a swap of a rig the host has no table for is not this stage's.
func TestATappedStepThatIsNoSwapWritesNothing(t *testing.T) {
	r, bead, _, web := aStagedSwapForATap(t, `{"ok":true,"commit":"4c9db71"}`)

	notes := r.stage.TappedSwap(context.Background(), bead, domain.HandsStep{ID: "echo", Host: "laptop", As: "user", Run: "echo hello"}, application.HandsOutcome{Exit: 0})

	if len(notes) != 0 || len(web.asked) != 0 || len(deployedChecks(r.tracker.Comments("mw-j0f2d.28"))) != 0 {
		t.Errorf("expected nothing for a step that is no swap, got notes %q, asked %v", notes, web.asked)
	}
}

const secondLanded = "7e5d2c1a0f7a5b3c2d1e8f9a0b1c2d3e4f5a6b7c"

// anOlderSwapStillWaiting stages a second landing on top of the first one's swap
// that had not run: the newer binary carries both stories' backend changes.
func anOlderSwapStillWaiting(t *testing.T, r backendRig) {
	t.Helper()
	r.tracker.AddStory("mw-j0f2d", domain.Story{ID: "mw-j0f2d.29", Title: "Show the Mayor's presence on the phone"})
	r.stage.Landed(context.Background(), application.BackendLanding{Rig: "postern", Story: "mw-j0f2d.29", Title: "on the phone", Epic: "mw-j0f2d", Commit: secondLanded})
}

func TestATappedSwapWritesDeployedCheckedOnEveryStoryInItsRange(t *testing.T) {
	r, _, _, _ := aStagedSwapForATap(t, `{"ok":true,"commit":"7e5d2c1"}`)
	anOlderSwapStillWaiting(t, r)
	var newer string
	for _, s := range filedUnder(t, r, "mw-j0f2d") {
		if strings.Contains(s.Story.Title, "7e5d2c1") && !strings.Contains(s.Story.Title, "standby") {
			newer = s.Story.ID
		}
	}
	if newer == "" {
		t.Fatalf("expected a swap bead for 7e5d2c1 to be filed")
	}
	step := handsSteps(t, r.tracker, newer)[0].HandsStep

	r.stage.TappedSwap(context.Background(), newer, step, application.HandsOutcome{Exit: 0, Output: "backend 7e5d2c1 is live and answering\n"})

	for _, id := range []string{"mw-j0f2d.28", "mw-j0f2d.29"} {
		if found := deployedChecks(r.tracker.Comments(id)); len(found) != 1 {
			t.Errorf("expected one DEPLOYED-CHECKED on %s, got %q", id, r.tracker.Comments(id))
		}
	}
}

func TestTheAutomaticSwapWritesDeployedCheckedOnEveryStoryInItsRange(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: "backend 7e5d2c1 is live and answering\n"})
	web := &liveWeb{pages: map[string]string{swapHealthz: `{"ok":true,"commit":"7e5d2c1"}`}}
	a.stage.Live = deployCheckAt(web, domain.EpicRequirements{HealthzURL: swapHealthz}, a.tracker)
	a.stage.Landed(context.Background(), presenceLanding())
	anOlderSwapStillWaiting(t, a.backendRig)

	a.stage.Pending(context.Background())

	for _, id := range []string{"mw-j0f2d.28", "mw-j0f2d.29"} {
		found := deployedChecks(a.tracker.Comments(id))
		if len(found) != 1 || !strings.Contains(found[0], "version 7e5d2c1 was found in the healthz answer") {
			t.Errorf("expected one DEPLOYED-CHECKED on %s, got %q", id, a.tracker.Comments(id))
		}
	}
	if web.asked[swapHealthz] != 1 {
		t.Errorf("expected the live site read once for the whole range, got %v", web.asked)
	}
}

// tappedSwapsSeen records the tap-run steps it is offered.
type tappedSwapsSeen struct {
	beads []string
	steps []string
	exits []int
}

func (s *tappedSwapsSeen) TappedSwap(_ context.Context, bead string, step domain.HandsStep, outcome application.HandsOutcome) []string {
	s.beads, s.steps, s.exits = append(s.beads, bead), append(s.steps, step.ID), append(s.exits, outcome.Exit)
	return []string{"backend: the swap of postern to 4c9db71: live check noted"}
}

// A step the Governor's tap ran is offered to the swap check once it has run, and
// what the check says is added to the outcome on the bead.
func TestATapRunOffersTheStepToTheSwapCheckAfterItRan(t *testing.T) {
	f := newRunFixture(t)
	f.approve(t, "tx-run", "echo", runNow.Add(-2*time.Minute), "")
	seen := &tappedSwapsSeen{}
	inbox := f.inbox()
	inbox.Swaps = seen

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}

	if len(seen.steps) != 1 || seen.steps[0] != "echo" || seen.beads[0] != "mw-e.3" || seen.exits[0] != 0 {
		t.Fatalf("expected echo on mw-e.3 offered once with exit 0, got %+v", seen)
	}
	got := f.tracker.Comments("mw-e.3")
	if len(got) != 1 || !strings.Contains(got[0], "RAN step echo") || !strings.HasSuffix(got[0], "live check noted") {
		t.Fatalf("expected the outcome comment to end with the check's note, got %q", got)
	}
}
