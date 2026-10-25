package application_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.781: on 2026-10-25 the desktop's Claude login expired, and every story
// it claimed failed in seconds as an ordinary session-failed attempt. The words
// are a harness stop of their own, held per host and mailed once.

const expiredLogin = "Failed to authenticate: OAuth session expired and could not be refreshed"

func TestAnExpiredLoginIsAHarnessStopOfItsOwnReason(t *testing.T) {
	ended := time.Date(2026, 10, 25, 10, 44, 0, 0, time.UTC)
	result := application.SessionResult{IsError: true, Subtype: "success", TerminalReason: "api_error", Said: expiredLogin}
	stop, ok := application.HarnessStopOf(result, ended)
	if !ok || stop.Reason != application.ReasonLoginExpired {
		t.Fatalf("expected %q read as %s, got %+v (%v)", expiredLogin, application.ReasonLoginExpired, stop, ok)
	}
	if want := ended.Add(application.LoginExpiredWait); !stop.Until.Equal(want) {
		t.Fatalf("expected the stop to last until %s, got %s", want, stop.Until)
	}
	words, ok := application.HarnessStopOfWords("claude\n"+expiredLogin+"\n", ended)
	if !ok || words.Reason != application.ReasonLoginExpired || words.Said != expiredLogin {
		t.Fatalf("expected the same words read from a pane, got %+v (%v)", words, ok)
	}
}

func TestAnExpiredLoginIsNotReadFromProseThatNamesIt(t *testing.T) {
	ended := time.Date(2026, 10, 25, 10, 44, 0, 0, time.UTC)
	for _, said := range []string{
		"the test for Failed to authenticate: OAuth session expired passes",
		"Done: the OAuth session expired branch is handled",
	} {
		if stop, ok := application.HarnessStopOf(application.SessionResult{Subtype: "success", Said: said}, ended); ok {
			t.Fatalf("expected a finished session not read as a stop, got %+v", stop)
		}
		if stop, ok := application.HarnessStopOfWords("I wrote: "+said, ended); ok {
			t.Fatalf("expected prose not read as a stop, got %+v", stop)
		}
	}
}

func TestAnUnreachableAPIIsAHarnessStopThatWaitsAQuarterHour(t *testing.T) {
	ended := time.Date(2026, 10, 25, 10, 44, 0, 0, time.UTC)
	for _, said := range []string{
		"API Error: Can't reach the API server — check your internet or DNS (EAI_AGAIN)",
		"API Error: Connection refused — a firewall or proxy may be blocking it",
		"API Error: Connection dropped (ECONNRESET)",
	} {
		stop, ok := application.HarnessStopOf(application.SessionResult{IsError: true, Subtype: "success", Said: said}, ended)
		if !ok || stop.Reason != application.ReasonUnreachable || !stop.Until.Equal(ended.Add(application.HarnessStopWait)) {
			t.Fatalf("expected %q read as %s for a quarter hour, got %+v (%v)", said, application.ReasonUnreachable, stop, ok)
		}
	}
}

// loginExpiredNext is a mw next for the factory's host, with a mailbox, ready to
// close out stories whose results are the expired login.
func loginExpiredNext(t *testing.T, now time.Time) (application.Next, *apptest.FakeTracker, *apptest.FakeMailbox, application.Dispatch, *apptest.FakeRunner, string) {
	t.Helper()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	mailbox := apptest.NewFakeMailbox()
	lands := &fakeRetryLanding{}
	next := application.Next{
		Tracker: tracker, Vault: dispatch.Boot.Vault, Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Runner: runner,
		Seat: "builder", Host: "vps", Rigs: dispatch.Rigs,
		Memory: tracker, Mailbox: mailbox,
		Now: func() time.Time { return now },
		Out: io.Discard, Err: io.Discard,
	}
	return next, tracker, mailbox, dispatch, runner, vaultDir
}

func sessionEndedWithExpiredLogin(t *testing.T, tracker *apptest.FakeTracker, runner *apptest.FakeRunner, vaultDir, id string, claimed time.Time) string {
	t.Helper()
	claimedStory(t, tracker, id, 1)
	if err := tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.ClaimedAtField: claimed.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	name := startOldSession(t, runner, id)
	result := fmt.Sprintf(`{"type":"result","subtype":"success","is_error":true,"terminal_reason":"api_error","num_turns":4,"duration_ms":7198,"result":%q}`, expiredLogin)
	dir := filepath.Join(vaultDir, application.RunsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, application.ResultFileName), []byte(result), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestNextHoldsTheHostForAnExpiredLoginAndMailsTheMayorOncePerHost(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 10, 44, 0, 0, time.UTC)
	next, tracker, mailbox, _, runner, vaultDir := loginExpiredNext(t, now)

	for _, id := range []string{"mw-gq6.9", "mw-gq6.10"} {
		sessionEndedWithExpiredLogin(t, tracker, runner, vaultDir, id, now.Add(-time.Minute))
		closed, _ := next.Run(ctx, id)
		if closed.Reason != application.ReasonLoginExpired {
			t.Fatalf("expected %s stopped for %s, got %q: %s", id, application.ReasonLoginExpired, closed.Reason, closed.Why)
		}
	}
	note, _ := tracker.Note(ctx, application.HarnessHoldKey("vps"))
	if want := now.Add(-time.Minute).Add(7*time.Second).Add(application.LoginExpiredWait).Format(time.RFC3339) + " login-expired "; !strings.HasPrefix(note, want) {
		t.Fatalf("expected the host's hold note to start %q, got %q", want, note)
	}
	mails, err := mailbox.Inbox(ctx, application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(mails) != 1 {
		t.Fatalf("expected one mail to the Mayor for two stories on one host, got %d: %+v", len(mails), mails)
	}
	if !strings.Contains(mails[0].Subject, "vps") || !strings.Contains(mails[0].Body, "log in") {
		t.Fatalf("expected the mail to name the host and the login, got %+v", mails[0])
	}
}

func TestADispatchClaimsNothingOnAHostWhoseLoginExpiredAndGivesTheStoryBackOnceItIsCleared(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 10, 44, 0, 0, time.UTC)
	next, tracker, _, dispatch, runner, vaultDir := loginExpiredNext(t, now)
	name := sessionEndedWithExpiredLogin(t, tracker, runner, vaultDir, "mw-gq6.9", now.Add(-time.Minute))
	if closed, _ := next.Run(ctx, "mw-gq6.9"); closed.Reason != application.ReasonLoginExpired {
		t.Fatalf("expected the close-out to stop for the login, got %q", closed.Reason)
	}
	runner.Exit(name, 0)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story ready to be worked"})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Started) != 0 || len(report.HarnessRetried) != 0 || report.HarnessHold == nil || report.HarnessHold.Reason != application.ReasonLoginExpired {
		t.Fatalf("expected the host held for the login with nothing started, got %+v", report)
	}

	// The Governor logs in and clears the hold: well before the hold's time.
	if err := tracker.ClearNote(ctx, application.HarnessHoldKey("vps")); err != nil {
		t.Fatal(err)
	}
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.HarnessRetried) != 1 || report.HarnessHold != nil {
		t.Fatalf("expected the stopped story given back at once, got %+v", report)
	}
	started := false
	for _, one := range report.Started {
		if one.StoryID == "mw-gq6.9" && one.Attempt == 1 {
			started = true
		}
	}
	if !started {
		t.Fatalf("expected the story started again as attempt 1, the one the login stopped not counted, got %+v", report.Started)
	}
}

func TestStatusNamesTheHostHeldForALoginAndTheCommandThatClearsIt(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	hold := statusNow.Add(time.Hour).Format(time.RFC3339) + " login-expired " + expiredLogin
	if err := tracker.SetNote(context.Background(), application.HarnessHoldKey("vps"), hold); err != nil {
		t.Fatal(err)
	}
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	printed := report.String()
	for _, want := range []string{"HELD · Claude login expired on vps", `bd kv set host.vps.harness_hold ""`} {
		if !strings.Contains(printed, want) {
			t.Errorf("expected status to say %q, got:\n%s", want, printed)
		}
	}
}
