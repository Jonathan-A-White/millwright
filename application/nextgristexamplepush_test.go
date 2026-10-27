package application

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// pushedBranches is a Landing that records the pushes made through it; every
// other method is the embedded nil Landing's, and is not called.
type pushedBranches struct {
	Landing
	dirs, remotes, branches []string
	err                     error
}

func (p *pushedBranches) Push(_ context.Context, dir, remote, branch string) error {
	p.dirs, p.remotes, p.branches = append(p.dirs, dir), append(p.remotes, remote), append(p.branches, branch)
	return p.err
}

func unprovedOn(t *testing.T, key func() bool, landing Landing, comment string) (Refusal, bool, *NextReport) {
	t.Helper()
	report := &NextReport{}
	c := &closeOut{id: "mw-t.1", rigDir: "/rig", worktree: "/wt/mw-t.1", branch: "mw/mw-t.1", target: "main"}
	n := Next{Changes: changedStub{branchChangesStub: branchChangesStub{verseExample}}, Landing: landing, HasGristTestKey: key}
	refusal, refused := n.gristExampleRefusal(context.Background(), c, report, comment)
	return refusal, refused, report
}

func holdsNoKey() bool { return false }
func holdsKey() bool   { return true }

func TestAnUnprovedGrindExampleOnAHostThatCannotReachTheMillPushesTheBranchAndNamesTheEvalForTheHome(t *testing.T) {
	landing := &pushedBranches{}
	refusal, refused, _ := unprovedOn(t, holdsNoKey, landing, "Added the example.")
	if !refused || refusal.Reason != ReasonGristExampleUnproved {
		t.Fatalf("expected %s, got %+v, %v", ReasonGristExampleUnproved, refusal, refused)
	}
	if len(landing.branches) != 1 || landing.dirs[0] != "/wt/mw-t.1" || landing.remotes[0] != DefaultRemote || landing.branches[0] != "mw/mw-t.1" {
		t.Fatalf("expected the story's worktree pushed once to %s as mw/mw-t.1, got %+v", DefaultRemote, landing)
	}
	for _, want := range []string{
		"Pushed mw/mw-t.1 to " + DefaultRemote,
		"mw grist eval <app> bible-talk --example verse-text-ask --ref mw/mw-t.1",
		"From the home",
	} {
		if !strings.Contains(refusal.Why+"\n"+refusal.Said, want) {
			t.Errorf("expected the refusal to say %q, got %+v", want, refusal)
		}
	}
}

func TestAnUnprovedGrindExampleOnAHostThatCanReachTheMillIsStoppedAsBeforeAndNothingIsPushed(t *testing.T) {
	for name, key := range map[string]func() bool{"the key": holdsKey, "no word of a key": nil} {
		landing := &pushedBranches{}
		refusal, refused, _ := unprovedOn(t, key, landing, "Added the example.")
		if !refused || refusal.Reason != ReasonGristExampleUnproved {
			t.Fatalf("%s: expected %s, got %+v, %v", name, ReasonGristExampleUnproved, refusal, refused)
		}
		if len(landing.branches) != 0 {
			t.Errorf("%s: expected no push, got %+v", name, landing)
		}
		if strings.Contains(refusal.Why+refusal.Said, "pushed") || !strings.Contains(refusal.Said, "Run each on the branch against the real mill") {
			t.Errorf("%s: expected the stop as before, got %+v", name, refusal)
		}
	}
}

func TestAProvedGrindExampleOnAHostThatCannotReachTheMillPushesNothing(t *testing.T) {
	landing := &pushedBranches{}
	comment := "mw grist eval lampas bible-talk --example verse-text-ask: PASS"
	if refusal, refused, _ := unprovedOn(t, holdsNoKey, landing, comment); refused || len(landing.branches) != 0 {
		t.Fatalf("expected no refusal and no push, got %+v, %v, %+v", refusal, refused, landing)
	}
}

func TestABranchThatCouldNotBePushedIsStillStoppedAndTheStopSaysSo(t *testing.T) {
	landing := &pushedBranches{err: errors.New("the origin is gone")}
	refusal, refused, report := unprovedOn(t, holdsNoKey, landing, "Added the example.")
	if !refused || refusal.Reason != ReasonGristExampleUnproved {
		t.Fatalf("expected %s, got %+v, %v", ReasonGristExampleUnproved, refusal, refused)
	}
	if !strings.Contains(refusal.Said, "could not be pushed") || !strings.Contains(refusal.Said, "the origin is gone") {
		t.Errorf("expected the stop to say the push failed and why, got %+v", refusal)
	}
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "the origin is gone") {
		t.Errorf("expected one note naming the failure, got %q", report.Notes)
	}
}
