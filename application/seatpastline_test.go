package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// pastLineEvents is the seat_past_line events the log holds.
func pastLineEvents(t *testing.T, log application.EventLog) []events.SeatPastLine {
	t.Helper()
	all, err := log.Since(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []events.SeatPastLine
	for _, ev := range all {
		if ev.Kind != events.KindSeatPastLine {
			continue
		}
		p, ok := events.SeatPastLineOf(ev)
		if !ok {
			t.Fatalf("a seat_past_line event with an unreadable detail %q", ev.Detail)
		}
		out = append(out, p)
	}
	return out
}

// sprung is what the watch asked to have brought up.
type sprung struct{ seat, reason string }

// aWatchWithMail is the context world with the Deputy's box, an event log and a
// spring wired.
func aWatchWithMail(t *testing.T, unread int) (*contextWorld, *apptest.FakeEventLog, *[]sprung) {
	t.Helper()
	w := newContextWorld(deputyLimits)
	log := &apptest.FakeEventLog{}
	var springs []sprung
	w.watch.Mail = aBoxWith(t, unread)
	w.watch.Events = log
	w.watch.Host = "laptop"
	w.watch.Springs = []string{"deputy", "millhand"}
	w.watch.Spring = func(_ context.Context, seat, reason string) error {
		springs = append(springs, sprung{seat, reason})
		return nil
	}
	return w, log, &springs
}

// Seen 2026-11-04: Deputy 910 sat at its 'write your handoff now and stop' line
// with five jobs unread. The watch told it once, and nothing closed its window
// or sprang a successor (mw-gq6.975).
func TestASessionToldToHandOffThatStopsPastItsLineWithMailUnreadIsClosedAndASuccessorSprung(t *testing.T) {
	w, log, springs := aWatchWithMail(t, 5)
	w.seatUp("@7", "deputy-2026-11-04-02", 231000)

	w.tick(t) // told
	if got := w.windows.Typed("@7"); len(got) != 1 {
		t.Fatalf("expected the line typed once, got %q", got)
	}
	if !w.windows.IsOpen("@7") {
		t.Fatal("the window was closed on the pass that told it")
	}

	w.tick(t) // told, idle, past, mail unread
	if w.windows.IsOpen("@7") {
		t.Errorf("the window of a session stopped past its line with 5 unread mails was left open")
	}
	if len(*springs) != 1 || (*springs)[0].seat != "deputy" || !strings.Contains((*springs)[0].reason, "5 unread") {
		t.Errorf("expected a successor sprung for deputy naming the 5 unread mails, got %+v", *springs)
	}
	got := pastLineEvents(t, log)
	want := events.SeatPastLine{Seat: "deputy", Window: "deputy-2026-11-04-02", Tokens: 231000, HandoffAt: 200000, Unread: 5}
	if len(got) != 1 || got[0] != want {
		t.Errorf("expected the event %+v, got %+v", want, got)
	}
	if typed := w.windows.Typed("@7"); len(typed) != 1 {
		t.Errorf("a seat past its line was typed to again: %q", typed)
	}
	if len(w.marks.marks) != 0 {
		t.Errorf("the closed window kept its mark: %v", w.marks.marks)
	}
}

func TestAPastLineSessionAtWorkOrWithNoMailUnreadOrNotSpringableIsLeftOpen(t *testing.T) {
	t.Run("at work", func(t *testing.T) {
		w, log, springs := aWatchWithMail(t, 2)
		w.seatUp("@7", "deputy-2026-11-04-02", 231000)
		w.tick(t)
		if err := w.windows.Pane("@7", application.PaneWorking); err != nil {
			t.Fatal(err)
		}
		w.tick(t)
		if !w.windows.IsOpen("@7") || len(*springs) != 0 || len(pastLineEvents(t, log)) != 0 {
			t.Errorf("a session writing its handoff was retired: open=%v springs=%v", w.windows.IsOpen("@7"), *springs)
		}
	})
	t.Run("no unread mail", func(t *testing.T) {
		w, log, springs := aWatchWithMail(t, 0)
		w.seatUp("@7", "deputy-2026-11-04-02", 231000)
		w.tick(t)
		w.tick(t)
		if !w.windows.IsOpen("@7") || len(*springs) != 0 || len(pastLineEvents(t, log)) != 0 {
			t.Errorf("a session with nothing waiting was retired: open=%v springs=%v", w.windows.IsOpen("@7"), *springs)
		}
	})
	t.Run("a seat the spring cannot bring up", func(t *testing.T) {
		w, _, springs := aWatchWithMail(t, 2)
		w.watch.Springs = []string{"millhand"}
		w.seatUp("@7", "deputy-2026-11-04-02", 231000)
		w.tick(t)
		w.tick(t)
		if !w.windows.IsOpen("@7") || len(*springs) != 0 {
			t.Errorf("a seat that cannot be sprung was closed: open=%v", w.windows.IsOpen("@7"))
		}
	})
	t.Run("a session under its line", func(t *testing.T) {
		w, _, springs := aWatchWithMail(t, 2)
		w.seatUp("@7", "deputy-2026-11-04-02", 150000)
		w.tick(t)
		w.tick(t)
		if !w.windows.IsOpen("@7") || len(*springs) != 0 {
			t.Errorf("a session under its line was closed")
		}
	})
}

// A mailbox the watch could not count is not a reason to close.
func TestAMailCountThatFailsLeavesAPastLineWindowOpen(t *testing.T) {
	w, _, springs := aWatchWithMail(t, 2)
	w.watch.Mail.(*apptest.FakeMailbox).Err = context.DeadlineExceeded
	w.seatUp("@7", "deputy-2026-11-04-02", 231000)
	w.tick(t)
	w.tick(t)
	if !w.windows.IsOpen("@7") || len(*springs) != 0 {
		t.Errorf("a window was closed over a mail count that failed")
	}
}

// mw deputy found the Deputy up, past its line, idle, with mail waiting: the
// nudge would go to a seat that has refused to work.
func TestMwDeputyClosesADeputyStoppedPastItsLineWithMailUnreadAndStartsAFreshOne(t *testing.T) {
	windows := apptest.NewFakeWindows()
	windows.HoldsWithID("@7", "deputy-2026-11-04-02", time.Date(2026, 11, 4, 20, 0, 0, 0, time.UTC))
	tr := &titledTranscripts{tokens: map[string]int{"deputy-2026-11-04-02": 231000}}
	log := &apptest.FakeEventLog{}
	files := &fakeSeatFiles{start: aSeatStart(nil)}
	var out strings.Builder

	report, err := application.Deputy{
		Seats: files, Windows: windows, Harness: &fakeSeatHarness{},
		Terminal: windows, Armer: &apptest.FakeReapArmer{},
		Mail: aBoxWith(t, 5), Events: log,
		Line: application.SeatLine{Transcripts: tr, Dir: "/vault", HandoffAt: 200000},
		Host: "laptop",
		Now:  func() time.Time { return time.Date(2026, 11, 4, 21, 0, 0, 0, time.UTC) },
		Out:  &out,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("mw deputy: %v", err)
	}
	if windows.IsOpen("@7") {
		t.Error("the window of the Deputy stopped past its line was left open")
	}
	if report.Window == "" || report.Window == "deputy-2026-11-04-02" {
		t.Errorf("expected a fresh Deputy window, got %q", report.Window)
	}
	if typed := windows.Typed("@7"); len(typed) != 0 {
		t.Errorf("a seat past its line was nudged: %q", typed)
	}
	got := pastLineEvents(t, log)
	want := events.SeatPastLine{Seat: "deputy", Window: "deputy-2026-11-04-02", Tokens: 231000, HandoffAt: 200000, Unread: 5}
	if len(got) != 1 || got[0] != want {
		t.Errorf("expected the event %+v, got %+v", want, got)
	}
}

func TestMwDeputyStillNudgesADeputyUnderItsLine(t *testing.T) {
	windows := apptest.NewFakeWindows()
	windows.HoldsWithID("@7", "deputy-2026-11-04-02", time.Date(2026, 11, 4, 20, 0, 0, 0, time.UTC))
	tr := &titledTranscripts{tokens: map[string]int{"deputy-2026-11-04-02": 120000}}
	_, err := application.Deputy{
		Seats: &fakeSeatFiles{start: aSeatStart(nil)}, Windows: windows, Harness: &fakeSeatHarness{},
		Terminal: windows, Armer: &apptest.FakeReapArmer{},
		Mail: aBoxWith(t, 2), Events: &apptest.FakeEventLog{},
		Line: application.SeatLine{Transcripts: tr, Dir: "/vault", HandoffAt: 200000},
		Host: "laptop",
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("mw deputy: %v", err)
	}
	if !windows.IsOpen("@7") || len(windows.Typed("@7")) != 1 {
		t.Errorf("expected the nudge typed into the open window, typed %q", windows.Typed("@7"))
	}
}

// The reaper does not type the mail nudge into a session past its line.
func TestTheReaperDoesNotNudgeASessionPastItsLine(t *testing.T) {
	box := aBoxWith(t, 2)
	r := newDeputyReaper(t, box)
	r.reap.Line = application.SeatLine{
		Transcripts: &titledTranscripts{tokens: map[string]int{"mayor-2026-09-19-12": 231000}},
		Dir:         "/vault", HandoffAt: 200000,
	}

	report, err := r.reap.Run(context.Background())
	if err == nil || report.Outcome != application.ReapGaveUp {
		t.Fatalf("expected the watch to give up with the window left, it ended %q with %v", report.Outcome, err)
	}
	if typed := r.windows.Typed("@3"); len(typed) != 0 {
		t.Errorf("a session past its line was nudged: %q", typed)
	}
	said := 0
	for _, line := range r.log.lines {
		if strings.Contains(line, "not nudging: the session is at context 231000, past its line of 200000, with 2 unread mail(s)") {
			said++
		}
	}
	if said != 1 {
		t.Errorf("expected one line saying it did not nudge, got %q", r.log.lines)
	}
	if got := r.windows.ClosedIDs(); len(got) != 0 {
		t.Errorf("expected nothing closed over unread mail, got %v", got)
	}
}

func TestASeatPastLineDetailReadsBack(t *testing.T) {
	p := events.SeatPastLine{Seat: "millhand", Window: "millhand-2026-11-04-01", Tokens: 231000, HandoffAt: 200000, Unread: 3}
	back, ok := events.ParseSeatPastLine(p.Detail())
	if !ok || back != p {
		t.Fatalf("expected %+v back from %q, got %+v (%v)", p, p.Detail(), back, ok)
	}
	for _, bad := range []string{"", "deputy", "deputy in w: context x past 2, 1 unread"} {
		if _, ok := events.ParseSeatPastLine(bad); ok {
			t.Errorf("expected %q to be no seat_past_line detail", bad)
		}
	}
}
