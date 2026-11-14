package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1281: the home's cloud check replaced cloud8's Claude Code while the
// box's own dispatch started mw-gq6.1247, whose session died with "nice: claude:
// No such file or directory". A session starts only once claude --version
// answers on the host.
func TestADispatchWaitsForClaudeToAnswerBeforeItStartsTheSession(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	asks := 0
	var waits []time.Duration
	dispatch.Runner = application.HarnessReadyRunner{Runner: runner, Host: "vps", Sleep: func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}, Ready: func(context.Context) error {
		asks++
		if n := len(runner.Names()); n != 0 {
			t.Errorf("ask %d: expected no session started before claude answers, got %q", asks, runner.Names())
		}
		if asks < 3 {
			return errors.New("claude: command not found")
		}
		return nil
	}}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 1 || len(runner.Names()) != 1 {
		t.Fatalf("expected the story started once claude answered, got %+v", report)
	}
	if asks != 3 || len(waits) != 2 || waits[0] != application.DefaultHarnessPoll {
		t.Errorf("expected 3 asks with 2 waits of %s between, got %d asks and waits %v", application.DefaultHarnessPoll, asks, waits)
	}
}

func TestADispatchWhoseClaudeNeverAnswersGivesTheClaimBackAndStartsNothing(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	dispatch.Runner = application.HarnessReadyRunner{Runner: runner, Host: "vps", Tries: 3,
		Sleep: func(context.Context, time.Duration) error { return nil },
		Ready: func(context.Context) error { return errors.New("claude: command not found") }}

	report, err := dispatch.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "claude --version did not answer") {
		t.Fatalf("expected the dispatch to say claude never answered, got %v", err)
	}
	if len(report.Started) != 0 || len(runner.Names()) != 0 {
		t.Fatalf("expected nothing started, got %+v", report)
	}
	if len(report.Failed) != 1 || !report.Failed[0].Released {
		t.Fatalf("expected one failure with the claim given back, got %+v", report.Failed)
	}
	if detail, err := tracker.ShowStory(ctx, "mw-gq6.1"); err != nil || detail.Status != application.StatusOpen || detail.Attempts != 0 {
		t.Errorf("expected the story open with no attempt counted, got %+v (%v)", detail, err)
	}
}
