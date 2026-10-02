package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// aBoxWith is a mailbox holding n unread messages for the deputy.
func aBoxWith(t *testing.T, n int) *apptest.FakeMailbox {
	t.Helper()
	box := apptest.NewFakeMailbox()
	for i := 0; i < n; i++ {
		if _, err := box.Send(context.Background(), application.NewMessage{From: "mayor", To: "deputy", Subject: "work"}); err != nil {
			t.Fatal(err)
		}
	}
	return box
}

// aDeputyReaper is an idle-mode reaper over a window whose seat has a handoff
// newer than it, with the Deputy's box wired. Time is its own: each sleep moves
// the clock on a minute and then runs what the test scheduled for that look.
type aDeputyReaper struct {
	reap    application.SeatReap
	windows *apptest.FakeWindows
	files   *fakeSeatFiles
	log     *memoryLog
	now     time.Time
	look    int
	events  map[int]func()
}

func newDeputyReaper(t *testing.T, box application.Mailbox) *aDeputyReaper {
	t.Helper()
	windows, files := aFinishedWindow(t)
	r := &aDeputyReaper{
		windows: windows, files: files, log: &memoryLog{},
		now:    time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
		events: map[int]func(){},
	}
	r.reap = application.SeatReap{
		Seats: files, Terminal: windows, Log: r.log,
		Seat: "mayor", Host: "laptop", Window: "@3", WhenIdle: true,
		Mail: box, Mailbox: application.DeputyMailbox, NudgeFormat: application.DeputyNudgeFormat,
		Interval: time.Minute, Limit: 30 * time.Minute,
		Now: func() time.Time { return r.now },
		Sleep: func(context.Context, time.Duration) error {
			r.look++
			r.now = r.now.Add(time.Minute)
			if event := r.events[r.look]; event != nil {
				event()
			}
			return nil
		},
	}
	return r
}

// handsOffAgain writes a handoff stamped with the clock, as a session that has
// worked what it was nudged about does.
func (r *aDeputyReaper) handsOffAgain() {
	r.files.start.Handoffs = append(r.files.start.Handoffs, application.Handoff{
		Name: "2026-09-19-13", Number: 13, Written: r.now,
	})
}

func TestAnIdleDeputyWithUnreadMailIsNudgedNotClosedAndClosedOnceItsBoxIsEmpty(t *testing.T) {
	box := aBoxWith(t, 2)
	r := newDeputyReaper(t, box)
	r.events[5] = func() {
		r.handsOffAgain()
		for _, m := range mustInbox(t, box) {
			if _, err := box.Read(context.Background(), m.ID, "deputy"); err != nil {
				t.Fatal(err)
			}
		}
	}

	report, err := r.reap.Run(context.Background())
	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the window closed once the box was empty, it ended %q with %v", report.Outcome, err)
	}

	nudge := fmt.Sprintf(application.DeputyNudgeFormat, 2) + "\n"
	if typed := r.windows.Typed("@3"); len(typed) != 1 || typed[0] != nudge {
		t.Errorf("expected the nudge typed once, got %q", typed)
	}
	nudged := 0
	for _, line := range r.log.lines {
		if strings.Contains(line, "nudged: 2 unread mail(s) for deputy; not closing") {
			nudged++
		}
	}
	if nudged != 1 {
		t.Errorf("expected one nudged line in the log, got %q", r.log.lines)
	}
	if r.look < 5 {
		t.Errorf("the window closed on look %d, before the session handed off again", r.look)
	}
}

func TestAnIdleDeputyWhoDoesNotHandOffAgainAfterTheNudgeIsNotClosed(t *testing.T) {
	box := aBoxWith(t, 2)
	r := newDeputyReaper(t, box)
	// The mail is read, but no new handoff follows the nudge.
	r.events[4] = func() {
		for _, m := range mustInbox(t, box) {
			_, _ = box.Read(context.Background(), m.ID, "deputy")
		}
	}

	report, err := r.reap.Run(context.Background())
	if err == nil || report.Outcome != application.ReapGaveUp {
		t.Fatalf("expected the watch to give up, it ended %q with %v", report.Outcome, err)
	}
	if got := r.windows.ClosedIDs(); len(got) != 0 {
		t.Errorf("expected nothing closed, got %v", got)
	}
	if typed := r.windows.Typed("@3"); len(typed) != 1 {
		t.Errorf("expected the nudge typed once, got %q", typed)
	}
}

func TestAMailCountThatFailsClosesNothingAndIsLogged(t *testing.T) {
	box := aBoxWith(t, 0)
	box.Err = errors.New("the database is down")
	r := newDeputyReaper(t, box)

	report, err := r.reap.Run(context.Background())
	if err == nil || report.Outcome != application.ReapGaveUp {
		t.Fatalf("expected the watch to give up, it ended %q with %v", report.Outcome, err)
	}
	if got := r.windows.ClosedIDs(); len(got) != 0 {
		t.Errorf("expected nothing closed, got %v", got)
	}
	if typed := r.windows.Typed("@3"); len(typed) != 0 {
		t.Errorf("expected nothing typed, got %q", typed)
	}
	if !strings.Contains(strings.Join(r.log.lines, "\n"), "could not be counted") {
		t.Errorf("expected the log to say the mail could not be counted, got %q", r.log.lines)
	}
}

func TestAMailCountThatFailsIsTriedAgainAndAnEmptyBoxCloses(t *testing.T) {
	box := aBoxWith(t, 0)
	box.Err = errors.New("the database is down")
	r := newDeputyReaper(t, box)
	r.events[4] = func() { box.Err = nil }

	report, err := r.reap.Run(context.Background())
	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the window closed once the count worked, it ended %q with %v", report.Outcome, err)
	}
}

func TestAnEmptyBoxClosesAsBefore(t *testing.T) {
	r := newDeputyReaper(t, aBoxWith(t, 0))

	report, err := r.reap.Run(context.Background())
	if err != nil || report.Outcome != application.ReapClosed || r.look != 2 {
		t.Fatalf("expected the window closed on look 2, it ended %q on look %d with %v", report.Outcome, r.look, err)
	}
	if typed := r.windows.Typed("@3"); len(typed) != 0 {
		t.Errorf("expected nothing typed, got %q", typed)
	}
}

func TestAReaperWithoutMailIgnoresUnreadMail(t *testing.T) {
	r := newDeputyReaper(t, nil)
	r.reap.Mail = nil

	report, err := r.reap.Run(context.Background())
	if err != nil || report.Outcome != application.ReapClosed || r.look != 2 {
		t.Fatalf("expected the window closed on look 2, it ended %q on look %d with %v", report.Outcome, r.look, err)
	}
	if typed := r.windows.Typed("@3"); len(typed) != 0 {
		t.Errorf("expected nothing typed, got %q", typed)
	}
}

func mustInbox(t *testing.T, box application.Mailbox) []application.Message {
	t.Helper()
	inbox, err := box.Inbox(context.Background(), application.DeputyMailbox)
	if err != nil {
		t.Fatal(err)
	}
	return inbox
}
