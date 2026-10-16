package application_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A lostNotes is a note store whose writes always fail with err, as a tracker
// does when bd's dolt circuit breaker is open for good.
type lostNotes struct {
	*apptest.FakeTracker
	err error
}

func (l lostNotes) SetNote(context.Context, string, string) error { return l.err }

func TestARefusalWhoseContinueNoteIsLostMailsTheMayorALostWriteNamingTheStoryAndTheCommand(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	mailbox := apptest.NewFakeMailbox()
	failure := errors.New("bd kv set continue.mw-x.1 1: exit status 1: dolt circuit breaker is open: server appears down, failing fast (cooldown 5s)")

	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &scriptedChecks{passes: []bool{false}}, Slot: aSlot{}, Mailbox: mailbox,
		Memory: lostNotes{tracker, failure}, RetryFromBranch: true,
		RetryWait: func(context.Context, time.Duration) error { return nil },
		Seat:      "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")

	if err == nil || !report.Refused || report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", err, report)
	}
	mail, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	var lost []application.Message
	for _, m := range mail {
		if strings.HasPrefix(m.Subject, "lost write") {
			lost = append(lost, m)
		}
	}
	if len(lost) != 1 {
		t.Fatalf("expected one lost write mail to the Mayor, got %+v", mail)
	}
	for _, want := range []string{"mw-x.1", "bd kv set continue.mw-x.1 1"} {
		if !strings.Contains(lost[0].Subject+lost[0].Body, want) {
			t.Errorf("expected the lost write mail to name %q, got %+v", want, lost[0])
		}
	}
}
