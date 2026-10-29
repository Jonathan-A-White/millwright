package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

func logWith(t *testing.T, evs ...events.Event) *apptest.FakeEventLog {
	t.Helper()
	log := &apptest.FakeEventLog{}
	appendAll(t, log, evs...)
	return log
}

func appendAll(t *testing.T, log *apptest.FakeEventLog, evs ...events.Event) {
	t.Helper()
	for _, e := range evs {
		e.Ts = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		e.Lane = events.LaneNormal
		if _, err := log.Append(context.Background(), []events.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
}

var (
	jobEvent  = events.Event{Kind: events.KindJob, Actor: "dispatch@laptop", From: events.JobScheduled, To: events.JobRunning}
	mailEvent = events.Event{Kind: events.KindMail, Bead: "mw-m1", Actor: "mw@laptop", Detail: "mayor"}
	msgEvent  = events.Event{Kind: events.KindMessage, Bead: "mw-1", Detail: "direct:abc"}
)

func TestWaitReturnsOnTheFirstMatchingKindAndIgnoresOthers(t *testing.T) {
	log := logWith(t, jobEvent)
	var out bytes.Buffer
	polls := 0
	sleep := func(ctx context.Context, _ time.Duration) error {
		polls++
		switch polls {
		case 1: // a job and a mail for another box: neither ends the wait
			appendAll(t, log, jobEvent, events.Event{Kind: events.KindMail, Bead: "mw-m0", Detail: "deputy"})
		case 2:
			appendAll(t, log, mailEvent, msgEvent)
		}
		return nil
	}
	got, err := application.EventWait{
		Log:          log,
		Subscription: application.Subscription{Seat: "mayor", Kinds: []string{"mail"}},
		Sleep:        sleep,
		Out:          &out,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if len(got) != 1 || got[0].Kind != events.KindMail || got[0].Bead != "mw-m1" {
		t.Fatalf("returned %+v, want the one mail for mayor", got)
	}
	if polls != 2 {
		t.Fatalf("it polled %d times, want 2: the events before the match were not matches", polls)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || lines[0] != "New events for mayor: 1. Run mw events tail --since 3." || !strings.Contains(lines[1], " mail mw-m1 mw@laptop - mayor") {
		t.Fatalf("printed %q", out.String())
	}
}

func TestWaitWithSinceReturnsAtOnceOnWhatIsAlreadyThere(t *testing.T) {
	log := logWith(t, jobEvent, mailEvent, msgEvent)
	since := uint64(0)
	var out bytes.Buffer
	got, err := application.EventWait{
		Log:          log,
		Subscription: application.Subscription{Seat: "mayor", Kinds: []string{"mail", "message"}},
		Since:        &since,
		Sleep:        func(context.Context, time.Duration) error { t.Fatal("it slept"); return nil },
		Out:          &out,
	}.Run(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v; want both matches", got, err)
	}
	if first := strings.SplitN(out.String(), "\n", 2)[0]; first != "New events for mayor: 2. Run mw events tail --since 1." {
		t.Fatalf("first line %q", first)
	}
}

func TestWaitStartsAtTheHeadWithoutSince(t *testing.T) {
	log := logWith(t, mailEvent) // already there before the wait: not new
	polls := 0
	got, err := application.EventWait{
		Log:          log,
		Subscription: application.Subscription{Seat: "mayor", Kinds: []string{"mail"}},
		Limit:        time.Second,
		Sleep: func(context.Context, time.Duration) error {
			polls++
			if polls == 1 {
				appendAll(t, log, mailEvent)
			}
			return nil
		},
		Out: &bytes.Buffer{},
	}.Run(context.Background())
	if err != nil || len(got) != 1 || got[0].Seq != 2 {
		t.Fatalf("got %+v, %v; want the mail that came after the wait began", got, err)
	}
}

func TestWaitEndsOnItsLimitSayingSo(t *testing.T) {
	var out bytes.Buffer
	got, err := application.EventWait{
		Log:          logWith(t, jobEvent),
		Subscription: application.Subscription{Seat: "mayor", Kinds: []string{"mail"}},
		Limit:        30 * time.Millisecond,
		Every:        time.Millisecond,
		Out:          &out,
	}.Run(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if !strings.Contains(out.String(), "No events for mayor by ") || !strings.Contains(out.String(), "Arm it again.") {
		t.Fatalf("printed %q", out.String())
	}
}

// countingBox counts the calls to Inbox, to say the wait asks the box once.
type countingBox struct {
	*apptest.FakeMailbox
	inboxCalls int
}

func (b *countingBox) Inbox(ctx context.Context, mailbox string) ([]application.Message, error) {
	b.inboxCalls++
	return b.FakeMailbox.Inbox(ctx, mailbox)
}

func boxWithMail(t *testing.T, to string) *countingBox {
	t.Helper()
	box := &countingBox{FakeMailbox: apptest.NewFakeMailbox()}
	if _, err := box.Send(context.Background(), application.NewMessage{From: "mayor", To: to, Subject: "s"}); err != nil {
		t.Fatal(err)
	}
	return box
}

func TestWaitWithMailKindAndNoSinceEndsAtOnceOnUnreadMailInTheBox(t *testing.T) {
	box := boxWithMail(t, "deputy")
	var out bytes.Buffer
	got, err := application.EventWait{
		Log:          logWith(t, jobEvent),
		Subscription: application.Subscription{Seat: "deputy", Kinds: []string{"mail", "message"}},
		Box:          box,
		Sleep:        func(context.Context, time.Duration) error { t.Fatal("it slept"); return nil },
		Out:          &out,
	}.Run(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want it to end at once with no log events", got, err)
	}
	if want := "New events for deputy: unread mail in its box (1). Run bd mail inbox.\n"; out.String() != want {
		t.Fatalf("printed %q, want %q", out.String(), want)
	}
}

func TestWaitWithMailKindAndAnEmptyBoxStillBlocksUntilANewMail(t *testing.T) {
	box := boxWithMail(t, "mayor") // mail for another box does not count
	log := logWith(t, jobEvent)
	polls := 0
	var out bytes.Buffer
	got, err := application.EventWait{
		Log:          log,
		Subscription: application.Subscription{Seat: "deputy", Kinds: []string{"mail"}},
		Box:          box,
		Sleep: func(context.Context, time.Duration) error {
			polls++
			if polls == 2 {
				appendAll(t, log, events.Event{Kind: events.KindMail, Bead: "mw-m2", Detail: "deputy"})
			}
			return nil
		},
		Out: &out,
	}.Run(context.Background())
	if err != nil || len(got) != 1 || got[0].Bead != "mw-m2" || polls != 2 {
		t.Fatalf("got %+v, %v after %d polls; want the new mail after two", got, err, polls)
	}
	if box.inboxCalls != 1 {
		t.Fatalf("the box was read %d times, want once at the start and never in the loop", box.inboxCalls)
	}
}

func TestWaitGivenSinceDoesNotConsultTheBox(t *testing.T) {
	box := boxWithMail(t, "deputy")
	since := uint64(0)
	got, err := application.EventWait{
		Log:          logWith(t, mailEvent),
		Subscription: application.Subscription{Seat: "mayor", Kinds: []string{"mail"}},
		Since:        &since,
		Box:          box,
		Sleep:        func(context.Context, time.Duration) error { t.Fatal("it slept"); return nil },
		Out:          &bytes.Buffer{},
	}.Run(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if box.inboxCalls != 0 {
		t.Fatalf("the box was read %d times, want none with --since", box.inboxCalls)
	}
}

func TestWaitWithoutMailKindDoesNotConsultTheBox(t *testing.T) {
	box := boxWithMail(t, "deputy")
	_, err := application.EventWait{
		Log:          logWith(t, jobEvent),
		Subscription: application.Subscription{Seat: "deputy", Kinds: []string{"message"}},
		Box:          box,
		Limit:        20 * time.Millisecond,
		Every:        time.Millisecond,
		Out:          &bytes.Buffer{},
	}.Run(context.Background())
	if err != nil || box.inboxCalls != 0 {
		t.Fatalf("err %v, box read %d times; want none", err, box.inboxCalls)
	}
}

func handoverEvent(seat, successor string, at uint64) events.Event {
	return events.Event{Kind: events.KindHandover, Actor: seat, Detail: events.Handover{Seat: seat, Successor: successor, At: at}.Detail()}
}

// A handover to this window that landed before the wait was armed (the
// successor reading its boot while the old session handed over: mw-gq6.668)
// ends the wait at once: the seat is already this window's.
func TestHandoverWaitBegunAfterAHandoverToItsWindowHoldsTheSeatAtOnce(t *testing.T) {
	log := logWith(t, jobEvent, mailEvent, handoverEvent("mayor", "mayor-w", 2), jobEvent)
	var out bytes.Buffer
	got, err := application.EventWait{
		Log:          log,
		Subscription: application.Subscription{Seat: "mayor", Kinds: []string{events.KindHandover}},
		Self:         "mayor-w",
		Sleep:        func(context.Context, time.Duration) error { t.Fatal("it slept"); return nil },
		Out:          &out,
	}.Run(context.Background())
	if err != nil || len(got) != 1 || got[0].Seq != 3 {
		t.Fatalf("got %+v, %v; want the handover, event 3", got, err)
	}
	if first := strings.SplitN(out.String(), "\n", 2)[0]; !strings.HasPrefix(first, "You hold the mayor seat from event 2:") {
		t.Fatalf("printed %q, want 'You hold the mayor seat from event 2'", out.String())
	}
}

// The look-back reads the acting file when it is given: the seat is this
// window's only when the file names it, and a handover older than the event
// the file stands at is stale (mw-gq6.767).
func TestHandoverWaitLookBackHoldsTheSeatOnlyWhenTheActingFileNamesItsWindowAtThatHandover(t *testing.T) {
	cases := []struct {
		name, acting string
		holds        bool
	}{
		{"names this window at the handover's event", "Mayor mayor-w (handed over at event 2), since 2026-10-25T02:40:00Z\n", true},
		{"names another window (the predecessor still acts)", "Mayor after handoff 592 (tmux window 0 'mayor-592', window id @3, desktop), since 2026-10-25T02:40:00Z\n", false},
		{"names this window at a newer event", "Mayor mayor-w (handed over at event 9), since 2026-10-25T03:40:00Z\n", false},
		{"names a longer window that begins with this one's", "Mayor mayor-w2 (handed over at event 2), since 2026-10-25T02:40:00Z\n", false},
		{"no acting file", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			acting := c.acting
			got, err := application.EventWait{
				Log:          logWith(t, jobEvent, mailEvent, handoverEvent("mayor", "mayor-w", 2), jobEvent),
				Subscription: application.Subscription{Seat: "mayor", Kinds: []string{events.KindHandover}},
				Self:         "mayor-w",
				Acting:       &acting,
				Limit:        20 * time.Millisecond,
				Every:        time.Millisecond,
				Out:          &out,
			}.Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			held := strings.Contains(out.String(), "You hold the mayor seat from event 2")
			if held != c.holds || (c.holds && len(got) != 1) || (!c.holds && len(got) != 0) {
				t.Fatalf("got %+v, printed %q; want holds=%v", got, out.String(), c.holds)
			}
		})
	}
}

func TestHandoverWaitBegunAfterAHandoverToAnotherWindowOrAwayFromItsWindowWaits(t *testing.T) {
	cases := map[string][]events.Event{
		"to another window":  {handoverEvent("mayor", "mayor-other", 1)},
		"then away from it":  {handoverEvent("mayor", "mayor-w", 1), handoverEvent("mayor", "mayor-next", 2)},
		"another seat's":     {handoverEvent("deputy", "mayor-w", 1)},
		"no handover at all": {jobEvent},
	}
	for name, evs := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			got, err := application.EventWait{
				Log:          logWith(t, evs...),
				Subscription: application.Subscription{Seat: "mayor", Kinds: []string{events.KindHandover}},
				Self:         "mayor-w",
				Limit:        20 * time.Millisecond,
				Every:        time.Millisecond,
				Out:          &out,
			}.Run(context.Background())
			if err != nil || len(got) != 0 {
				t.Fatalf("got %+v, %v; want it to wait out its limit", got, err)
			}
			if !strings.Contains(out.String(), "No events for mayor by ") {
				t.Fatalf("printed %q, want the time-limit line", out.String())
			}
		})
	}
}

func TestHandoverWaitGivenSincePastTheHandoverIgnoresIt(t *testing.T) {
	log := logWith(t, handoverEvent("mayor", "mayor-w", 0), jobEvent)
	since := uint64(1)
	var out bytes.Buffer
	got, err := application.EventWait{
		Log:          log,
		Subscription: application.Subscription{Seat: "mayor", Kinds: []string{events.KindHandover}},
		Self:         "mayor-w",
		Since:        &since,
		Limit:        20 * time.Millisecond,
		Every:        time.Millisecond,
		Out:          &out,
	}.Run(context.Background())
	if err != nil || len(got) != 0 || strings.Contains(out.String(), "You hold") {
		t.Fatalf("got %+v, %v, printed %q; want the handover ignored", got, err, out.String())
	}
}

// The look-back is for a wait that listens for the handover: a mail wait
// re-armed in the successor's session must not end on the old handover.
func TestWaitWithoutTheHandoverKindDoesNotLookBackForOne(t *testing.T) {
	got, err := application.EventWait{
		Log:          logWith(t, handoverEvent("mayor", "mayor-w", 0)),
		Subscription: application.Subscription{Seat: "mayor", Kinds: []string{events.KindMail}},
		Self:         "mayor-w",
		Limit:        20 * time.Millisecond,
		Every:        time.Millisecond,
		Out:          &bytes.Buffer{},
	}.Run(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want it to wait out its limit", got, err)
	}
}

// A Mayor started because its predecessor is gone waits for a handover that
// can never come: the acting file names a window that is no longer open
// (mw-gq6.831).
func TestHandoverWaitEndsEarlyWhenTheActingFilesWindowIsGone(t *testing.T) {
	const acting = "Mayor mayor-2026-10-28-659 (handed over at event 41), since 2026-10-28T21:00:00Z\n"
	cases := []struct {
		name   string
		acting string
		open   []string
		err    error
		gone   bool
	}{
		{"the named window is closed", acting, []string{"mayor-2026-10-29-662", "dispatch"}, nil, true},
		{"no windows are open", acting, nil, nil, true},
		{"the named window is open", acting, []string{"mayor-2026-10-28-659", "mayor-2026-10-29-662"}, nil, false},
		{"it names a window by the old handoff form, open", "Mayor after handoff 592 (tmux window 0 'mayor-592', window id @3), since x\n", []string{"mayor-592"}, nil, false},
		{"it names a window by the old handoff form, closed", "Mayor after handoff 592 (tmux window 0 'mayor-592', window id @3), since x\n", []string{"millhand"}, nil, true},
		{"the file names this window itself", "Mayor mayor-2026-10-29-662 (taken by hand; window @4), since x\n", []string{"millhand"}, nil, false},
		{"the file names no window", "Mayor after the handoff, since x\n", []string{"millhand"}, nil, false},
		{"only a script is named, not a window", "Mayor started by bin/mayor-up, since x\n", []string{"millhand"}, nil, false},
		{"no acting file", "", []string{"millhand"}, nil, false},
		{"tmux cannot say", acting, nil, context.DeadlineExceeded, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			acting := c.acting
			_, err := application.EventWait{
				Log:          logWith(t, jobEvent),
				Subscription: application.Subscription{Seat: "mayor", Kinds: []string{events.KindHandover}},
				Self:         "mayor-2026-10-29-662",
				Acting:       &acting,
				OpenWindows:  func(context.Context) ([]string, error) { return c.open, c.err },
				Limit:        20 * time.Millisecond,
				Every:        time.Millisecond,
				Out:          &out,
			}.Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			ended := strings.Contains(out.String(), "no handover can come")
			if ended != c.gone || ended == strings.Contains(out.String(), "No events for mayor by ") {
				t.Fatalf("printed %q; want gone=%v", out.String(), c.gone)
			}
			if ended && !strings.Contains(out.String(), "mw seat take mayor") {
				t.Fatalf("printed %q; want it to say how to take the seat", out.String())
			}
		})
	}
}

func TestHandoverWaitFindsThePredecessorGoneWhileItWaits(t *testing.T) {
	acting := "Mayor mayor-2026-10-28-659 (handed over at event 41), since x\n"
	calls := 0
	var out bytes.Buffer
	_, err := application.EventWait{
		Log:          logWith(t, jobEvent),
		Subscription: application.Subscription{Seat: "mayor", Kinds: []string{events.KindHandover}},
		Self:         "mayor-2026-10-29-662",
		Acting:       &acting,
		OpenWindows: func(context.Context) ([]string, error) {
			calls++
			if calls < 3 {
				return []string{"mayor-2026-10-28-659"}, nil
			}
			return []string{"millhand"}, nil
		},
		Limit: 5 * time.Second,
		Every: time.Millisecond,
		Out:   &out,
	}.Run(context.Background())
	if err != nil || !strings.Contains(out.String(), "no handover can come") {
		t.Fatalf("printed %q, %v; want the gone line once the window closed", out.String(), err)
	}
}
