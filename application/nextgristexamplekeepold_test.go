package application

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// anAttemptsRemote is a Landing over one remote branch and the commits the
// worktree's head holds. The remote already holds the branch at an earlier
// attempt's tip that the head does not contain (mw-gq6.1756): a push is a
// fast-forward only when the head holds the remote's tip, as git has it, and
// KeepOrigin makes it so by merging that tip in. movesBeforePush is a tip some
// other push puts on the remote between the two.
type anAttemptsRemote struct {
	Landing
	remote          string
	head            string
	held            map[string]bool
	movesBeforePush string
	pushed          bool
}

func (r *anAttemptsRemote) KeepOrigin(_ context.Context, _, _, _ string) (KeptOrigin, error) {
	if r.remote == "" || r.held[r.remote] {
		return KeptOrigin{Remote: r.remote, Head: r.head}, nil
	}
	r.held[r.remote] = true
	kept := KeptOrigin{Remote: r.remote, Head: "merge-of-" + r.remote, Merged: true}
	r.head = kept.Head
	if r.movesBeforePush != "" {
		r.remote = r.movesBeforePush
	}
	return kept, nil
}

func (r *anAttemptsRemote) Push(_ context.Context, _, _, branch string) error {
	if r.remote != "" && !r.held[r.remote] {
		return fmt.Errorf("! [rejected] HEAD -> %s (non-fast-forward): %w", branch, ErrPushRejected)
	}
	r.remote, r.pushed = r.head, true
	return nil
}

func unprovedAgainstARemote(t *testing.T, remote *anAttemptsRemote) (Refusal, *closeOut, *NextReport) {
	t.Helper()
	report := &NextReport{}
	c := &closeOut{id: "mw-t.1", rigDir: "/rig", worktree: "/wt/mw-t.1", branch: "mw/mw-t.1", target: "main"}
	n := Next{Changes: changedStub{branchChangesStub: branchChangesStub{verseExample}}, Landing: remote, HasGristTestKey: holdsNoKey}
	refusal, refused := n.gristExampleRefusal(context.Background(), c, report, "Added the example.")
	if !refused || refusal.Reason != ReasonGristExampleUnproved {
		t.Fatalf("expected %s, got %+v, %v", ReasonGristExampleUnproved, refusal, refused)
	}
	return refusal, c, report
}

func TestAFreshCutRetryPushesOverAnEarlierAttemptsPushedTipAndKeepsThatTipAndNamesIt(t *testing.T) {
	remote := &anAttemptsRemote{remote: "120fc6112aaaaaaa", head: "attempt2", held: map[string]bool{}}
	refusal, c, report := unprovedAgainstARemote(t, remote)
	if !c.branchPushed || !remote.pushed {
		t.Fatalf("expected the branch pushed, got branchPushed=%v, %+v, notes %q", c.branchPushed, refusal, report.Notes)
	}
	if !remote.held["120fc6112aaaaaaa"] || remote.remote != "merge-of-120fc6112aaaaaaa" {
		t.Errorf("expected the earlier attempt's tip kept under the pushed head, got %+v", remote)
	}
	if !strings.Contains(refusal.Said+"\n"+strings.Join(report.Notes, "\n"), "120fc611") {
		t.Errorf("expected the earlier tip named, got %+v, notes %q", refusal, report.Notes)
	}
}

func TestAFreshCutRetryWhoseRemoteTipMovedAfterItWasReadStillRefusesAndOverwritesNothing(t *testing.T) {
	remote := &anAttemptsRemote{remote: "120fc6112aaaaaaa", head: "attempt2", held: map[string]bool{}, movesBeforePush: "someone-elses"}
	refusal, c, _ := unprovedAgainstARemote(t, remote)
	if c.branchPushed || remote.pushed || remote.remote != "someone-elses" {
		t.Fatalf("expected nothing pushed and the remote as it moved, got branchPushed=%v, %+v", c.branchPushed, remote)
	}
	if !strings.Contains(refusal.Said, "could not be pushed") || !strings.Contains(refusal.Said, "non-fast-forward") {
		t.Errorf("expected the stop to say the push was refused, got %+v", refusal)
	}
}
