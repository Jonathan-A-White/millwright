package application_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

type nudgeWorld struct {
	log      *apptest.FakeEventLog
	subs     *apptest.FakeSubscribeFiles
	cursors  *apptest.FakeNudgeCursors
	windows  *apptest.FakeWindows
	err      bytes.Buffer
	sprung   []string
	springBy func(seat, reason string) error
	nudge    *application.EventNudge
}

func newNudgeWorld(t *testing.T) *nudgeWorld {
	t.Helper()
	w := &nudgeWorld{log: &apptest.FakeEventLog{}, subs: &apptest.FakeSubscribeFiles{}, cursors: &apptest.FakeNudgeCursors{}, windows: apptest.NewFakeWindows()}
	w.nudge = &application.EventNudge{
		Log: w.log, Subs: w.subs, Cursors: w.cursors, Terminal: w.windows, Err: &w.err,
		Spring: func(_ context.Context, seat, reason string) error {
			w.sprung = append(w.sprung, seat+": "+reason)
			if w.springBy != nil {
				return w.springBy(seat, reason)
			}
			return nil
		},
	}
	return w
}

// pass runs one Nudge.
func (w *nudgeWorld) pass(t *testing.T) {
	t.Helper()
	if err := w.nudge.Nudge(context.Background()); err != nil {
		t.Fatalf("nudge: %v", err)
	}
}

// One mail is one notice (mw-gq6.457): events that are all mail are told with
// the mail line alone, never the events line and the mail line one after the
// other.
func TestNudgeTypesTheMailLineAloneIntoAnIdlePaneWhenEveryEventIsMail(t *testing.T) {
	w := newNudgeWorld(t)
	w.subs.Set("deputy", `kinds = ["mail", "message"]`)
	w.windows.HoldsWithID("@7", "deputy-2026-10-01-01", time.Time{})
	w.pass(t) // the seat starts at the head: nothing is told
	appendAll(t, w.log, jobEvent, events.Event{Kind: events.KindMail, Bead: "mw-m1", Detail: "deputy"}, events.Event{Kind: events.KindMail, Bead: "mw-m2", Detail: "deputy"})
	w.pass(t)
	want := []string{"New mail for deputy: 2 message(s): mw-m1, mw-m2. Run bd mail inbox.\n"}
	if got := w.windows.Typed("@7"); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("typed %q, want %q", got, want)
	}
	w.pass(t) // told once
	if got := w.windows.Typed("@7"); len(got) != 1 {
		t.Fatalf("a second pass typed again: %q", got)
	}
}

// Mail among other events is told by the events line alone: mw events tail
// names the mail with the rest.
func TestNudgeTypesTheEventsLineAloneWhenMailIsAmongOtherEvents(t *testing.T) {
	w := newNudgeWorld(t)
	w.subs.Set("deputy", `kinds = ["mail", "message"]`)
	w.windows.HoldsWithID("@7", "deputy-2026-10-01-01", time.Time{})
	w.pass(t)
	appendAll(t, w.log, events.Event{Kind: events.KindMail, Bead: "mw-m1", Detail: "deputy"}, msgEvent)
	w.pass(t)
	want := []string{"New events for deputy: 2. Run mw events tail --since 0.\n"}
	if got := w.windows.Typed("@7"); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("typed %q, want %q", got, want)
	}
}

func TestNudgeNamesOnlyTheEventsFromBeforeTheFirstMatch(t *testing.T) {
	w := newNudgeWorld(t)
	w.subs.Set("mayor", `kinds = ["message"]`)
	w.windows.HoldsWithID("@1", "mayor-2026-10-01-02", time.Time{})
	w.pass(t)
	appendAll(t, w.log, jobEvent, msgEvent)
	w.pass(t)
	if got := w.windows.Typed("@1"); len(got) != 1 || got[0] != "New events for mayor: 1. Run mw events tail --since 1.\n" {
		t.Fatalf("typed %q (no mail among them, so no mail line)", got)
	}
}

func TestNudgeLeavesABusyPaneAndTellsItOnceIdle(t *testing.T) {
	for _, state := range []application.PaneState{application.PaneWorking, application.PaneInput, application.PaneFirstRun} {
		w := newNudgeWorld(t)
		w.subs.Set("deputy", `kinds = ["mail"]`)
		w.windows.HoldsWithID("@7", "deputy-2026-10-01-01", time.Time{})
		w.pass(t)
		appendAll(t, w.log, events.Event{Kind: events.KindMail, Bead: "mw-m1", Detail: "deputy"})
		_ = w.windows.Pane("@7", state)
		w.pass(t)
		w.pass(t)
		if got := w.windows.Typed("@7"); len(got) != 0 {
			t.Fatalf("pane %s: typed into a pane that is not idle: %q", state, got)
		}
		_ = w.windows.Pane("@7", application.PaneIdle)
		w.pass(t)
		if got := w.windows.Typed("@7"); len(got) != 1 || got[0] != "New mail for deputy: 1 message(s): mw-m1. Run bd mail inbox.\n" {
			t.Fatalf("pane %s: once idle it was typed %q", state, got)
		}
		if len(w.sprung) != 0 {
			t.Fatalf("a seat with a window was sprung: %v", w.sprung)
		}
	}
}

func TestNudgeSpringsADownSeatMarkedSpring(t *testing.T) {
	w := newNudgeWorld(t)
	w.subs.Set("deputy", "kinds = [\"mail\"]\nspring = true\n")
	w.subs.Set("millhand", "kinds = [\"mail\"]\n") // down, but not marked
	w.pass(t)
	appendAll(t, w.log, events.Event{Kind: events.KindMail, Bead: "mw-m1", Detail: "deputy"}, events.Event{Kind: events.KindMail, Bead: "mw-m2", Detail: "millhand"})
	w.pass(t)
	if len(w.sprung) != 1 || w.sprung[0] != "deputy: New events for deputy: 1. Run mw events tail --since 0." {
		t.Fatalf("sprung %q, want the Deputy alone", w.sprung)
	}
	w.pass(t)
	if len(w.sprung) != 1 {
		t.Fatalf("a second pass sprung again: %q", w.sprung)
	}
}

func TestNudgeRetriesAFailedSpringAndSaysSoOnce(t *testing.T) {
	w := newNudgeWorld(t)
	w.subs.Set("deputy", "kinds = [\"mail\"]\nspring = true\n")
	w.pass(t)
	appendAll(t, w.log, events.Event{Kind: events.KindMail, Bead: "mw-m1", Detail: "deputy"})
	w.springBy = func(string, string) error { return errors.New("no tmux") }
	w.pass(t)
	w.pass(t)
	if len(w.sprung) != 2 || strings.Count(w.err.String(), "no tmux") != 1 {
		t.Fatalf("sprung %d times, said %q; want a retry each pass and one line said", len(w.sprung), w.err.String())
	}
	w.springBy = nil
	w.pass(t)
	w.pass(t)
	if len(w.sprung) != 3 {
		t.Fatalf("sprung %d times, want it to stop once it worked", len(w.sprung))
	}
}

func TestNudgeLeavesASeatNotMarkedSpringWhenItsWindowIsDown(t *testing.T) {
	w := newNudgeWorld(t)
	w.subs.Set("millhand", `kinds = ["mail"]`)
	w.pass(t)
	appendAll(t, w.log, events.Event{Kind: events.KindMail, Bead: "mw-m1", Detail: "millhand"})
	w.pass(t)
	if len(w.sprung) != 0 {
		t.Fatalf("sprung %q", w.sprung)
	}
}

func TestNudgeSaysABadSubscribeFileAndTellsTheOthers(t *testing.T) {
	w := newNudgeWorld(t)
	w.subs.Set("deputy", `kinds = ["mail"]`)
	w.subs.Set("mayor", `kinds = ["smoke-signal"]`)
	w.windows.HoldsWithID("@7", "deputy-2026-10-01-01", time.Time{})
	w.windows.HoldsWithID("@8", "mayor-2026-10-01-01", time.Time{})
	w.pass(t)
	appendAll(t, w.log, events.Event{Kind: events.KindMail, Bead: "mw-m1", Detail: "deputy"})
	w.pass(t)
	if len(w.windows.Typed("@7")) != 1 || len(w.windows.Typed("@8")) != 0 {
		t.Fatalf("typed %q / %q", w.windows.Typed("@7"), w.windows.Typed("@8"))
	}
	if !strings.Contains(w.err.String(), "smoke-signal") || !strings.Contains(w.err.String(), "card_answered") {
		t.Fatalf("said %q, want the bad kind and the kinds", w.err.String())
	}
}

func TestNudgeSubmitsWithASecondEnterWhenTheFirstWasLostAndSaysSo(t *testing.T) {
	w := newNudgeWorld(t)
	w.subs.Set("mayor", `kinds = ["message"]`)
	w.windows.HoldsWithID("@1", "mayor-2026-10-01-02", time.Time{})
	w.pass(t)
	w.windows.LoseEnters("@1", 1)
	appendAll(t, w.log, jobEvent, msgEvent)
	w.pass(t)
	if got := w.windows.Enters("@1"); got != 2 {
		t.Fatalf("Enter pressed %d times, want 2 (the first was lost)", got)
	}
	if line := w.windows.InputLineOf("@1"); line != "" {
		t.Fatalf("the nudge is still on the input line: %q", line)
	}
	if got := w.windows.Typed("@1"); len(got) != 1 {
		t.Fatalf("typed %q, want the nudge once", got)
	}
	if log := w.err.String(); strings.Count(log, "\n") != 1 || !strings.Contains(log, "again") || !strings.Contains(log, "mayor") {
		t.Fatalf("the log should say once that Enter was pressed again for mayor, got %q", log)
	}
}

func TestNudgeAPaneThatNeverTakesEnterIsRetriedOnceSaidOnceAndNotTypedIntoAgain(t *testing.T) {
	w := newNudgeWorld(t)
	w.subs.Set("mayor", `kinds = ["message"]`)
	w.windows.HoldsWithID("@1", "mayor-2026-10-01-02", time.Time{})
	w.pass(t)
	w.windows.LoseEnters("@1", -1)
	appendAll(t, w.log, jobEvent, msgEvent)
	w.pass(t)
	if got := w.windows.Enters("@1"); got != 2 {
		t.Fatalf("Enter pressed %d times, want 2 (one retry)", got)
	}
	log := w.err.String()
	if strings.Count(log, "\n") != 1 || !strings.Contains(log, "still") {
		t.Fatalf("the log should say once that the nudge is still on the line, got %q", log)
	}
	// More events while the line still holds the first nudge: nothing typed.
	appendAll(t, w.log, msgEvent)
	w.pass(t)
	w.pass(t)
	if got := w.windows.Typed("@1"); len(got) != 1 {
		t.Fatalf("a second copy of the nudge was typed: %q", got)
	}
	if got := w.windows.Enters("@1"); got != 2 {
		t.Fatalf("Enter pressed %d times, want still 2", got)
	}
	if got := strings.Count(w.err.String(), "\n"); got != 1 {
		t.Fatalf("the log grew to %d lines: %q", got, w.err.String())
	}
	// The line is emptied by someone: the waiting events are told.
	w.windows.ClearInputLine("@1")
	w.pass(t)
	if got := w.windows.Typed("@1"); len(got) != 2 {
		t.Fatalf("typed %q, want the second nudge once the line was empty", got)
	}
}

func TestNudgeAnEnterThatTookIsNotPressedAgain(t *testing.T) {
	w := newNudgeWorld(t)
	w.subs.Set("mayor", `kinds = ["message"]`)
	w.windows.HoldsWithID("@1", "mayor-2026-10-01-02", time.Time{})
	w.pass(t)
	appendAll(t, w.log, jobEvent, msgEvent)
	w.pass(t)
	if got := w.windows.Enters("@1"); got != 1 {
		t.Fatalf("Enter pressed %d times, want 1", got)
	}
	if w.err.Len() != 0 {
		t.Fatalf("nothing to say, got %q", w.err.String())
	}
}

// A nudge held while the pane was busy is not typed for mail the seat read in
// the meantime (mw-gq6.572): only the mail still unread is counted, and mail
// all read is not told at all.
func TestNudgeTellsOnlyMailStillUnreadWhenAHeldNudgeIsTyped(t *testing.T) {
	w := newNudgeWorld(t)
	mailbox := apptest.NewFakeMailbox()
	w.nudge.Inbox = mailbox.Inbox
	w.subs.Set("mayor", `kinds = ["mail"]`)
	w.windows.HoldsWithID("@1", "mayor-2026-10-01-02", time.Time{})
	w.pass(t)
	var ids []string
	for _, subject := range []string{"one", "two", "three"} {
		id, err := mailbox.Send(context.Background(), application.NewMessage{From: "builder", To: "mayor", Subject: subject, Body: "b"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		appendAll(t, w.log, events.Event{Kind: events.KindMail, Bead: id, Detail: "mayor"})
	}
	_ = w.windows.Pane("@1", application.PaneWorking)
	w.pass(t)
	if _, err := mailbox.Read(context.Background(), ids[0], "mayor"); err != nil {
		t.Fatal(err)
	}
	if _, err := mailbox.Read(context.Background(), ids[1], "mayor"); err != nil {
		t.Fatal(err)
	}
	_ = w.windows.Pane("@1", application.PaneIdle)
	w.pass(t)
	if got := w.windows.Typed("@1"); len(got) != 1 || got[0] != "New mail for mayor: 1 message(s): "+ids[2]+". Run bd mail inbox.\n" {
		t.Fatalf("typed %q, want the one mail still unread", got)
	}

	// All read before the pane is idle: nothing is typed, now or later.
	id, err := mailbox.Send(context.Background(), application.NewMessage{From: "builder", To: "mayor", Subject: "four", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	appendAll(t, w.log, events.Event{Kind: events.KindMail, Bead: id, Detail: "mayor"})
	_ = w.windows.Pane("@1", application.PaneWorking)
	w.pass(t)
	if _, err := mailbox.Read(context.Background(), id, "mayor"); err != nil {
		t.Fatal(err)
	}
	_ = w.windows.Pane("@1", application.PaneIdle)
	w.pass(t)
	w.pass(t)
	if got := w.windows.Typed("@1"); len(got) != 1 {
		t.Fatalf("typed %q for mail already read", got)
	}
}

// The mail line names the ids it counts, in seq order, the first ten and then
// "and <k> more", in the words contrib/mail-notify types (mw-gq6.693).
func TestNudgeMailLineNamesTheIdsItCounts(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		want string
	}{
		{"three", 3, "New mail for mayor: 3 message(s): mw-id00, mw-id01, mw-id02. Run bd mail inbox.\n"},
		{"twelve", 12, "New mail for mayor: 12 message(s): mw-id00, mw-id01, mw-id02, mw-id03, mw-id04, mw-id05, mw-id06, mw-id07, mw-id08, mw-id09 and 2 more. Run bd mail inbox.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newNudgeWorld(t)
			w.subs.Set("mayor", `kinds = ["mail"]`)
			w.windows.HoldsWithID("@1", "mayor-2026-10-01-02", time.Time{})
			w.pass(t)
			for i := 0; i < tc.n; i++ {
				appendAll(t, w.log, events.Event{Kind: events.KindMail, Bead: fmt.Sprintf("mw-id%02d", i), Detail: "mayor"})
			}
			w.pass(t)
			if got := w.windows.Typed("@1"); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("typed %q, want %q", got, tc.want)
			}
		})
	}
}

// A mail event stillUnread drops (already read) is neither counted nor named.
func TestNudgeMailLineLeavesOutMailAlreadyRead(t *testing.T) {
	w := newNudgeWorld(t)
	mailbox := apptest.NewFakeMailbox()
	w.nudge.Inbox = mailbox.Inbox
	w.subs.Set("mayor", `kinds = ["mail"]`)
	w.windows.HoldsWithID("@1", "mayor-2026-10-01-02", time.Time{})
	w.pass(t)
	var ids []string
	for _, subject := range []string{"one", "two", "three"} {
		id, err := mailbox.Send(context.Background(), application.NewMessage{From: "builder", To: "mayor", Subject: subject, Body: "b"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		appendAll(t, w.log, events.Event{Kind: events.KindMail, Bead: id, Detail: "mayor"})
	}
	if _, err := mailbox.Read(context.Background(), ids[1], "mayor"); err != nil {
		t.Fatal(err)
	}
	w.pass(t)
	want := "New mail for mayor: 2 message(s): " + ids[0] + ", " + ids[2] + ". Run bd mail inbox.\n"
	if got := w.windows.Typed("@1"); len(got) != 1 || got[0] != want {
		t.Fatalf("typed %q, want %q", got, want)
	}
}

// MailNudgeLine is the one place the line is made.
func TestMailNudgeLineNamesAtMostTenIds(t *testing.T) {
	if got, want := application.MailNudgeLine("deputy", []string{"a"}), "New mail for deputy: 1 message(s): a. Run bd mail inbox."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
