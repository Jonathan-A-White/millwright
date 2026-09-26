package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// stubSeatFiles is the smallest application.SeatFiles that lets a wake
// actually start: one charter and one handoff, and nothing a test needs to
// change host to host.
type stubSeatFiles struct{}

func (stubSeatFiles) SeatStart(context.Context, string, string) (application.SeatStart, error) {
	return application.SeatStart{
		Charter:  "charter.md",
		Handoffs: []application.Handoff{{Name: "2026-09-24-01", Path: "handoffs/2026-09-24-01.md", Written: level}},
	}, nil
}

func (stubSeatFiles) HostFile(context.Context, string, string, string) (string, error) {
	return "", nil
}

// stubSeatHarness turns a launch into a window spec that runs nothing.
type stubSeatHarness struct{}

func (stubSeatHarness) SeatSession(l application.SeatLaunch) (application.WindowSpec, error) {
	if err := l.Validate(); err != nil {
		return application.WindowSpec{}, err
	}
	return application.WindowSpec{Name: l.Name, Dir: l.Dir, Command: []string{"/bin/true"}}, nil
}

// resumeTick is a tick able to wake for real — a full SeatUp pipeline stood
// in for — with a log, a doctor notes store, a watch and a Reach a test can
// each shape on its own; whichever the test leaves nil or unset answers as
// if it were not wired at all.
type resumeTick struct {
	log     *apptest.FakeTickLog
	notes   *apptest.FakeTracker
	mailbox *apptest.FakeMailbox
	watch   *apptest.FakeWatch
	reach   *apptest.FakeReach
	now     time.Time

	// watching is the [watch] table wired into the tick, empty until a test
	// asks for one with watchSaysUnwell.
	watching application.Watch
}

func newResumeTick() *resumeTick {
	return &resumeTick{
		log:     &apptest.FakeTickLog{},
		notes:   apptest.NewFakeTracker(),
		mailbox: apptest.NewFakeMailbox(),
		watch:   apptest.NewFakeWatch(),
		reach:   apptest.NewFakeReach(),
		now:     level,
	}
}

// seedLogLine puts one line in the tick's log, ago before the tick's current
// clock, as if an earlier tick had written it.
func (r *resumeTick) seedLogLine(ago time.Duration, words string) {
	at := r.now.Add(-ago)
	_ = r.log.Append(context.Background(), at.UTC().Format(time.RFC3339)+" "+words)
}

// watchSaysUnwell wires a [watch] table whose outside place answers and whose
// health line, fresh, ends unwell: a health verdict that would wake.
func (r *resumeTick) watchSaysUnwell() {
	r.watch.Answering["https://one.example"] = true
	r.watch.Health = r.now.Add(-5*time.Minute).UTC().Format(time.RFC3339) + " load1=0.50 mem_avail_mb=1000 disk_pct=42 services=none verdict=unwell:load1"
	r.watching = application.Watch{
		Probes:   r.watch,
		Notes:    r.notes,
		Settings: application.WatchSettings{SSH: "vps-ssh", Host: "vps", Outside: []string{"https://one.example"}},
		Now:      func() time.Time { return r.now },
	}
}

func (r *resumeTick) doctorNote(check, text string) {
	_ = r.notes.SetNote(context.Background(), application.DoctorNoteKey("laptop", check), text)
}

func (r *resumeTick) mail(box, subject string) {
	_, _ = r.mailbox.Send(context.Background(), application.NewMessage{From: "mayor", To: box, Subject: subject, Body: "test"})
}

func (r *resumeTick) run(t *testing.T) application.MillhandTickReport {
	t.Helper()
	now := func() time.Time { return r.now }
	windows := apptest.NewFakeWindows()
	tick := application.MillhandTick{
		Millhand: application.Millhand{
			Seats: stubSeatFiles{}, Windows: windows, Harness: stubSeatHarness{},
			Terminal: windows, Armer: &apptest.FakeReapArmer{},
			Host: "laptop", RoutineModel: "sonnet", ReviewModel: "sonnet", Now: now,
		},
		Sync:        &stubSync{},
		Mail:        r.mailbox,
		DoctorNotes: r.notes,
		Sweep:       application.Sweep{Tracker: apptest.NewFakeTracker(), Host: "laptop", Now: now},
		Reach:       r.reach,
		Watch:       r.watching,
		Log:         r.log,
		Host:        "laptop",
		Now:         now,
	}
	report, err := tick.Run(context.Background())
	if err != nil {
		t.Fatalf("running the tick: %v", err)
	}
	return report
}

// TestAResumeDefersAPendingDoctorNoteAndAHealthVerdictThatWouldWake is
// acceptance (a): a gap since the log's newest line bigger than ResumeGap
// announces a resume and, with a doctor note pending and a watch that would
// otherwise wake, wakes nobody.
func TestAResumeDefersAPendingDoctorNoteAndAHealthVerdictThatWouldWake(t *testing.T) {
	r := newResumeTick()
	r.seedLogLine(45*time.Minute, "quiet")
	r.doctorNote("wifi", "2026-09-24T10:00:00Z faulty internet unreachable since 2026-09-24T09:55:00Z")
	r.watchSaysUnwell()

	report := r.run(t)
	t.Logf("case (a) tick log line: %s", report.Line)
	if report.Woke {
		t.Fatalf("expected a resuming tick to wake nobody, it woke: %s", report.Line)
	}
	if !strings.Contains(report.Line, "resumed after 45m0s") {
		t.Fatalf("expected the line to announce the resume, got %q", report.Line)
	}
	if !strings.Contains(report.Line, "resuming: 1 doctor note and health deferred") {
		t.Fatalf("expected the line to say the deferral, got %q", report.Line)
	}
	if strings.Contains(report.Line, "doctor: wifi") {
		t.Fatalf("expected the doctor note's own reason not to appear, got %q", report.Line)
	}

	// The note must still be unseen: a later tick, past the grace, must find
	// it fresh.
	if seen, _ := r.notes.Note(context.Background(), application.DoctorSeenKey("laptop", "wifi")); seen != "" {
		t.Fatalf("expected the doctor note not to be marked seen during the grace, it holds %q", seen)
	}
}

// TestAResumeStillWakesForUnreadMail is acceptance (b): the same resuming
// tick, with unread mail for the Millhand, wakes for the mail regardless.
func TestAResumeStillWakesForUnreadMail(t *testing.T) {
	r := newResumeTick()
	r.seedLogLine(45*time.Minute, "quiet")
	r.doctorNote("wifi", "2026-09-24T10:00:00Z faulty internet unreachable since 2026-09-24T09:55:00Z")
	r.mail("millhand@laptop", "Please look at the queue")

	report := r.run(t)
	t.Logf("case (b) tick log line: %s", report.Line)
	if !report.Woke {
		t.Fatalf("expected the mail to wake the Millhand, it did not: %s", report.Line)
	}
	if !strings.Contains(report.Line, "Please look at the queue") {
		t.Fatalf("expected the reason to name the mail, got %q", report.Line)
	}
	if !strings.Contains(report.Line, "resumed after 45m0s") {
		t.Fatalf("expected the line to still announce the resume, got %q", report.Line)
	}
}

// TestAResumeGraceEndsAndTheDeferredNoteWakesOnce is acceptance (c): a second
// tick, run GraceAfterResume+1s after the first, wakes for the doctor note
// the first tick deferred rather than lost.
func TestAResumeGraceEndsAndTheDeferredNoteWakesOnce(t *testing.T) {
	r := newResumeTick()
	r.seedLogLine(45*time.Minute, "quiet")
	r.doctorNote("wifi", "2026-09-24T10:00:00Z faulty internet unreachable since 2026-09-24T09:55:00Z")

	first := r.run(t)
	t.Logf("case (c) first tick log line: %s", first.Line)
	if first.Woke {
		t.Fatalf("expected the first, resuming tick to wake nobody, it woke: %s", first.Line)
	}

	r.now = r.now.Add(application.GraceAfterResume + time.Second)
	second := r.run(t)
	t.Logf("case (c) second tick log line: %s", second.Line)
	if !second.Woke {
		t.Fatalf("expected the second tick, past the grace, to wake for the still-unseen note, it did not: %s", second.Line)
	}
	if !strings.Contains(second.Line, "doctor: wifi") {
		t.Fatalf("expected the note to be the reason, got %q", second.Line)
	}
	if strings.Contains(second.Line, "resumed after") {
		t.Fatalf("expected the second tick not to announce a fresh resume, got %q", second.Line)
	}
}

// TestALocalNetworkFaultDefersTheSameWay is acceptance (d): Reach failing for
// every target, with no resume at all, defers a pending doctor note the same
// way a resume does.
func TestALocalNetworkFaultDefersTheSameWay(t *testing.T) {
	r := newResumeTick()
	r.reach.SetReachable(false)
	r.doctorNote("wifi", "2026-09-24T10:00:00Z faulty internet unreachable since 2026-09-24T09:55:00Z")

	report := r.run(t)
	t.Logf("case (d) tick log line: %s", report.Line)
	if report.Woke {
		t.Fatalf("expected a local network fault to wake nobody, it woke: %s", report.Line)
	}
	if !strings.Contains(report.Line, "local network fault") {
		t.Fatalf("expected the line to say the fault, got %q", report.Line)
	}
	if !strings.Contains(report.Line, "resuming: 1 doctor note and health deferred") {
		t.Fatalf("expected the line to say the deferral, got %q", report.Line)
	}
	if strings.Contains(report.Line, "resumed after") {
		t.Fatalf("expected no resume announcement from a log with nothing in it, got %q", report.Line)
	}
}

// TestAnOrdinaryTickDefersNothing is acceptance (e): a gap under ResumeGap
// with Reach ok wakes for a doctor note exactly as it always has.
func TestAnOrdinaryTickDefersNothing(t *testing.T) {
	r := newResumeTick()
	r.seedLogLine(time.Minute, "quiet")
	r.doctorNote("wifi", "2026-09-24T10:00:00Z faulty internet unreachable since 2026-09-24T09:55:00Z")

	report := r.run(t)
	if !report.Woke {
		t.Fatalf("expected an ordinary tick to wake for the doctor note, it did not: %s", report.Line)
	}
	if !strings.Contains(report.Line, "doctor: wifi") {
		t.Fatalf("expected the note to be the reason, got %q", report.Line)
	}
	if strings.Contains(report.Line, "resumed after") || strings.Contains(report.Line, "resuming:") {
		t.Fatalf("expected no grace at all, got %q", report.Line)
	}
}
