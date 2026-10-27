package application_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aRebuiltBranchLanding is a landing whose remote already holds the story's
// branch at a commit this attempt's head does not contain (an earlier attempt's
// push, the branch since rebuilt): git refuses a push of the head until that
// tip is merged in, which KeepOrigin does, unless stubborn says the remote
// moved again meanwhile.
type aRebuiltBranchLanding struct {
	fakeRetryLanding
	remoteTip string
	stubborn  bool
	kept      bool
	keeps     int
	pushes    int
}

func (l *aRebuiltBranchLanding) KeepOrigin(context.Context, string, string, string) (application.KeptOrigin, error) {
	l.keeps++
	l.kept = true
	return application.KeptOrigin{Remote: l.remoteTip, Head: "bbbbbbbbbbbbbbbb", Merged: true}, nil
}

func (l *aRebuiltBranchLanding) RefHead(_ context.Context, _, ref string) (string, error) {
	if l.kept {
		return "bbbbbbbbbbbbbbbb", nil
	}
	return "aaaaaaaaaaaaaaaa", nil
}

func (l *aRebuiltBranchLanding) Push(_ context.Context, _, _, branch string) error {
	l.pushes++
	if l.stubborn || !l.kept {
		return fmt.Errorf("! [rejected] HEAD -> %s (non-fast-forward): %w", branch, application.ErrPushRejected)
	}
	return nil
}

func aBatchRigCloseOutOfARebuiltBranch(t *testing.T, stubborn bool) (application.NextReport, error, *aRebuiltBranchLanding) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aRebuiltBranchLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}, remoteTip: "a9d28977a9d28977", stubborn: stubborn}
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands, Rules: aBatchRigsRules{},
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"lampas": "/rigs/lampas"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return report, err, lands
}

// An attempt's branch that was rebuilt since an earlier attempt pushed it to
// the queue is queued all the same: origin's copy is merged in keeping the
// attempt's files, and the push is a fast-forward (mw-gq6.818).
func TestABatchRigStoryWhoseBranchWasRebuiltSinceAnEarlierPushIsQueued(t *testing.T) {
	report, err, lands := aBatchRigCloseOutOfARebuiltBranch(t, false)

	if err != nil || !report.Queued {
		t.Fatalf("expected the story queued, got err %v and %+v", err, report)
	}
	if lands.keeps != 1 {
		t.Errorf("expected origin's copy reconciled once, got %d", lands.keeps)
	}
	said := strings.Join(report.Notes, "\n")
	if !strings.Contains(said, "a9d28977") || !strings.Contains(said, "merged") {
		t.Errorf("expected a note that origin's tip a9d28977 was merged in, got %q", said)
	}
}

// When the push is refused even then, the stop names this branch's tip, the
// remote's, and the one command that lands it, and not a guess at a rebase.
func TestABatchRigStopNamesTheTwoTipsAndTheCommandThatLandsIt(t *testing.T) {
	report, err, _ := aBatchRigCloseOutOfARebuiltBranch(t, true)

	if err == nil && report.Queued {
		t.Fatalf("expected the story not queued, got %+v", report)
	}
	said := report.Why
	if err != nil {
		said += " " + err.Error()
	}
	for _, want := range []string{"bbbbbbbb", "a9d28977", "--force-with-lease=mw/mw-x.1:a9d28977a9d28977", "HEAD:refs/heads/mw/mw-x.1"} {
		if !strings.Contains(said, want) {
			t.Errorf("expected the stop to name %q, got %q", want, said)
		}
	}
	if strings.Contains(said, "rebased since it was queued") {
		t.Errorf("expected no guess at a rebase, got %q", said)
	}
}
