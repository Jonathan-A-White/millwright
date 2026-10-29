package application_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aRemoteThatMovesWhileTheTestsRun is a remote whose target branch gains a
// commit while the rig's tests run in the story's worktree: the rig sees only
// what its last fetch brought (mw-gq6.848).
type aRemoteThatMovesWhileTheTestsRun struct {
	fakeRetryLanding
	remoteHead, seenHead string
	// openedAt is the target commit each landing worktree was cut at.
	openedAt []string
}

func (r *aRemoteThatMovesWhileTheTestsRun) Fetch(context.Context, string) error {
	r.seenHead = r.remoteHead
	return nil
}

func (r *aRemoteThatMovesWhileTheTestsRun) RefHead(_ context.Context, _, ref string) (string, error) {
	if ref == "origin/main" {
		return r.seenHead, nil
	}
	return "", nil
}

func (r *aRemoteThatMovesWhileTheTestsRun) OpenLanding(context.Context, string, string) (string, error) {
	r.openedAt = append(r.openedAt, r.seenHead)
	return "/landing", nil
}

func (*aRemoteThatMovesWhileTheTestsRun) Merge(context.Context, string, string) (application.Landed, error) {
	return application.Landed{Commit: "0123456789abcdef"}, nil
}

// checksThatLetTheRemoteMove are the rig's tests: the first run, in the
// worktree, ends with another host having landed on the target branch.
type checksThatLetTheRemoteMove struct {
	remote *aRemoteThatMovesWhileTheTestsRun
	runs   int
	// failFirst makes the worktree's run fail, as cloud2's did.
	failFirst bool
}

func (c *checksThatLetTheRemoteMove) Run(context.Context, string, string) (application.Checked, error) {
	c.runs++
	if c.runs == 1 {
		c.remote.remoteHead = "cf5a5669aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		if c.failFirst {
			return application.Checked{Command: "make test", Output: "FAIL: tests/unit/match-export.test.ts\n"}, nil
		}
	}
	return application.Checked{Command: "make test", Passed: true}, nil
}

func aCloseOutOverAMovingRemote(t *testing.T, failFirst bool) (*aRemoteThatMovesWhileTheTestsRun, application.NextReport, error) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	remote := &aRemoteThatMovesWhileTheTestsRun{
		fakeRetryLanding: fakeRetryLanding{AheadCount: 1, BehindCount: 4},
		remoteHead:       "c76340d7bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: remote, Landing: remote,
		Checks: &checksThatLetTheRemoteMove{remote: remote, failFirst: failFirst}, Slot: aSlot{},
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return remote, report, err
}

func TestAMergeIsMadeOnTheTargetTheRemoteHasAfterTheTestsRan(t *testing.T) {
	remote, report, err := aCloseOutOverAMovingRemote(t, false)
	if err != nil || !report.Landed {
		t.Fatalf("expected the story landed, got %v and %+v", err, report)
	}
	if len(remote.openedAt) != 1 || remote.openedAt[0] != remote.remoteHead {
		t.Fatalf("expected the landing cut at the remote's newest target %s, got %q", remote.remoteHead, remote.openedAt)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "origin/main at cf5a5669") {
		t.Fatalf("expected a note naming the target commit the merge used, got %q", report.Notes)
	}
}

func TestAMergedRunAfterAFailedWorktreeRunUsesTheTargetTheRemoteHasThen(t *testing.T) {
	remote, report, err := aCloseOutOverAMovingRemote(t, true)
	if err != nil || !report.Landed {
		t.Fatalf("expected the story landed on the merged result, got %v and %+v", err, report)
	}
	for _, at := range remote.openedAt {
		if at != remote.remoteHead {
			t.Fatalf("expected every landing cut at the remote's newest target %s, got %q", remote.remoteHead, remote.openedAt)
		}
	}
}

// aBatchRemoteThatMoves is the moving remote of a rig that lands in batches.
type aBatchRemoteThatMoves struct {
	aRemoteThatMovesWhileTheTestsRun
}

func (*aBatchRemoteThatMoves) CloseLanding(context.Context, string, string) error { return nil }

func TestABatchRigsMergedRunUsesTheTargetTheRemoteHasAfterTheWorktreeRunsTests(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	remote := &aBatchRemoteThatMoves{aRemoteThatMovesWhileTheTestsRun{
		fakeRetryLanding: fakeRetryLanding{AheadCount: 1, BehindCount: 4},
		remoteHead:       "c76340d7bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}}
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: remote, Landing: remote, Rules: aBatchRigsRules{},
		Checks: &checksThatLetTheRemoteMove{remote: &remote.aRemoteThatMovesWhileTheTestsRun, failFirst: true}, Slot: aSlot{},
		Mailbox: apptest.NewFakeMailbox(),
		Seat:    "builder", Host: "laptop",
		Rigs: map[string]string{"lampas": "/rigs/lampas"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	if err != nil || !report.Queued {
		t.Fatalf("expected the story queued, got %v and %+v", err, report)
	}
	if len(remote.openedAt) != 1 || remote.openedAt[0] != remote.remoteHead {
		t.Fatalf("expected the landing cut at the remote's newest target %s, got %q", remote.remoteHead, remote.openedAt)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "with origin/main at cf5a5669") {
		t.Fatalf("expected a note naming the target commit the merged run used, got %q", report.Notes)
	}
}
