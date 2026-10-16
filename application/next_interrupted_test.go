package application_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// mw-gq6.441: the predecessor Mayor's background shell ended while its
// close-out of mw-ev7oyy.14 was running the rig's tests. The cancel of the
// caller came back as the tests not running, and bd and git were cancelled
// with it, so mw next wrote a tests-not-run refusal and the replay file mw
// dispatch would have recorded as run=blocked, a Refused mail and an attempt
// spent, though the story's code was never judged.

// cancelledWrites is a tracker that writes as bd does under a cancelled
// context: not at all. down makes every write fail as bd's server being
// unreachable does, whatever the context.
type cancelledWrites struct {
	*apptest.FakeTracker
	down bool
}

func (f *cancelledWrites) refuse(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.down {
		return doltTimeout()
	}
	return nil
}

func (f *cancelledWrites) SetStoryMetadata(ctx context.Context, id string, fields map[string]string) error {
	if err := f.refuse(ctx); err != nil {
		return err
	}
	return f.FakeTracker.SetStoryMetadata(ctx, id, fields)
}

func (f *cancelledWrites) SetStoryState(ctx context.Context, id, dimension, value, reason string) error {
	if err := f.refuse(ctx); err != nil {
		return err
	}
	return f.FakeTracker.SetStoryState(ctx, id, dimension, value, reason)
}

func (f *cancelledWrites) CommentOnStory(ctx context.Context, id, text string) error {
	if err := f.refuse(ctx); err != nil {
		return err
	}
	return f.FakeTracker.CommentOnStory(ctx, id, text)
}

// cancelledMail is a mailbox that sends nothing under a cancelled context, or
// when bd is down.
type cancelledMail struct {
	*apptest.FakeMailbox
	down bool
}

func (f *cancelledMail) Send(ctx context.Context, message application.NewMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.down {
		return "", doltTimeout()
	}
	return f.FakeMailbox.Send(ctx, message)
}

// interruptedChecks are the rig's tests as the rig adapter runs them: when
// cancel is set, the caller goes away while they run, and they block until
// the context is done and say they could not be run, as a killed `make test`
// does; otherwise they fail.
type interruptedChecks struct {
	cancel context.CancelFunc
	runs   int
}

func (c *interruptedChecks) Run(ctx context.Context, dir, _ string) (application.Checked, error) {
	c.runs++
	if c.cancel == nil {
		return application.Checked{Command: "make test", Output: "--- FAIL: TestSomething\n"}, nil
	}
	c.cancel()
	<-ctx.Done()
	return application.Checked{}, fmt.Errorf("make test in %s: %w", dir, ctx.Err())
}

type interruptedRun struct {
	report  application.NextReport
	err     error
	tracker *cancelledWrites
	mail    *cancelledMail
	vault   *fakeVault
	before  application.StoryDetail
}

// aCloseOutWhoseTestsAre closes out a finished story while its tests run:
// interrupted, its caller goes away while they run; otherwise they fail. down
// says bd is unreachable for every write.
func aCloseOutWhoseTestsAre(t *testing.T, interrupted, down bool) *interruptedRun {
	t.Helper()
	tracker := &cancelledWrites{FakeTracker: apptest.NewFakeTracker(), down: down}
	tracker.AddEpic("mw-ev7oyy", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "opus", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-ev7oyy", domain.Story{ID: "mw-ev7oyy.14", Title: "A story"})
	if err := tracker.ClaimStory(context.Background(), "mw-ev7oyy.14"); err != nil {
		t.Fatalf("claiming the story: %v", err)
	}
	before, _ := tracker.ShowStory(context.Background(), "mw-ev7oyy.14")
	vault := newFakeVault()
	vault.written["mw-ev7oyy.14/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checks := &interruptedChecks{}
	if interrupted {
		checks.cancel = cancel
	}
	run := &interruptedRun{tracker: tracker, mail: &cancelledMail{FakeMailbox: apptest.NewFakeMailbox(), down: down}, vault: vault, before: before}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: checks, Slot: aSlot{}, Mailbox: run.mail,
		PushWait:  20 * time.Second,
		RetryWait: func(context.Context, time.Duration) error { return nil },
		Seat:      "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-ev7oyy.14")
	if checks.runs != 1 {
		t.Fatalf("expected the rig's tests run once, got %d (err %v)", checks.runs, run.err)
	}
	return run
}

func TestACloseOutInterruptedWhileItsTestsRunRecordsNoOutcome(t *testing.T) {
	ctx := context.Background()
	run := aCloseOutWhoseTestsAre(t, true, false)

	if run.err == nil {
		t.Fatalf("expected the interrupted close-out to say so, got no error and %+v", run.report)
	}
	for _, want := range []string{"interrupted", "mw next mw-ev7oyy.14 --no-dispatch"} {
		if !strings.Contains(run.err.Error(), want) {
			t.Errorf("expected the error to hold %q, got %v", want, run.err)
		}
	}
	if _, kept := run.vault.written["mw-ev7oyy.14/"+application.UnrecordedFileName]; kept {
		t.Fatalf("expected no %s, the run holds:\n%s", application.UnrecordedFileName, run.vault.written["mw-ev7oyy.14/"+application.UnrecordedFileName])
	}
	if state, _ := run.tracker.StoryState(ctx, "mw-ev7oyy.14", application.RunState); state == application.RunBlocked {
		t.Fatalf("expected no %s=%s, got it", application.RunState, application.RunBlocked)
	}
	if run.report.Refused || run.report.Reason != "" {
		t.Fatalf("expected no refusal reported, got refused %v reason %q", run.report.Refused, run.report.Reason)
	}
	if inbox, _ := run.mail.Inbox(ctx, application.MayorMailbox); len(inbox) != 0 {
		t.Fatalf("expected no mail to the Mayor, got %+v", inbox)
	}
	if comments := run.tracker.Comments("mw-ev7oyy.14"); len(comments) != 0 {
		t.Fatalf("expected nothing said on the story, got %q", comments)
	}
	after, _ := run.tracker.ShowStory(ctx, "mw-ev7oyy.14")
	if after.Attempts != run.before.Attempts || after.AttemptsRefunded != run.before.AttemptsRefunded || after.Assignee != run.before.Assignee || after.Assignee == "" {
		t.Fatalf("expected the claim and attempts as they were (%d attempts, held by %q), got %d attempts, %d refunded, held by %q",
			run.before.Attempts, run.before.Assignee, after.Attempts, after.AttemptsRefunded, after.Assignee)
	}
	for _, line := range run.vault.ledger {
		if strings.Contains(line, "not landed") {
			t.Fatalf("expected no not-landed line in the ledger, got %q", line)
		}
	}
	if len(run.vault.ledger) != 1 || !strings.Contains(run.vault.ledger[0], "close-out interrupted, not judged") {
		t.Fatalf("expected one ledger line saying the close-out was interrupted, got %q", run.vault.ledger)
	}
}

func TestACloseOutWhoseTestsFailStillRecordsTheRefusal(t *testing.T) {
	ctx := context.Background()
	run := aCloseOutWhoseTestsAre(t, false, false)

	if run.err == nil || run.report.Reason != application.ReasonTestsFail || strings.Contains(run.err.Error(), "interrupted") {
		t.Fatalf("expected a tests-fail refusal, got err %v and reason %q", run.err, run.report.Reason)
	}
	if state, _ := run.tracker.StoryState(ctx, "mw-ev7oyy.14", application.RunState); state != application.RunBlocked {
		t.Fatalf("expected %s=%s, got %q", application.RunState, application.RunBlocked, state)
	}
	if inbox, _ := run.mail.Inbox(ctx, application.MayorMailbox); len(inbox) != 1 || !strings.HasPrefix(inbox[0].Subject, application.MailRefused) {
		t.Fatalf("expected one %q mail, got %+v", application.MailRefused, inbox)
	}
	if _, kept := run.vault.written["mw-ev7oyy.14/"+application.UnrecordedFileName]; kept {
		t.Fatalf("expected no %s, every write landed", application.UnrecordedFileName)
	}
}

func TestACloseOutWhoseTestsFailWithBdDownKeepsTheRefusalToReplay(t *testing.T) {
	run := aCloseOutWhoseTestsAre(t, false, true)

	if run.err == nil || run.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and reason %q", run.err, run.report.Reason)
	}
	kept, ok := run.vault.written["mw-ev7oyy.14/"+application.UnrecordedFileName]
	if !ok {
		t.Fatalf("expected the refusal kept in the run for mw dispatch, the run holds %v", run.vault.written)
	}
	for _, want := range []string{`"outcome": "refused"`, `"reason": "tests-fail"`, `"run-state"`, `"mail"`} {
		if !strings.Contains(kept, want) {
			t.Errorf("expected the kept outcome to hold %q, got:\n%s", want, kept)
		}
	}
}

// killedOnTheMergedResult are the rig's tests passing on the story's branch and
// then, on the merged result, cut off by the caller going away and reported as
// a test run that did not pass, as a killed child that is not told apart from a
// failing one would be.
type killedOnTheMergedResult struct {
	cancel context.CancelFunc
	runs   int
}

func (c *killedOnTheMergedResult) Run(ctx context.Context, _, _ string) (application.Checked, error) {
	c.runs++
	if c.runs == 1 {
		return application.Checked{Command: "make test", Passed: true}, nil
	}
	c.cancel()
	<-ctx.Done()
	return application.Checked{Command: "make test", Output: "signal: terminated\n"}, nil
}

func TestALandingInterruptedWhileTheMergedResultIsTestedIsNeitherSentBackNorStopped(t *testing.T) {
	tracker := &cancelledWrites{FakeTracker: apptest.NewFakeTracker()}
	tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-gq6.9/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &mergingLanding{fakeRetryLanding{AheadCount: 1}}
	mail := &cancelledMail{FakeMailbox: apptest.NewFakeMailbox()}
	runner := apptest.NewFakeRunner()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checks := &killedOnTheMergedResult{cancel: cancel}
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands, Rules: apptest.NewFakeEpicRules(),
		Checks: checks, Slot: aSlot{}, Runner: runner, Mailbox: mail,
		Boot:      application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "laptop"},
		PushWait:  20 * time.Second,
		RetryWait: func(context.Context, time.Duration) error { return nil },
		Seat:      "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-gq6.9")

	if checks.runs != 2 {
		t.Fatalf("expected the tests run on the branch and on the merged result, got %d runs (err %v)", checks.runs, err)
	}
	if err == nil || !strings.Contains(err.Error(), application.CloseOutInterrupted("mw-gq6.9")) {
		t.Fatalf("expected the close-out to say it was interrupted, got %v", err)
	}
	if report.SentBack != "" || report.Reason != "" || !report.Interrupted {
		t.Fatalf("expected nothing sent back or stopped, got sent back to %q, reason %q", report.SentBack, report.Reason)
	}
	if started := runner.Started(); len(started) != 0 {
		t.Fatalf("expected no session started, got %+v", started)
	}
	if state, _ := tracker.StoryState(context.Background(), "mw-gq6.9", application.MergedTestsState); state != "" {
		t.Fatalf("expected no %s recorded, got %q", application.MergedTestsState, state)
	}
	for _, name := range []string{application.UnrecordedFileName, application.LandingErrorFileName} {
		if _, kept := vault.written["mw-gq6.9/"+name]; kept {
			t.Fatalf("expected no %s kept as the story's failure", name)
		}
	}
}
