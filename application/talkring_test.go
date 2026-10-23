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

var ringClock = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

// ringInto rings once into log, keeping the ring's note in notes, at ringClock
// plus after, and gives the txid the backend gave it.
func ringInto(t *testing.T, log application.EventLog, notes application.PosternNotes, after time.Duration, text string) string {
	t.Helper()
	backend := apptest.NewFakePostern()
	backend.NextTxid = strings.Repeat("ab", 32)
	_, err := application.TalkCall{
		Postern: backend, Chain: apptest.NewFakeChain(backend, echoKeys{stubPosternKeys{pubKey: "mayor-pubkey-hex"}}),
		Cipher: apptest.NewFakeCipher(),
		Keys:   echoKeys{stubPosternKeys{pubKey: "mayor-pubkey-hex"}}, GovernorKey: "governor-pubkey-hex",
		FloatSats: 100000, Log: log, Actor: "mayor@laptop", Notes: notes,
		Now: func() time.Time { return ringClock.Add(after) },
	}.Run(context.Background(), application.TalkCallRequest{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return backend.NextTxid
}

func eventsOf(t *testing.T, log application.EventLog) []events.Event {
	t.Helper()
	got, err := log.Since(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTalkCallRingEventNamesWhoCallsAndWhyAndShowsNoTxid(t *testing.T) {
	log := &apptest.FakeEventLog{}
	txid := ringInto(t, log, apptest.NewFakeTracker(), 0, "Back now: two landings.")

	got := eventsOf(t, log)
	if len(got) != 1 || got[0].Lane != events.LaneEmergency {
		t.Fatalf("want one emergency event, got %+v", got)
	}
	if want := "The Mayor is calling: Back now: two landings."; got[0].Detail != want {
		t.Errorf("detail is %q, want %q", got[0].Detail, want)
	}
	if strings.Contains(got[0].Detail, txid) {
		t.Errorf("the ring's text carries its txid: %q", got[0].Detail)
	}
}

// A ring followed by the Governor's answer writes one event in the normal lane
// whose clears is the ring's seq.
func TestTalkWaitClearsTheRingWhenTheGovernorAnswers(t *testing.T) {
	log := &apptest.FakeEventLog{}
	f := newTalkPosternFixture(t)
	ringInto(t, log, f.tracker, 0, "Back now.")
	ring := eventsOf(t, log)[0]
	seq := f.addTurn("I am here")

	f.runLogged(seq, log, ringClock.Add(time.Minute))

	got := eventsOf(t, log)
	if len(got) != 2 {
		t.Fatalf("want the ring and its clear, got %+v", got)
	}
	if c := got[1]; c.Lane != events.LaneNormal || c.Clears != ring.Seq || c.Detail == "" {
		t.Errorf("the clear is %+v, want a normal-lane event clearing seq %d in words", c, ring.Seq)
	}
	// Answered once: a second wait writes nothing more.
	f.runLogged(seq, log, ringClock.Add(2*time.Minute))
	if n := len(eventsOf(t, log)); n != 2 {
		t.Errorf("%d events after a second wait, want 2", n)
	}
}

func TestTalkWaitClearsAnUnansweredRingOnceItsTimeIsUp(t *testing.T) {
	log := &apptest.FakeEventLog{}
	f := newTalkPosternFixture(t)
	ringInto(t, log, f.tracker, 0, "Back now.")
	ring := eventsOf(t, log)[0]

	f.runLogged(0, log, ringClock.Add(application.TalkRingFor-time.Second))
	if n := len(eventsOf(t, log)); n != 1 {
		t.Fatalf("a ring still inside its time was cleared: %+v", eventsOf(t, log))
	}
	f.runLogged(0, log, ringClock.Add(application.TalkRingFor+time.Second))
	got := eventsOf(t, log)
	if len(got) != 2 || got[1].Clears != ring.Seq || got[1].Lane != events.LaneNormal {
		t.Fatalf("want a normal-lane clear of seq %d, got %+v", ring.Seq, got)
	}
}

func TestTalkCallClearsTheRingItReplaces(t *testing.T) {
	log := &apptest.FakeEventLog{}
	notes := apptest.NewFakeTracker()
	ringInto(t, log, notes, 0, "First.")
	first := eventsOf(t, log)[0]
	ringInto(t, log, notes, time.Minute, "Second.")

	got := eventsOf(t, log)
	if len(got) != 3 || got[1].Clears != first.Seq || got[1].Lane != events.LaneNormal || got[2].Lane != events.LaneEmergency {
		t.Fatalf("want ring, clear of the first, second ring; got %+v", got)
	}
}

// runLogged is run with the event log and a clock at now.
func (f *talkPosternFixture) runLogged(head int64, log application.EventLog, now time.Time) {
	f.t.Helper()
	var out strings.Builder
	_, err := application.TalkWait{
		Stream:      talkWaitStream{seq: head},
		Postern:     f.postern,
		Cipher:      apptest.NewFakeCipher(),
		Keys:        stubPosternKeys{pubKey: talkWaitMayorKey},
		Memory:      f.tracker,
		Log:         log,
		Self:        "mayor",
		GovernorKey: talkWaitGovernorKey,
		Limit:       200 * time.Millisecond,
		MinBackoff:  5 * time.Millisecond,
		MaxBackoff:  5 * time.Millisecond,
		Now:         func() time.Time { return now },
		Out:         &out,
	}.Run(context.Background())
	if err != nil {
		f.t.Fatalf("the wait: %v", err)
	}
}
