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
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// mw-gq6.375: a close-out whose bd writes met the Dolt server's i/o timeout
// said so only on the pane's stderr, so the merged-tests send-back and its mail
// were lost and mw dispatch refunded the dead pane. These tests walk a close-out
// of a merged result whose tests fail through writes that time out a few times,
// and through writes that always do.

// flakyWrites is a tracker whose first fails writes of each kind — metadata, a
// state, a comment — fail with err; fails of 0 writes them all.
type flakyWrites struct {
	*apptest.FakeTracker
	fails int
	err   error

	calls map[string]int
}

func (f *flakyWrites) meet(kind string) error {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[kind]++
	if f.calls[kind] <= f.fails {
		return f.err
	}
	return nil
}

func (f *flakyWrites) SetStoryMetadata(ctx context.Context, id string, fields map[string]string) error {
	if err := f.meet("metadata"); err != nil {
		return err
	}
	return f.FakeTracker.SetStoryMetadata(ctx, id, fields)
}

func (f *flakyWrites) SetStoryState(ctx context.Context, id, dimension, value, reason string) error {
	if err := f.meet("state " + dimension); err != nil {
		return err
	}
	return f.FakeTracker.SetStoryState(ctx, id, dimension, value, reason)
}

func (f *flakyWrites) CommentOnStory(ctx context.Context, id, text string) error {
	if err := f.meet("comment"); err != nil {
		return err
	}
	return f.FakeTracker.CommentOnStory(ctx, id, text)
}

// flakyMail is a mailbox whose first fails sends fail with err.
type flakyMail struct {
	*apptest.FakeMailbox
	fails int
	err   error

	sends int
}

func (f *flakyMail) Send(ctx context.Context, message application.NewMessage) (string, error) {
	f.sends++
	if f.sends <= f.fails {
		return "", f.err
	}
	return f.FakeMailbox.Send(ctx, message)
}

// mergingLanding merges cleanly into a merge commit, so that the rig's tests
// are asked again on the merged result.
type mergingLanding struct{ fakeRetryLanding }

func (*mergingLanding) Merge(context.Context, string, string) (application.Landed, error) {
	return application.Landed{Commit: "0123456789abcdef"}, nil
}

type mergedFailRun struct {
	report  application.NextReport
	err     error
	tracker *flakyWrites
	mail    *flakyMail
	vault   *fakeVault
	events  *apptest.FakeEventLog
	runner  *apptest.FakeRunner
	waits   []time.Duration
	stderr  *strings.Builder
}

// aMergedResultWhoseTestsFail closes out a story whose branch merges into main
// without conflicts and whose tests then fail on the merged result, with a
// Builder session to send it back to, over writes that time out the first
// fails times.
func aMergedResultWhoseTestsFail(t *testing.T, fails int) *mergedFailRun {
	t.Helper()
	tracker := &flakyWrites{FakeTracker: apptest.NewFakeTracker(), fails: fails, err: doltTimeout()}
	tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-gq6.9/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &mergingLanding{fakeRetryLanding{AheadCount: 1}}
	run := &mergedFailRun{
		tracker: tracker, vault: vault, events: &apptest.FakeEventLog{}, runner: apptest.NewFakeRunner(),
		mail:   &flakyMail{FakeMailbox: apptest.NewFakeMailbox(), fails: fails, err: doltTimeout()},
		stderr: &strings.Builder{},
	}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands, Rules: apptest.NewFakeEpicRules(),
		Checks: &scriptedChecks{passes: []bool{true, false}}, Slot: aSlot{},
		Runner: run.runner, Mailbox: run.mail, Events: run.events,
		Boot:     application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "laptop"},
		PushWait: 20 * time.Second,
		RetryWait: func(_ context.Context, d time.Duration) error {
			run.waits = append(run.waits, d)
			return nil
		},
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: run.stderr,
	}.Run(context.Background(), "mw-gq6.9")
	return run
}

func TestASendBackWhoseWritesTimeOutTwiceIsRetriedAndLands(t *testing.T) {
	ctx := context.Background()
	run := aMergedResultWhoseTestsFail(t, 2)

	if run.report.SentBack == "" || run.report.Reason != application.ReasonMergedTestsFail {
		t.Fatalf("expected the story sent back to fix the merged tests, got err %v and %+v", run.err, run.report)
	}
	if state, _ := run.tracker.StoryState(ctx, "mw-gq6.9", application.MergedTestsState); state != application.MergedTestsSentBack {
		t.Fatalf("expected %s=%s recorded, got %q", application.MergedTestsState, application.MergedTestsSentBack, state)
	}
	if state, _ := run.tracker.StoryState(ctx, "mw-gq6.9", application.RunState); state != application.RunRunning {
		t.Fatalf("expected %s=%s recorded for the fresh session, got %q", application.RunState, application.RunRunning, state)
	}
	if comments := run.tracker.Comments("mw-gq6.9"); len(comments) != 1 {
		t.Fatalf("expected one comment on the story, got %q", comments)
	}
	inbox, _ := run.mail.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 || !strings.HasPrefix(inbox[0].Subject, application.MailSentBack) {
		t.Fatalf("expected one %q mail, got %+v", application.MailSentBack, inbox)
	}
	if _, ok := run.vault.written["mw-gq6.9/"+application.UnrecordedFileName]; ok {
		t.Fatalf("expected nothing kept as unrecorded, the writes all landed")
	}
	if notes := notesNaming(run.report, "write of the merged-tests state failed"); len(notes) != 2 {
		t.Fatalf("expected a note per retry of the merged-tests state, got %q", notes)
	}
}

func TestAStopWhoseWritesAlwaysTimeOutKeepsItsOutcomeInTheRunAndTheEventsLog(t *testing.T) {
	ctx := context.Background()
	run := aMergedResultWhoseTestsFail(t, 1000)

	if run.err == nil || run.report.Reason != application.ReasonMergedTestsFail || run.report.SentBack != "" {
		t.Fatalf("expected a merged-tests-fail stop, got err %v and %+v", run.err, run.report)
	}
	kept, ok := run.vault.written["mw-gq6.9/"+application.UnrecordedFileName]
	if !ok {
		t.Fatalf("expected the outcome kept in the run, the run holds %v", run.vault.written)
	}
	for _, want := range []string{`"reason": "merged-tests-fail"`, `"attempt": 1`, `"comment"`, `"run-state"`, `"mail"`, `"mail_subject": "Blocked: A story"`, "i/o timeout"} {
		if !strings.Contains(kept, want) {
			t.Errorf("expected the kept outcome to hold %q, got:\n%s", want, kept)
		}
	}
	evs, _ := run.events.Since(ctx, 0)
	if len(evs) == 0 || evs[len(evs)-1].Kind != events.KindJob || evs[len(evs)-1].To != events.JobFailed ||
		!strings.Contains(evs[len(evs)-1].Detail, "mw-gq6.9") {
		t.Fatalf("expected a failed job event naming the story, got %+v", evs)
	}
	if inbox, _ := run.mail.Inbox(ctx, application.MayorMailbox); len(inbox) != 0 {
		t.Fatalf("expected no mail to have got through, got %+v", inbox)
	}
}

func TestADeadPaneWhoseCloseOutKeptItsOutcomeIsReplayedAndNotRefunded(t *testing.T) {
	ctx := context.Background()
	run := aMergedResultWhoseTestsFail(t, 1000)
	kept := run.vault.written["mw-gq6.9/"+application.UnrecordedFileName]
	if kept == "" {
		t.Fatalf("expected the outcome kept in the run, got %v", run.vault.written)
	}

	dispatch, tracker, runner, _ := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{})
	mail := apptest.NewFakeMailbox()
	dispatch.Mailbox, dispatch.Memory = mail, tracker
	if _, err := dispatch.Boot.Vault.PutRunFile(ctx, "mw-gq6.9", application.UnrecordedFileName, kept); err != nil {
		t.Fatalf("putting the kept outcome in the run: %v", err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Reclaimed) != 0 || len(report.Started) != 0 {
		t.Fatalf("expected the story neither given back nor started again, got reclaimed %+v started %+v", report.Reclaimed, report.Started)
	}
	if len(report.HeldRefused) != 1 || report.HeldRefused[0].StoryID != "mw-gq6.9" {
		t.Fatalf("expected the story held as refused, got %+v", report.HeldRefused)
	}
	detail, _ := tracker.ShowStory(ctx, "mw-gq6.9")
	if detail.Attempts != 1 || detail.AttemptsRefunded != 0 || detail.Assignee == "" {
		t.Fatalf("expected the attempt counted and the claim kept, got %d attempts, %d refunded, held by %q", detail.Attempts, detail.AttemptsRefunded, detail.Assignee)
	}
	if state, _ := tracker.StoryState(ctx, "mw-gq6.9", application.RunState); state != application.RunBlocked {
		t.Fatalf("expected %s=%s recorded, got %q", application.RunState, application.RunBlocked, state)
	}
	if !strings.Contains(strings.Join(tracker.Comments("mw-gq6.9"), "\n"), "merged-tests-fail") {
		t.Fatalf("expected a comment naming merged-tests-fail, got %q", tracker.Comments("mw-gq6.9"))
	}
	inbox, _ := mail.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 || !strings.HasPrefix(inbox[0].Subject, application.MailBlocked) {
		t.Fatalf("expected one %q mail, got %+v", application.MailBlocked, inbox)
	}
	if closed := runner.Closed(); len(closed) != 0 {
		t.Fatalf("expected the dead window left as the evidence, got %q closed", closed)
	}

	// Another tick finds the story blocked and the record replayed: nothing is
	// written twice.
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("expected the second dispatch to run cleanly, got %v", err)
	}
	if again, _ := mail.Inbox(ctx, application.MayorMailbox); len(again) != 1 {
		t.Fatalf("expected the mail sent once, got %d", len(again))
	}
	if comments := tracker.Comments("mw-gq6.9"); len(comments) != 1 {
		t.Fatalf("expected the comment written once, got %q", comments)
	}
}
