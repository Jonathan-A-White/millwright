package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// quietStream says hello and then holds the stream open until the wait ends:
// a backend with nothing new.
type quietStream struct{}

func (quietStream) Events(ctx context.Context, onEvent func(application.PosternEvent) error) error {
	if err := onEvent(application.PosternEvent{Kind: application.PosternEventHello}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *talkPosternFixture) handoverWait(stream application.PosternStream, log application.EventLog, self string, limit, every time.Duration) (application.TalkWaitReport, string) {
	f.t.Helper()
	var out strings.Builder
	report, err := application.TalkWait{
		Stream:  stream,
		Postern: f.postern,
		Cipher:  apptest.NewFakeCipher(),
		Keys:    stubPosternKeys{pubKey: talkWaitMayorKey},
		Memory:  f.tracker, GovernorKey: talkWaitGovernorKey,
		Limit: limit, MinBackoff: 5 * time.Millisecond, MaxBackoff: 5 * time.Millisecond,
		Log: log, Self: self, HandoverEvery: every,
		Out: &out,
	}.Run(context.Background())
	if err != nil {
		f.t.Fatalf("the wait: %v", err)
	}
	return report, out.String()
}

func TestTalkWaitInTheOldWindowExitsAtOnceOnAHandoverSayingHandedOverAtN(t *testing.T) {
	f := newTalkPosternFixture(t)
	log := logWith(t, jobEvent, mailEvent)
	go func() {
		time.Sleep(30 * time.Millisecond)
		handoverAt(t, log, 2)
	}()

	started := time.Now()
	report, printed := f.handoverWait(quietStream{}, log, oldMayorWindow, 5*time.Second, 5*time.Millisecond)

	if time.Since(started) > 2*time.Second {
		t.Errorf("the wait took %s: it should end on the handover, not its limit", time.Since(started))
	}
	if report.HandedOver == nil || report.HandedOver.At != 2 || report.HandedOver.Successor != newMayorWindow {
		t.Fatalf("report %+v", report.HandedOver)
	}
	if !strings.Contains(printed, "handed over at 2") || strings.Contains(printed, "No talk turn") {
		t.Errorf("printed %q", printed)
	}
}

// beforeStream runs something just before its stream's first event, as the
// old Mayor's handover lands between the wait's start and the Governor's turn.
type beforeStream struct {
	inner  application.PosternStream
	before func()
}

func (s beforeStream) Events(ctx context.Context, onEvent func(application.PosternEvent) error) error {
	s.before()
	return s.inner.Events(ctx, onEvent)
}

// A turn that arrives after the handover is the successor's: the old wait does
// not print it and leaves the cursor where it was, so the successor, reading
// from the same cursor, hears it.
func TestTalkWaitInTheOldWindowLeavesATurnAfterTheHandoverToTheSuccessor(t *testing.T) {
	f := newTalkPosternFixture(t)
	log := logWith(t, jobEvent)
	seq := f.addTurn("is anyone there?")
	// Its own look at the log is an hour away: only the check before it takes a turn can see this.
	stream := beforeStream{inner: talkWaitStream{seq: seq}, before: func() { handoverAt(t, log, 1) }}
	report, printed := f.handoverWait(stream, log, oldMayorWindow, time.Second, time.Hour)

	if report.Turn != nil || report.HandedOver == nil {
		t.Fatalf("turn %+v, handed over %+v: the old wait must not take the turn", report.Turn, report.HandedOver)
	}
	if strings.Contains(printed, "is anyone there?") || !strings.Contains(printed, "handed over at 1") {
		t.Errorf("printed %q", printed)
	}
	if saved, _ := f.tracker.Note(context.Background(), application.TalkWaitCursorKey); saved != "" && saved != "0" {
		t.Errorf("the old wait moved the talk cursor to %s", saved)
	}

	// The successor's wait, from the same cursor, hears the turn.
	successor, said := f.handoverWait(talkWaitStream{seq: seq}, log, newMayorWindow, time.Second, time.Hour)
	if successor.Turn == nil || successor.Turn.Text != "is anyone there?" {
		t.Fatalf("the successor heard %+v, printed %q", successor.Turn, said)
	}
}

func TestTalkWaitInTheSuccessorsWindowIsNotEndedByItsOwnHandover(t *testing.T) {
	f := newTalkPosternFixture(t)
	log := logWith(t, jobEvent)
	go func() {
		time.Sleep(20 * time.Millisecond)
		handoverAt(t, log, 1)
	}()
	report, printed := f.handoverWait(quietStream{}, log, newMayorWindow, 150*time.Millisecond, 5*time.Millisecond)
	if report.HandedOver != nil || !strings.Contains(printed, "No talk turn") {
		t.Errorf("handed over %+v, printed %q: the successor's wait should run to its limit", report.HandedOver, printed)
	}
}

func TestTalkWaitWithNoLogIsAsItWas(t *testing.T) {
	f := newTalkPosternFixture(t)
	report, _ := f.run(0)
	if report.HandedOver != nil {
		t.Errorf("handed over %+v", report.HandedOver)
	}
}
