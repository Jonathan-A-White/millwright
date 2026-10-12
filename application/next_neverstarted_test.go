package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aheadOnlyOn is a landing on which one story's branch has commits beyond the
// target, and every other branch has none.
type aheadOnlyOn struct {
	*fakeRetryLanding
	branch  string
	commits int
}

func (a aheadOnlyOn) Ahead(ctx context.Context, rigDir, branch, base string) (int, error) {
	if branch == a.branch {
		return a.commits, nil
	}
	return a.fakeRetryLanding.Ahead(ctx, rigDir, branch, base)
}

// aTranscript is what the harness recorded in a story's worktree.
type aTranscript string

func (t aTranscript) Tail(context.Context, string, int) (string, error) { return string(t), nil }

// aNextWithAStrandedClaim is the mw next that closes mw-x.1 out of its ledger
// line while mw-x.2 sits claimed on this host with no session behind it,
// claimed claimAge before the clock (mw-gq6.372).
func aNextWithAStrandedClaim(t *testing.T, claimAge time.Duration, landing application.Landing, worktrees application.Worktrees, transcripts application.TranscriptTail) (application.Next, *apptest.FakeTracker) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.2", Title: "A story whose session never started"})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.2"))
	mustDo(t, tracker.SetStarted("mw-x.2", now.Add(-claimAge)))
	vault := newFakeVault()
	mustDo(t, vault.AppendToLedger(ctx, "builder", "| 2026-09-30 | mw-x.1 | landed on main | 1 | |"))
	return application.Next{
		Tracker: tracker, Vault: vault, Worktrees: worktrees, Landing: landing,
		Checks: checkStub{}, Slot: slotStub{}, Runner: apptest.NewFakeRunner(),
		Transcripts: transcripts,
		Seat:        "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Now:  func() time.Time { return now },
		Out:  io.Discard, Err: io.Discard,
	}, tracker
}

func TestNextGivesBackAClaimWhoseSessionNeverStarted(t *testing.T) {
	for _, held := range []struct {
		name     string
		worktree bool
	}{
		{"no worktree at all", false},
		{"a worktree nothing was done in", true},
	} {
		t.Run(held.name, func(t *testing.T) {
			ctx := context.Background()
			lands := &fakeRetryLanding{NoWorktree: !held.worktree}
			next, tracker := aNextWithAStrandedClaim(t, 2*time.Hour, lands, lands, aTranscript(""))

			report, err := next.Run(ctx, "mw-x.1")
			if err != nil {
				t.Fatalf("mw next: %v", err)
			}
			detail, err := tracker.ShowStory(ctx, "mw-x.2")
			if err != nil {
				t.Fatalf("showing the story: %v", err)
			}
			if detail.Status != application.StatusOpen || detail.Assignee != "" {
				t.Fatalf("expected mw-x.2 open and unclaimed, got status %q assignee %q", detail.Status, detail.Assignee)
			}
			comments := tracker.Comments("mw-x.2")
			if len(comments) != 1 || !strings.Contains(comments[0], "gave the claim back") {
				t.Fatalf("expected one comment saying the claim was given back, got %q", comments)
			}
			if len(report.Abandoned) != 0 || len(report.GaveBack) != 1 || report.GaveBack[0] != "mw-x.2" {
				t.Fatalf("expected mw-x.2 reported as given back and not as abandoned, got abandoned %q gave back %q", report.Abandoned, report.GaveBack)
			}
		})
	}
}

func TestNextKeepsTheClaimOfASessionThatLeftSomething(t *testing.T) {
	for _, left := range []struct {
		name       string
		age        time.Duration
		uncommited []string
		commits    int
		transcript string
	}{
		{name: "uncommitted changes", age: 2 * time.Hour, uncommited: []string{"main.go"}},
		{name: "commits ahead of the target", age: 2 * time.Hour, commits: 2},
		{name: "a transcript", age: 2 * time.Hour, transcript: "assistant: started"},
		{name: "a claim made a moment ago", age: time.Minute},
	} {
		t.Run(left.name, func(t *testing.T) {
			ctx := context.Background()
			lands := &fakeRetryLanding{UncommittedPaths: left.uncommited}
			landing := aheadOnlyOn{fakeRetryLanding: lands, branch: application.StoryBranch("mw-x.2"), commits: left.commits}
			next, tracker := aNextWithAStrandedClaim(t, left.age, landing, lands, aTranscript(left.transcript))

			report, err := next.Run(ctx, "mw-x.1")
			if err != nil {
				t.Fatalf("mw next: %v", err)
			}
			detail, err := tracker.ShowStory(ctx, "mw-x.2")
			if err != nil {
				t.Fatalf("showing the story: %v", err)
			}
			if detail.Status != application.StatusInProgress || detail.Assignee == "" {
				t.Fatalf("expected the claim kept, got status %q assignee %q", detail.Status, detail.Assignee)
			}
			if len(report.GaveBack) != 0 {
				t.Fatalf("expected nothing given back, got %q", report.GaveBack)
			}
		})
	}
}
