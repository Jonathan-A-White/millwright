package application_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// mw-gq6.1590 (folds mw-gq6.1359, .1360, .1361): mw next, mw retry and mw land
// hand their Runner to the use case, and a session they start must wait for
// claude --version as mw dispatch's does (HarnessReadyRunner). The wiring itself
// is pinned in cmd/mw (TestNoCommandHandsARawTmuxRunnerToASessionStart).

// claudeAnsweringAfter is a HarnessReadyRunner over runner whose claude answers
// from the third ask on, and which fails the test if a session started earlier.
func claudeAnsweringAfter(t *testing.T, runner application.Runner, asks *atomic.Int32, started func() int) application.HarnessReadyRunner {
	t.Helper()
	return application.HarnessReadyRunner{Runner: runner, Host: "laptop", Out: io.Discard,
		Sleep: func(context.Context, time.Duration) error { return nil },
		Ready: func(context.Context) error {
			n := asks.Add(1)
			if started() != 0 {
				t.Errorf("ask %d: a session started before claude answered", n)
			}
			if n < 3 {
				return errors.New("claude: command not found")
			}
			return nil
		}}
}

// A Next's send-backs (a rebase, the merged tests) start a Builder session
// through its Runner; it starts only once claude answers.
func TestANextWaitsForClaude(t *testing.T) {
	for name, lands := range map[string]application.Landing{
		"merge conflict":    &aConflictingLanding{fakeRetryLanding{AheadCount: 1}},
		"merged tests fail": &mergingLanding{fakeRetryLanding{AheadCount: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			id := "mw-x.8"
			next := aCloseOutOn("laptop", id, aSlot{}, aCrossSlotOn(apptest.NewFakeTracker(), "laptop", new(atomic.Int32)),
				lands, &scriptedChecks{passes: []bool{true, false}})
			fake := apptest.NewFakeRunner()
			var asks atomic.Int32
			next.Runner = claudeAnsweringAfter(t, fake, &asks, func() int { return len(fake.Started()) })

			report, _ := next.Run(context.Background(), id)
			if asks.Load() != 3 {
				t.Errorf("expected claude asked for 3 times before the send-back started, got %d (report %+v)", asks.Load(), report)
			}
			if names := fake.Started(); len(names) != 1 || report.SentBack == "" {
				t.Errorf("expected one send-back session started once claude answered, got %v and %+v", names, report)
			}
		})
	}
}

// A fixer of mw land's batch starts through the Next's Runner as well.
func TestALandFixerWaitsForClaude(t *testing.T) {
	vault := newFakeVault()
	landing := &leftoversLanding{fakeRetryLanding: fakeRetryLanding{}}
	fake := apptest.NewFakeRunner()
	var asks atomic.Int32
	land := application.BatchLand{
		Next: application.Next{
			Landing: landing, Runner: claudeAnsweringAfter(t, endedRunner{fake, application.SessionStatus{State: application.StateExited}}, &asks, func() int { return len(fake.Started()) }),
			Boot:  application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "laptop"},
			Vault: vault, Host: "laptop", Seat: "builder", Out: io.Discard, Err: io.Discard,
		},
		Records: diffRecords{}, Fix: true,
	}
	report := &application.BatchReport{
		Rig: "millwright", Target: "main", Number: 4, Base: "base0123",
		Queue:    []application.BatchStory{{ID: "mw-r9xo1pj.4", Title: "The set-aside story"}},
		SetAside: []application.BatchStory{{ID: "mw-r9xo1pj.4", Title: "The set-aside story", Why: "conflicts in two unit test files", Conflicts: []string{"CONFLICT (content): a_test.ts"}}},
	}
	land.FixTheBatch(context.Background(), "/rigs/millwright/.landing-1", report)
	if asks.Load() != 3 || len(fake.Started()) != 1 {
		t.Errorf("expected claude asked for 3 times and then one fixer started, got %d asks and %v", asks.Load(), fake.Started())
	}
}

// A retry gives a claim back and starts nothing: the next dispatch tick starts
// the story's session, through its own HarnessReadyRunner. Its Runner is read
// for the session's status alone, so a retry handed the wrapped Runner asks for
// no claude and starts no session.
func TestARetryWaitsForClaude(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	fake := apptest.NewFakeRunner()
	var asks atomic.Int32
	var out strings.Builder
	retry := aRetry(tracker, &fakeRetryLanding{AheadCount: 1}, &apptest.FakeVaultFiles{}, &out)
	retry.Runner = claudeAnsweringAfter(t, fake, &asks, func() int { return len(fake.Started()) })

	if _, err := retry.Run(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatalf("retrying: %v\n%s", err, out.String())
	}
	if names := fake.Started(); len(names) != 0 || asks.Load() != 0 {
		t.Errorf("expected a retry to start no session, got %v after %d asks for claude", names, asks.Load())
	}
}
