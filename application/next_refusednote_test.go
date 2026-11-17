package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1417: a refused close-out's note said the next dispatch tick takes the
// story as attempt N while the story was left run:blocked, which no tick takes.
// mw-gq6.1409 since clears the run state before the claim is given back, so the
// words are true for each reason that gives the claim back; these tests hold the
// words and the story's state to each other, whichever reason it is.

func stoppedStory(t *testing.T, reason application.Reason) (*apptest.FakeTracker, string, error) {
	t.Helper()
	dispatch, tracker, _, _, _ := aFactory(t)
	claimedStory(t, tracker, "mw-gq6.9", 1)
	n := application.Next{
		Tracker: tracker, Vault: dispatch.Boot.Vault, Memory: tracker, Mailbox: apptest.NewFakeMailbox(), Files: &apptest.FakeVaultFiles{},
		Seat: "builder", Host: "laptop", Now: func() time.Time { return time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC) },
		Out: io.Discard, Err: io.Discard,
	}
	_, err := n.StopForTest(context.Background(), "mw-gq6.9", 1, reason, "a stop for the test")
	return tracker, strings.Join(tracker.Comments("mw-gq6.9"), "\n---\n"), err
}

func runStateOf(t *testing.T, tracker *apptest.FakeTracker) string {
	t.Helper()
	state, err := tracker.StoryState(context.Background(), "mw-gq6.9", application.RunState)
	if err != nil {
		t.Fatalf("reading the run state: %v", err)
	}
	return state
}

func TestAStopThatGivesTheClaimBackLeavesTheStoryOpenWithNoRunStateAsItsNoteSays(t *testing.T) {
	for _, reason := range []application.Reason{
		application.ReasonOpenSteps, application.ReasonNoClosingComment, application.ReasonAcceptanceChanged,
		application.ReasonGristExampleUnproved, application.ReasonTurnCap,
		application.ReasonHarnessCrash, application.ReasonHarnessMissing,
	} {
		t.Run(string(reason), func(t *testing.T) {
			tracker, note, _ := stoppedStory(t, reason)
			if !strings.Contains(note, "the next dispatch tick takes it as attempt 2") {
				t.Fatalf("expected the note to say the next dispatch tick takes the story as attempt 2, got:\n%s", note)
			}
			// What makes those words true: open, unclaimed and with no run:blocked.
			detail, err := tracker.ShowStory(context.Background(), "mw-gq6.9")
			if err != nil {
				t.Fatalf("reading the story: %v", err)
			}
			if detail.Status != application.StatusOpen || detail.Assignee != "" {
				t.Errorf("expected the story open and unclaimed, got status %q held by %q", detail.Status, detail.Assignee)
			}
			if state := runStateOf(t, tracker); state != "" {
				t.Errorf("expected no run state on the story its note says a tick takes, got %q", state)
			}
			for _, label := range detail.Labels {
				if strings.HasPrefix(label, application.RunState+":") {
					t.Errorf("expected no run label on the story its note says a tick takes, got %q", label)
				}
			}
		})
	}
}

func TestAStopThatKeepsTheClaimSaysSoAndLeavesTheStoryBlocked(t *testing.T) {
	for _, reason := range []application.Reason{application.ReasonTestsFail, application.ReasonNoResult} {
		t.Run(string(reason), func(t *testing.T) {
			tracker, note, _ := stoppedStory(t, reason)
			if strings.Contains(note, "dispatch tick takes it") || !strings.Contains(note, "the claim was not given back") {
				t.Errorf("expected the note to say the claim was not given back and no tick takes the story, got:\n%s", note)
			}
			if state := runStateOf(t, tracker); state != application.RunBlocked {
				t.Errorf("expected the story left %s=%s, got %q", application.RunState, application.RunBlocked, state)
			}
		})
	}
}
