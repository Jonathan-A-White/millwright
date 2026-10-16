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
