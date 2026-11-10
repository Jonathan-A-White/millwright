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

// fakeCeiling is the boot set's ceiling check, answering with err.
type fakeCeiling struct {
	err  error
	seat string
}

func (c *fakeCeiling) CheckCeiling(_ context.Context, seat string) error {
	c.seat = seat
	return c.err
}

// A handover is refused, with nothing written, while the seat's boot set is over
// its ceiling, and goes through when the check passes (mw-gq6.1058).
func TestAHandoverIsRefusedWithNothingWrittenWhileTheBootSetIsOverItsCeiling(t *testing.T) {
	f := aHandover(t)
	over := &application.BootSetOver{Seat: "mayor", Bytes: 15001, Ceiling: 15000, File: "seats/mayor/handoffs/2026-11-10-825.md", FileSize: 7472, Handoff: true}
	ceiling := &fakeCeiling{err: over}
	h := f.handover()
	h.Ceiling = ceiling

	_, err := h.Run(context.Background(), application.SeatHandoverRequest{})
	if err == nil {
		t.Fatal("a boot set over its ceiling was handed over")
	}
	for _, want := range []string{"15001 bytes", "15000 bytes", "seats/mayor/handoffs/2026-11-10-825.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not say %q", err, want)
		}
	}
	if ceiling.seat != "mayor" {
		t.Errorf("checked the %q seat", ceiling.seat)
	}
	if len(f.log.All()) != 3 || f.acting.Text("mayor") != "" || f.out.Len() != 0 {
		t.Error("something was written")
	}

	ceiling.err = nil
	if _, err := h.Run(context.Background(), application.SeatHandoverRequest{}); err != nil {
		t.Fatalf("a boot set within its ceiling: %v", err)
	}
	if len(f.log.All()) != 4 || f.acting.Text("mayor") == "" {
		t.Error("the handover was not written")
	}
}

// aWaitOf is the process the successor's armed wait is: mw events wait for a
// seat, with the kinds given.
func aWaitOf(pid int, seat, kinds string) application.Process {
	return application.Process{Pid: pid, Argv: []string{"/home/mw/bin/mw", "events", "wait", "--for", seat, "--kinds", kinds, "--limit", "50m"}}
}

// lookingTable is a process table that shows its processes only from the look
// numbered from, and notes how many events the log held at each look.
type lookingTable struct {
	from  int
	shows []application.Process
	log   *apptest.FakeEventLog
	looks int
	seen  []int
}

func (l *lookingTable) List(context.Context) ([]application.Process, error) {
	l.looks++
	l.seen = append(l.seen, len(l.log.All()))
	if l.looks < l.from {
		return nil, nil
	}
	return l.shows, nil
}

func (l *lookingTable) Stop(context.Context, int) error { return nil }

func (f *handoverFixture) waiting(table application.UserProcesses) (application.SeatHandover, *aClock) {
	clock := &aClock{now: time.Date(2026, 10, 1, 14, 30, 0, 0, time.UTC)}
	h := f.handover()
	h.Now = clock.Now
	h.Procs = table
	h.Sleep = clock.Sleep
	return h, clock
}

// With --when-waiting the handover waits for the successor's handover wait, and
// emits the event and writes the acting file once, after the look that shows it.
func TestAHandoverWhenWaitingWaitsForTheSuccessorsWaitThenHandsOver(t *testing.T) {
	f := aHandover(t)
	table := &lookingTable{from: 3, shows: []application.Process{aWaitOf(41, "mayor", "mail,handover")}, log: f.log}
	h, _ := f.waiting(table)

	ev, err := h.Run(context.Background(), application.SeatHandoverRequest{WhenWaiting: true, Limit: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if table.looks != 3 {
		t.Errorf("looked %d times, want 3", table.looks)
	}
	for i, n := range table.seen {
		if n != 3 {
			t.Errorf("at look %d the log held %d events: the handover was emitted before the wait was seen", i+1, n)
		}
	}
	if ev.Seq != 4 || len(f.log.All()) != 4 {
		t.Errorf("emitted %+v, the log holds %d events", ev, len(f.log.All()))
	}
	if !strings.Contains(f.acting.Text("mayor"), newMayorWindow) || !strings.Contains(f.out.String(), "Handed over to "+newMayorWindow) {
		t.Errorf("acting %q, printed %q", f.acting.Text("mayor"), f.out.String())
	}
}

// If the wait is never seen, nothing is handed over, and the error names the
// successor window and the command to run by hand.
func TestAHandoverWhenWaitingGivesUpAtTheLimitAndWritesNothing(t *testing.T) {
	f := aHandover(t)
	table := &lookingTable{from: 1 << 30, log: f.log}
	h, clock := f.waiting(table)
	start := clock.Now()
	at := uint64(2)

	_, err := h.Run(context.Background(), application.SeatHandoverRequest{At: &at, WhenWaiting: true, Limit: time.Minute})
	if err == nil {
		t.Fatal("a handover was made with no wait to hand over to")
	}
	for _, want := range []string{newMayorWindow, "mw seat handover --to " + newMayorWindow, "--at 2", "1m0s"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error %q does not say %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("the error %q is more than one line", err)
	}
	if clock.Now().Sub(start) < time.Minute || table.looks < 2 {
		t.Errorf("gave up after %v and %d looks", clock.Now().Sub(start), table.looks)
	}
	if len(f.log.All()) != 3 || f.acting.Text("mayor") != "" || f.out.Len() != 0 {
		t.Error("something was written")
	}
}

// The ceiling is checked before the process table is looked at.
func TestAHandoverWhenWaitingIsRefusedOverTheCeilingBeforeItLooksAtAnyProcess(t *testing.T) {
	f := aHandover(t)
	table := &lookingTable{from: 1, shows: []application.Process{aWaitOf(41, "mayor", "handover")}, log: f.log}
	h, _ := f.waiting(table)
	h.Ceiling = &fakeCeiling{err: &application.BootSetOver{Seat: "mayor", Bytes: 15001, Ceiling: 15000}}

	_, err := h.Run(context.Background(), application.SeatHandoverRequest{WhenWaiting: true, Limit: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "15001 bytes") {
		t.Fatalf("got %v, want the ceiling's refusal", err)
	}
	if table.looks != 0 {
		t.Errorf("looked at the process table %d times", table.looks)
	}
	if len(f.log.All()) != 3 || f.acting.Text("mayor") != "" {
		t.Error("something was written")
	}
}

// With no other window open it is refused at once, without waiting.
func TestAHandoverWhenWaitingWithNoSuccessorWindowIsRefusedAtOnce(t *testing.T) {
	f := aHandover(t)
	f.windows.Vanish("@5")
	table := &lookingTable{from: 1, log: f.log}
	h, _ := f.waiting(table)

	_, err := h.Run(context.Background(), application.SeatHandoverRequest{WhenWaiting: true, Limit: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "start the successor first") || table.looks != 0 {
		t.Errorf("got %v after %d looks", err, table.looks)
	}
}

// Only mw events wait for this seat with handover among its kinds counts.
func TestOnlyAnEventsWaitForTheSeatWithHandoverAmongItsKindsIsTheSuccessorsWait(t *testing.T) {
	for name, tc := range map[string]struct {
		argv []string
		want bool
	}{
		"for this seat with handover":        {[]string{"mw", "events", "wait", "--for", "mayor", "--kinds", "mail,handover"}, true},
		"for another seat":                   {[]string{"mw", "events", "wait", "--for", "deputy", "--kinds", "handover"}, false},
		"for this seat without handover":     {[]string{"mw", "events", "wait", "--for", "mayor", "--kinds", "mail,landing"}, false},
		"no kinds given":                     {[]string{"mw", "events", "wait", "--for", "mayor"}, false},
		"equals-sign flags":                  {[]string{"/bin/mw", "events", "wait", "--for=mayor", "--kinds=handover"}, true},
		"under timeout":                      {[]string{"timeout", "3000", "mw", "events", "wait", "--for", "mayor", "--kinds", "handover"}, true},
		"another command with the same word": {[]string{"mw", "events", "tail", "--for", "mayor", "--kinds", "handover"}, false},
		"a grep for it":                      {[]string{"pgrep", "-f", "events wait --for mayor --kinds handover"}, false},
		"the old window's own wait":          {[]string{"mw", "events", "wait", "--for", "mayor", "--kinds", "handover", "--as", oldMayorWindow}, false},
		"the successor's own wait":           {[]string{"mw", "events", "wait", "--for", "mayor", "--kinds", "handover", "--as", newMayorWindow}, true},
	} {
		f := aHandover(t)
		table := apptest.NewFakeUserProcesses()
		table.Start(application.Process{Pid: 7, Argv: tc.argv})
		h, _ := f.waiting(table)

		_, err := h.Run(context.Background(), application.SeatHandoverRequest{WhenWaiting: true, Limit: 10 * time.Second})
		if got := err == nil; got != tc.want {
			t.Errorf("%s: handed over %v (%v), want %v", name, got, err, tc.want)
		}
	}
}

// A process table that cannot be read is an error, not a hand over.
func TestAHandoverWhenWaitingFailsWhenTheProcessTableCannotBeRead(t *testing.T) {
	f := aHandover(t)
	table := apptest.NewFakeUserProcesses()
	table.Err = errors.New("no /proc")
	h, _ := f.waiting(table)

	_, err := h.Run(context.Background(), application.SeatHandoverRequest{WhenWaiting: true, Limit: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "no /proc") || len(f.log.All()) != 3 {
		t.Errorf("got %v", err)
	}
}
