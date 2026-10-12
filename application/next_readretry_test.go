package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A flakyReads is a work tracker whose reads of a story's comments fail the
// first commentFails times with err, and whose show fails the first showFails
// times, and work after that.
type flakyReads struct {
	*apptest.FakeTracker
	commentFails, showFails int
	err                     error

	comments, shows int
}

func (f *flakyReads) StoryComments(ctx context.Context, id string) ([]application.Comment, error) {
	f.comments++
	if f.comments <= f.commentFails {
		return nil, f.err
	}
	return f.FakeTracker.StoryComments(ctx, id)
}

func (f *flakyReads) ShowStory(ctx context.Context, id string) (application.StoryDetail, error) {
	f.shows++
	if f.shows <= f.showFails {
		return application.StoryDetail{}, f.err
	}
	return f.FakeTracker.ShowStory(ctx, id)
}

// A doltTimeout is the failure mw-5r3p30.227's close-out met reading its
// comments, as the bd adapter reports it.
func doltTimeout() error {
	return &application.TransientFault{
		Why: "dolt server unreachable",
		Err: errors.New("bd comments mw-x.1 --json: exit status 1: failed to open database: Dolt server unreachable at 10.88.0.2:3307: dial tcp 10.88.0.2:3307: i/o timeout"),
	}
}

type readRetryRun struct {
	report application.NextReport
	err    error
	waits  []time.Duration
	tracks *flakyReads
}

func aCloseOutOverFlakyReads(t *testing.T, flaky *flakyReads) *readRetryRun {
	t.Helper()
	flaky.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	flaky.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	rules := apptest.NewFakeEpicRules()
	rules.Require("millwright", domain.EpicRequirements{VersionFiles: []string{"VERSION"}, ChangelogFiles: []string{"CHANGELOG.md"}})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	run := &readRetryRun{tracks: flaky}
	run.report, run.err = application.Next{
		Tracker: flaky, Vault: vault, Worktrees: lands, Landing: lands, Rules: rules,
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{},
		PushWait: 20 * time.Second,
		RetryWait: func(_ context.Context, d time.Duration) error {
			run.waits = append(run.waits, d)
			return nil
		},
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return run
}

func TestACommentsReadThatMeetsAPassingTimeoutOnceIsRetriedAndTheStoryLands(t *testing.T) {
	run := aCloseOutOverFlakyReads(t, &flakyReads{FakeTracker: apptest.NewFakeTracker(), commentFails: 1, err: doltTimeout()})

	if run.err != nil || !run.report.Landed {
		t.Fatalf("expected the story landed on the second read, got err %v and %+v", run.err, run.report)
	}
	// The failed read, its retry, and the landing mail's own read for the rig
	// memory block.
	if run.tracks.comments != 3 {
		t.Fatalf("expected 3 reads of the comments, got %d", run.tracks.comments)
	}
	notes := notesNaming(run.report, "read of the comments failed")
	want := []string{"read of the comments failed (dolt server unreachable), retried in 5s"}
	if fmt.Sprint(notes) != fmt.Sprint(want) {
		t.Fatalf("expected one note per retry, %q, got %q", want, notes)
	}
}

func TestACommentsReadThatKeepsMeetingTheTimeoutStopsAfterAFewTriesUnderAMinuteQuotingBd(t *testing.T) {
	run := aCloseOutOverFlakyReads(t, &flakyReads{FakeTracker: apptest.NewFakeTracker(), commentFails: 100, err: doltTimeout()})

	if run.err == nil || run.report.Landed || run.report.Reason != application.ReasonLandingFailed {
		t.Fatalf("expected a landing-failed stop, got err %v and %+v", run.err, run.report)
	}
	if want := "Dolt server unreachable at 10.88.0.2:3307: dial tcp 10.88.0.2:3307: i/o timeout"; !strings.Contains(run.err.Error(), want) {
		t.Fatalf("expected bd's words %q in the error, got %v", want, run.err)
	}
	if run.tracks.comments != application.ReadRetries+1 {
		t.Fatalf("expected %d tries, got %d", application.ReadRetries+1, run.tracks.comments)
	}
	var total time.Duration
	for _, d := range run.waits {
		total += d
	}
	if len(run.waits) != application.ReadRetries || total >= time.Minute {
		t.Fatalf("expected %d waits under a minute in all, got %v", application.ReadRetries, run.waits)
	}
}

func TestAReadThatFailsForAnotherReasonIsNotRetried(t *testing.T) {
	run := aCloseOutOverFlakyReads(t, &flakyReads{FakeTracker: apptest.NewFakeTracker(), commentFails: 100, err: errors.New("bd comments mw-x.1 --json: exit status 1: no issue found matching \"mw-x.1\"")})

	if run.err == nil || run.report.Landed {
		t.Fatalf("expected the landing to fail, got err %v and %+v", run.err, run.report)
	}
	if run.tracks.comments != 1 || len(run.waits) != 0 {
		t.Fatalf("expected one read and no wait, got %d reads and waits %v", run.tracks.comments, run.waits)
	}
	if notes := notesNaming(run.report, "retried"); len(notes) != 0 {
		t.Fatalf("expected no retry note, got %q", notes)
	}
}

func TestTheShowThatOpensACloseOutIsRetriedOnAPassingTimeToo(t *testing.T) {
	run := aCloseOutOverFlakyReads(t, &flakyReads{FakeTracker: apptest.NewFakeTracker(), showFails: 2, err: doltTimeout()})

	if run.err != nil || !run.report.Landed {
		t.Fatalf("expected the story landed, got err %v and %+v", run.err, run.report)
	}
	if run.tracks.shows < 3 {
		t.Fatalf("expected at least 3 shows, got %d", run.tracks.shows)
	}
	if notes := notesNaming(run.report, "read of the story failed"); len(notes) != 2 {
		t.Fatalf("expected a note per retry, got %q", notes)
	}
}
