package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.640: a pre-dispatch re-run that could not be made was said only on the
// story's started line, which goes to the host's dispatch tick log and nowhere a
// seat reads. It is kept in a log of its own, shown by mw status and mailed.

var skipNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func skipLine(at time.Time, story, said string) string {
	return at.Format(time.RFC3339) + " " + story + " " + said
}

func TestADispatchKeepsEveryRerunItCouldNotMakeInTheRecheckLog(t *testing.T) {
	for name, rechecks := range map[string]*stubRechecks{
		"the run failed": {err: errors.New("`npm test` could not be run in /tmp/recheck-1: sh: playwright: not found\nmore")},
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
			dispatch.Rechecks = rechecks
			log := &apptest.FakeTickLog{}
			dispatch.RecheckSkips = log
			dispatch.Now = func() time.Time { return skipNow }

			if _, err := dispatch.Run(context.Background()); err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			lines := log.Lines()
			want := skipLine(skipNow, "mw-gq6.1", "not re-run on main first: `npm test` could not be run in /tmp/recheck-1: sh: playwright: not found")
			if len(lines) != 1 || lines[0] != want {
				t.Errorf("recheck log:\n got %q\nwant [%q]", lines, want)
			}
		})
	}
}

func TestADispatchKeepsABusyGateInTheRecheckLog(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	dispatch.Rechecks = &stubRechecks{}
	dispatch.GateLock = aBusyGateLock{by: "mw-x.9"}
	dispatch.GateWait = time.Millisecond
	log := &apptest.FakeTickLog{}
	dispatch.RecheckSkips = log
	dispatch.Now = func() time.Time { return skipNow }

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	lines := log.Lines()
	if len(lines) != 1 || !strings.Contains(lines[0], "mw-gq6.1 not re-checked: the gate is busy") {
		t.Errorf("recheck log = %q, want the busy gate named with the story", lines)
	}
}

func TestADispatchKeepsNothingInTheRecheckLogForARerunThatRan(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	dispatch.Rechecks = &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "9f3c2ab", Passed: false, Tail: "1 failed"}}
	log := &apptest.FakeTickLog{}
	dispatch.RecheckSkips = log

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if lines := log.Lines(); len(lines) != 0 {
		t.Errorf("a spec that still fails ran: the recheck log should hold nothing, got %q", lines)
	}
}

func skipStatus(t *testing.T, log application.TickLog) string {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	status := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		Now:   func() time.Time { return skipNow },
		Ticks: application.TickLogs{RecheckSkips: log},
	}
	report, err := status.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report.String()
}

func TestMwStatusNamesEachRerunNotMadeInTheLastDay(t *testing.T) {
	log := heldLog(t,
		skipLine(skipNow.Add(-30*time.Hour), "mw-gq6.9", "not re-run on main first: too old"),
		skipLine(skipNow.Add(-5*time.Hour), "mw-gq6.414", "not re-run on main first: `npm test` could not be run"),
		skipLine(skipNow.Add(-time.Hour), "mw-gq6.415", "not re-checked: the gate is busy"),
		"not a dated line",
	)
	printed := skipStatus(t, log)
	for _, want := range []string{application.RecheckSkipsHeading + " (2)", "mw-gq6.414", "npm test", "mw-gq6.415", "the gate is busy"} {
		if !strings.Contains(printed, want) {
			t.Errorf("mw status should say %q, got:\n%s", want, printed)
		}
	}
	if strings.Contains(printed, "mw-gq6.9") {
		t.Errorf("a re-run skipped over a day ago should not be shown, got:\n%s", printed)
	}
}

func TestMwStatusSaysNothingWhenNoRerunWasSkipped(t *testing.T) {
	for name, log := range map[string]application.TickLog{
		"no log":    nil,
		"empty log": &apptest.FakeTickLog{},
		"only old":  heldLog(t, skipLine(skipNow.Add(-48*time.Hour), "mw-gq6.9", "not re-run on main first: old")),
	} {
		if printed := skipStatus(t, log); strings.Contains(printed, application.RecheckSkipsHeading) {
			t.Errorf("%s: mw status should have no %s section, got:\n%s", name, application.RecheckSkipsHeading, printed)
		}
	}
}

func aRecheckSkipCheck(log application.TickLog, mailbox *apptest.FakeMailbox, now *time.Time) *application.RecheckSkipCheck {
	return &application.RecheckSkipCheck{
		Host: "vps", Log: log, Now: func() time.Time { return *now },
		Mail: func(ctx context.Context, m application.NewMessage) error { _, err := mailbox.Send(ctx, m); return err },
	}
}

func TestMwDoctorMailsTheMayorTheSkippedRerunsOnceADay(t *testing.T) {
	ctx := context.Background()
	log := heldLog(t, skipLine(skipNow.Add(-time.Hour), "mw-gq6.414", "not re-run on main first: `npm test` could not be run"))
	mailbox := apptest.NewFakeMailbox()
	state, dlog := apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}
	now := skipNow
	doctor := aDoctor(state, dlog, &now, aRecheckSkipCheck(log, mailbox, &now))

	if _, err := doctor.Run(ctx, "", false); application.ExitStatus(err) != 0 {
		t.Fatalf("the first run should cure by mailing, got %v", err)
	}
	inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, "mw-gq6.414") || !strings.Contains(inbox[0].Body, "npm test") {
		t.Fatalf("expected one mail naming the story and the error, got %+v", inbox)
	}

	// A tick of the doctor every five minutes for the rest of the day: no more mail.
	for range 100 {
		now = now.Add(5 * time.Minute)
		if _, err := doctor.Run(ctx, "", false); err != nil {
			t.Fatalf("a run inside the day's wait should be quiet, got %v", err)
		}
	}
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 1 {
		t.Errorf("the Mayor should be mailed once a day at most, got %d mails", len(inbox))
	}

	// A day on, with a skipped re-run still in the last 24 hours: the next mail.
	_ = log.Append(ctx, skipLine(now, "mw-gq6.416", "not re-checked: the gate is busy"))
	now = skipNow.Add(25 * time.Hour)
	if _, err := doctor.Run(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 2 {
		t.Errorf("a day later a still-skipped re-run should be mailed again, got %d mails", len(inbox))
	}
}

func TestMwDoctorFindsTheRecheckLogOkWhenNothingWasSkipped(t *testing.T) {
	mailbox := apptest.NewFakeMailbox()
	now := skipNow
	verdict, _ := aRecheckSkipCheck(&apptest.FakeTickLog{}, mailbox, &now).Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Errorf("verdict = %v, want ok", verdict)
	}
}
