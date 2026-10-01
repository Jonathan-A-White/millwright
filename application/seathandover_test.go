package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

const (
	oldMayorWindow = "mayor-2026-10-01-159"
	newMayorWindow = "mayor-2026-10-01-160"
)

type handoverFixture struct {
	log     *apptest.FakeEventLog
	windows *apptest.FakeWindows
	acting  *apptest.FakeActingFile
	out     bytes.Buffer
}

// aHandover is a log of 3 events and a terminal where the old Mayor's window
// @4 is the one running the command and the successor's, @5, is open.
func aHandover(t *testing.T) *handoverFixture {
	t.Helper()
	f := &handoverFixture{
		log:     logWith(t, jobEvent, mailEvent, msgEvent),
		windows: apptest.NewFakeWindows(),
		acting:  &apptest.FakeActingFile{},
	}
	opened := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	f.windows.HoldsWithID("@4", oldMayorWindow, opened)
	f.windows.HoldsWithID("@5", newMayorWindow, opened.Add(time.Hour))
	f.windows.HoldsWithID("@6", "deputy-2026-10-01-02", opened.Add(2*time.Hour))
	f.windows.RunsIn("@4")
	return f
}

func (f *handoverFixture) handover() application.SeatHandover {
	return application.SeatHandover{
		Seat: "mayor", Log: f.log, Terminal: f.windows, Acting: f.acting,
		Now: func() time.Time { return time.Date(2026, 10, 1, 14, 30, 0, 0, time.UTC) },
		Out: &f.out,
	}
}

func TestAHandoverWritesTheActingFileWithNameAndSeqAndEmitsTheEvent(t *testing.T) {
	f := aHandover(t)
	at := uint64(2)

	ev, err := f.handover().Run(context.Background(), application.SeatHandoverRequest{At: &at})
	if err != nil {
		t.Fatal(err)
	}

	acting := f.acting.Text("mayor")
	if !strings.Contains(acting, newMayorWindow) || !strings.Contains(acting, "event 2") {
		t.Errorf("the acting file %q should name the successor and event 2", acting)
	}
	if strings.Contains(acting, oldMayorWindow) {
		t.Errorf("the acting file %q must not name the old window, or a window cannot be told from it", acting)
	}
	if ev.Seq != 4 || ev.Kind != events.KindHandover {
		t.Fatalf("emitted %+v, want a handover at seq 4", ev)
	}
	h, ok := events.HandoverOf(f.log.All()[3])
	if !ok || h != (events.Handover{Seat: "mayor", Successor: newMayorWindow, At: 2}) {
		t.Errorf("the log's event says %+v, %v", h, ok)
	}
	for _, want := range []string{"2", newMayorWindow} {
		if !strings.Contains(f.out.String(), want) {
			t.Errorf("printed %q, want it to say %q", f.out.String(), want)
		}
	}
}

func TestAHandoverWithNoSeqMarksTheLogsHead(t *testing.T) {
	f := aHandover(t)
	if _, err := f.handover().Run(context.Background(), application.SeatHandoverRequest{}); err != nil {
		t.Fatal(err)
	}
	h, _ := events.HandoverOf(f.log.All()[3])
	if h.At != 3 {
		t.Errorf("marked event %d, want the head, 3", h.At)
	}
}

func TestAHandoverRefusesAndWritesNothingWhenItCannotBeOneClean(t *testing.T) {
	past := uint64(9)
	for name, tc := range map[string]struct {
		change func(*handoverFixture)
		req    application.SeatHandoverRequest
		want   string
	}{
		"an event the log does not have":     {req: application.SeatHandoverRequest{At: &past}, want: "head is 3"},
		"no successor window open":           {change: func(f *handoverFixture) { f.windows.Vanish("@5") }, want: "no window"},
		"a named successor that is not open": {req: application.SeatHandoverRequest{Successor: "mayor-nope-1"}, want: "mayor-nope-1"},
		"its own window named":               {req: application.SeatHandoverRequest{Successor: oldMayorWindow}, want: "this window"},
	} {
		f := aHandover(t)
		if tc.change != nil {
			tc.change(f)
		}
		_, err := f.handover().Run(context.Background(), tc.req)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error saying %q", name, err, tc.want)
		}
		if len(f.log.All()) != 3 || f.acting.Text("mayor") != "" {
			t.Errorf("%s: something was written", name)
		}
	}
}

// The reaper that respawn-mayor armed on the old window keeps closing it: the
// acting file the handover wrote names an open window that is not the old one.
func TestTheReaperClosesTheOldWindowOnceTheHandoverNamedTheSuccessor(t *testing.T) {
	f := aHandover(t)
	files := &fakeSeatFiles{start: aSeatStart(func(s *application.SeatStart) { s.Acting = "Mayor after handoff 159 (" + oldMayorWindow + ")" })}
	sleeps := 0
	report, err := application.SeatReap{
		Seats: files, Terminal: f.windows, Log: &memoryLog{},
		Seat: "mayor", Host: "laptop", Window: "@4",
		Interval: time.Second, Limit: time.Minute,
		Sleep: func(context.Context, time.Duration) error {
			if sleeps++; sleeps == 2 {
				if _, err := f.handover().Run(context.Background(), application.SeatHandoverRequest{}); err != nil {
					t.Fatal(err)
				}
				files.start.Acting = f.acting.Text("mayor")
			}
			return nil
		},
	}.Run(context.Background())
	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("the reaper ended %q with %v", report.Outcome, err)
	}
	if got := f.windows.ClosedIDs(); len(got) != 1 || got[0] != "@4" {
		t.Errorf("closed %v, want the old window @4", got)
	}
}

// handoverAt appends a handover event of the Mayor's to the log.
func handoverAt(t *testing.T, log *apptest.FakeEventLog, at uint64) {
	t.Helper()
	appendAll(t, log, events.Event{Kind: events.KindHandover, Actor: "mayor",
		Detail: events.Handover{Seat: "mayor", Successor: newMayorWindow, At: at}.Detail()})
}

func mayorWait(log application.EventLog, self string, since *uint64, sleep func(context.Context, time.Duration) error, out *bytes.Buffer) application.EventWait {
	return application.EventWait{
		Log:          log,
		Subscription: application.Subscription{Seat: "mayor", Kinds: []string{"mail"}},
		Self:         self, Since: since, Sleep: sleep, Out: out,
	}
}

func TestAWaitForTheOldSeatEndsAtOnceOnAHandoverAndSaysHandedOverAtN(t *testing.T) {
	log := logWith(t, jobEvent)
	var out bytes.Buffer
	sleep := func(context.Context, time.Duration) error {
		// A mail at 2 is the old Mayor's to answer; the handover marks 2, lands as 3.
		appendAll(t, log, mailEvent)
		handoverAt(t, log, 2)
		return nil
	}
	got, err := mayorWait(log, oldMayorWindow, nil, sleep, &out).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != events.KindMail || got[1].Kind != events.KindHandover {
		t.Fatalf("returned %+v, want the mail then the handover", got)
	}
	if !strings.Contains(out.String(), "handed over at 2") || !strings.Contains(out.String(), " mail mw-m1 ") {
		t.Errorf("printed %q", out.String())
	}
}

func TestTheOldSeatIsNotWokenForAnEventPastN(t *testing.T) {
	log := logWith(t, jobEvent)
	var out bytes.Buffer
	sleep := func(context.Context, time.Duration) error {
		handoverAt(t, log, 1)
		appendAll(t, log, mailEvent) // seq 3, past N: the successor's
		return nil
	}
	got, err := mayorWait(log, oldMayorWindow, nil, sleep, &out).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != events.KindHandover {
		t.Fatalf("returned %+v, want only the handover: the mail is the successor's", got)
	}
	if strings.Contains(out.String(), " mail ") {
		t.Errorf("the old seat was shown an event past N: %q", out.String())
	}
}

func TestAWaitWithNoWindowNameIsTheOldSeat(t *testing.T) {
	log := logWith(t, jobEvent)
	var out bytes.Buffer
	sleep := func(context.Context, time.Duration) error { handoverAt(t, log, 1); return nil }
	if _, err := mayorWait(log, "", nil, sleep, &out).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "handed over at 1") {
		t.Errorf("printed %q", out.String())
	}
}

func TestAHandoverForAnotherSeatLeavesAWaitAlone(t *testing.T) {
	log := logWith(t, jobEvent)
	var out bytes.Buffer
	polls := 0
	sleep := func(context.Context, time.Duration) error {
		if polls++; polls == 1 {
			appendAll(t, log, events.Event{Kind: events.KindHandover, Actor: "deputy",
				Detail: events.Handover{Seat: "deputy", Successor: "deputy-9", At: 1}.Detail()})
			return nil
		}
		appendAll(t, log, mailEvent)
		return nil
	}
	got, err := mayorWait(log, oldMayorWindow, nil, sleep, &out).Run(context.Background())
	if err != nil || len(got) != 1 || got[0].Kind != events.KindMail {
		t.Fatalf("got %+v, %v: the deputy's handover is none of the Mayor's", got, err)
	}
}

func TestTheSuccessorsWaitIsWokenByTheHandoverAndToldItHoldsTheSeatFromN(t *testing.T) {
	log := logWith(t, jobEvent, jobEvent)
	var out bytes.Buffer
	sleep := func(context.Context, time.Duration) error {
		appendAll(t, log, mailEvent) // 3: the old Mayor's, before N
		handoverAt(t, log, 3)
		return nil
	}
	got, err := mayorWait(log, newMayorWindow, nil, sleep, &out).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != events.KindHandover {
		t.Fatalf("returned %+v, want only the handover: the mail at 3 is the old Mayor's", got)
	}
	if !strings.Contains(out.String(), "hold the mayor seat from event 3") || !strings.Contains(out.String(), "mw events tail --since 3") {
		t.Errorf("printed %q", out.String())
	}
}

// A wait started for the successor at N ignores everything before N, the
// handover itself included, and ends on the first event after it.
func TestAWaitStartedForTheSuccessorAtNIgnoresEventsBeforeN(t *testing.T) {
	log := logWith(t, jobEvent, mailEvent)
	handoverAt(t, log, 2) // seq 3
	at := uint64(2)
	var out bytes.Buffer
	polls := 0
	sleep := func(context.Context, time.Duration) error {
		if polls++; polls == 1 {
			appendAll(t, log, jobEvent) // nothing it hears
			return nil
		}
		appendAll(t, log, events.Event{Kind: events.KindMail, Bead: "mw-m2", Detail: "mayor"})
		return nil
	}
	got, err := mayorWait(log, newMayorWindow, &at, sleep, &out).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Bead != "mw-m2" || polls != 2 {
		t.Fatalf("returned %+v after %d polls, want the mail at 5 only: the mail at 2 is before N, the handover is its own", got, polls)
	}
}

// A wait for the old seat that begins after the handover was already marked,
// asked for since before it, is told at once.
func TestAnOldWaitArmedAgainSinceBeforeTheHandoverIsTold(t *testing.T) {
	log := logWith(t, jobEvent, mailEvent)
	handoverAt(t, log, 2)
	at := uint64(1)
	var out bytes.Buffer
	got, err := mayorWait(log, oldMayorWindow, &at, nil, &out).Run(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if !strings.Contains(out.String(), "handed over at 2") {
		t.Errorf("printed %q", out.String())
	}
}

// The first live handover (mw-gq6.210): the successor armed its wait at head N
// with nothing since, and the handover marked At = N, landing as N+1. A wait
// begun at the head takes the seat from a handover appended after it began.
func TestASuccessorWaitArmedAtTheHeadTakesTheSeatFromAHandoverThatMarkedTheHead(t *testing.T) {
	log := logWith(t, jobEvent, mailEvent) // head 2
	var out bytes.Buffer
	polls := 0
	sleep := func(context.Context, time.Duration) error {
		if polls++; polls > 3 {
			return errors.New("the wait is still going: it never took the seat")
		}
		if polls == 1 {
			handoverAt(t, log, 2) // marks the head the wait began at; lands as 3
		}
		return nil
	}
	got, err := mayorWait(log, newMayorWindow, nil, sleep, &out).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != events.KindHandover || got[0].Seq != 3 {
		t.Fatalf("returned %+v, want only the handover at 3", got)
	}
	if !strings.Contains(out.String(), "You hold the mayor seat from event 2") {
		t.Errorf("printed %q", out.String())
	}
}

// A wait given --since N keeps its rule: a handover that marked N is the
// one the wait was armed past, and it is ignored.
func TestAWaitGivenSinceNIgnoresAHandoverThatMarkedN(t *testing.T) {
	log := logWith(t, jobEvent, mailEvent)
	handoverAt(t, log, 2) // seq 3
	at := uint64(2)
	var out bytes.Buffer
	polls := 0
	sleep := func(context.Context, time.Duration) error {
		if polls++; polls == 1 {
			handoverAt(t, log, 2) // a second one marking N, seq 4: still ignored
			return nil
		}
		appendAll(t, log, events.Event{Kind: events.KindMail, Bead: "mw-m2", Detail: "mayor"})
		return nil
	}
	got, err := mayorWait(log, newMayorWindow, &at, sleep, &out).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Bead != "mw-m2" || strings.Contains(out.String(), "hold the") {
		t.Fatalf("returned %+v, printed %q: the handovers that marked N are not the wait's to take", got, out.String())
	}
}

// The old window's wait, armed at the head with nothing since, is still told
// "handed over at N" when the handover marks the head.
func TestAnOldWaitArmedAtTheHeadIsToldHandedOverAtTheHead(t *testing.T) {
	log := logWith(t, jobEvent, mailEvent)
	var out bytes.Buffer
	sleep := func(context.Context, time.Duration) error { handoverAt(t, log, 2); return nil }
	got, err := mayorWait(log, oldMayorWindow, nil, sleep, &out).Run(context.Background())
	if err != nil || len(got) != 1 || got[0].Kind != events.KindHandover {
		t.Fatalf("got %+v, %v", got, err)
	}
	if !strings.Contains(out.String(), "handed over at 2") {
		t.Errorf("printed %q", out.String())
	}
}
