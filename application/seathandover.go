package application

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// MayorSeat is the seat the Governor talks to: the one a handover is of.
const MayorSeat = "mayor"

// ActingFile is the port a seat's acting file is written through. The live
// session writes it, never mw's automatic work: `mw seat handover` is the one
// command that does, for the session that is handing the seat over.
type ActingFile interface {
	// WriteActing replaces the seat's acting file (.<seat>-acting in the
	// vault) with text.
	WriteActing(ctx context.Context, seat, text string) error
}

// SeatHandover hands a seat from the session running it to a successor that is
// already up, with no gap between them (mw-6ww.55, Q7 A): the successor booted
// while the old session still answered, and this marks event N of the log, the
// last the old session answers. It emits a handover event, which ends the old
// session's waits ("handed over at N") and wakes the successor's, and writes
// the successor's name and N into the acting file, which is what the reaper
// the old window armed waits for before it closes the window.
type SeatHandover struct {
	Seat     string
	Log      EventLog
	Terminal ReapTerminal
	Acting   ActingFile
	Now      func() time.Time
	Out      io.Writer
	// Ceiling, when set, is asked before anything is written whether the seat's
	// boot set is within its manifest's ceiling: a handover is refused while the
	// successor's next boot would fail it (mw-gq6.1058).
	Ceiling BootCeiling
	// Procs and Sleep are what a WhenWaiting handover looks at the host's
	// processes and waits between looks with; Poll is the time between looks,
	// DefaultHandoverPoll when zero. Sleep nil is a real sleep.
	Procs UserProcesses
	Sleep func(ctx context.Context, d time.Duration) error
	Poll  time.Duration
}

// DefaultHandoverWaitLimit is how long a WhenWaiting handover waits for the
// successor's handover wait when the request names no limit.
const DefaultHandoverWaitLimit = 10 * time.Minute

// DefaultHandoverPoll is how often a WhenWaiting handover looks for it.
const DefaultHandoverPoll = 3 * time.Second

// SeatHandoverRequest is what one handover is asked to do. At is the seq of
// the last event the old session answers, the log's head when nil. Successor
// is the successor's window by name, the newest other window of the seat
// when empty.
//
// WhenWaiting makes the handover wait, before it writes anything, until a
// process of this user is `mw events wait` for the seat with handover among its
// kinds: the successor's wait, armed. Limit is how long it waits for it, zero
// DefaultHandoverWaitLimit; at the limit nothing is handed over.
type SeatHandoverRequest struct {
	At          *uint64
	Successor   string
	WhenWaiting bool
	Limit       time.Duration
}

// Run hands the seat over and returns the event it emitted.
func (h SeatHandover) Run(ctx context.Context, req SeatHandoverRequest) (events.Event, error) {
	if h.Seat == "" || h.Log == nil || h.Terminal == nil || h.Acting == nil || h.Now == nil {
		return events.Event{}, fmt.Errorf("handing a seat over needs a seat, its log, its terminal and its acting file")
	}
	head, err := h.Log.Head(ctx)
	if err != nil {
		return events.Event{}, err
	}
	at := head
	if req.At != nil {
		at = *req.At
	}
	if at > head {
		return events.Event{}, fmt.Errorf("the log has no event %d: its head is %d", at, head)
	}
	successor, err := h.successor(ctx, req.Successor)
	if err != nil {
		return events.Event{}, err
	}
	if h.Ceiling != nil {
		if err := h.Ceiling.CheckCeiling(ctx, h.Seat); err != nil {
			return events.Event{}, fmt.Errorf("the %s seat is not handed over, nothing is written: %w", h.Seat, err)
		}
	}
	if req.WhenWaiting {
		if successor, err = h.waitForSuccessor(ctx, req, successor); err != nil {
			return events.Event{}, err
		}
	}
	ev, err := EventEmit{
		Log: h.Log, Now: h.Now,
		Event: events.Event{Kind: events.KindHandover, Actor: h.Seat, Detail: events.Handover{Seat: h.Seat, Successor: successor, At: at}.Detail()},
	}.Run(ctx)
	if err != nil {
		return events.Event{}, err
	}
	if err := h.Acting.WriteActing(ctx, h.Seat, ActingText(h.Seat, successor, at, h.Now())); err != nil {
		return ev, fmt.Errorf("event %d says the %s seat is handed over at %d, but its acting file was not written: %w", ev.Seq, h.Seat, at, err)
	}
	if h.Out != nil {
		fmt.Fprintf(h.Out, "Handed over to %s at event %d (the handover is event %d). Answer nothing past event %d: "+
			"this is the last thing you run. Your window closes itself once it is idle, as bin/respawn-mayor armed it to.\n",
			successor, at, ev.Seq, at)
	}
	return ev, nil
}

// waitForSuccessor looks every Poll for the successor's handover wait among the
// host's processes, and returns the successor's window, found again, once it is
// there. At the limit it fails, in one line, with nothing written.
func (h SeatHandover) waitForSuccessor(ctx context.Context, req SeatHandoverRequest, successor string) (string, error) {
	if h.Procs == nil {
		return "", fmt.Errorf("waiting for the successor needs the host's process table")
	}
	limit, poll, sleep := req.Limit, h.Poll, h.Sleep
	if limit <= 0 {
		limit = DefaultHandoverWaitLimit
	}
	if poll <= 0 {
		poll = DefaultHandoverPoll
	}
	if sleep == nil {
		sleep = sleepContext
	}
	here, in, err := h.Terminal.ThisWindow(ctx)
	if err != nil {
		return "", err
	}
	self := ""
	if in {
		self = here.Name
	}
	deadline := h.Now().Add(limit)
	for {
		table, err := h.Procs.List(ctx)
		if err != nil {
			return "", fmt.Errorf("the processes of this host could not be looked at: %w", err)
		}
		for _, p := range table {
			if p.waitsForHandover(h.Seat, self) {
				return h.successor(ctx, req.Successor)
			}
		}
		if !h.Now().Before(deadline) {
			hand := "mw seat handover --to " + successor
			if req.At != nil {
				hand += fmt.Sprintf(" --at %d", *req.At)
			}
			return "", fmt.Errorf("the %s seat is not handed over, nothing is written: no mw events wait --for %s with handover among its kinds was running after %s, so %s is not ready; when it is, run: %s",
				h.Seat, h.Seat, limit, successor, hand)
		}
		if err := sleep(ctx, poll); err != nil {
			return "", err
		}
	}
}

// sleepContext waits d, or until ctx ends.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// waitsForHandover reports whether p is `mw events wait` for seat with handover
// among its --kinds: the wait a successor arms at boot. A wait named, with --as,
// for the window self is the old session's own, not the successor's.
func (p Process) waitsForHandover(seat, self string) bool {
	start := -1
	for i := 1; i+1 < len(p.Argv); i++ {
		if p.Argv[i] == "events" && p.Argv[i+1] == "wait" && filepath.Base(p.Argv[i-1]) == "mw" {
			start = i + 2
			break
		}
	}
	if start < 0 {
		return false
	}
	flag := func(name string) (string, bool) {
		for i := start; i < len(p.Argv); i++ {
			if v, ok := strings.CutPrefix(p.Argv[i], name+"="); ok {
				return v, true
			}
			if p.Argv[i] == name && i+1 < len(p.Argv) {
				return p.Argv[i+1], true
			}
		}
		return "", false
	}
	if target, _ := flag("--for"); target != seat {
		return false
	}
	if as, ok := flag("--as"); ok && self != "" && as == self {
		return false
	}
	kinds, _ := flag("--kinds")
	for _, k := range strings.Split(kinds, ",") {
		if strings.TrimSpace(k) == events.KindHandover {
			return true
		}
	}
	return false
}

// successor is the open window the seat is handed to: the one named, or the
// newest of the seat's other windows. It is never the window the command runs
// in.
func (h SeatHandover) successor(ctx context.Context, named string) (string, error) {
	windows, err := h.Terminal.OpenWindows(ctx)
	if err != nil {
		return "", err
	}
	here, in, err := h.Terminal.ThisWindow(ctx)
	if err != nil {
		return "", err
	}
	if named != "" {
		if in && here.Name == named {
			return "", fmt.Errorf("%s is this window, not the successor's: a handover is to the session that booted beside it", named)
		}
		for _, w := range windows {
			if w.Name == named {
				return named, nil
			}
		}
		return "", fmt.Errorf("no window named %s is open: start the successor first (bin/respawn-mayor for the Mayor)", named)
	}
	best, bestNumber := "", -1
	for _, w := range windows {
		number, ok := seatWindowNumber(h.Seat, w.Name)
		if !ok || (in && w.ID == here.ID) {
			continue
		}
		if number > bestNumber {
			best, bestNumber = w.Name, number
		}
	}
	if best == "" {
		return "", fmt.Errorf("no window of the %s seat is open besides this one: start the successor first (bin/respawn-mayor for the Mayor)", h.Seat)
	}
	return best, nil
}

// ActingText is what a handover writes into the seat's acting file: it names
// the successor's window, and so is the signal the reaper waits for, and
// carries the seq N the successor reads from. The old window is not named,
// so that the file names exactly one window.
func ActingText(seat, successor string, at uint64, now time.Time) string {
	return fmt.Sprintf("%s %s (handed over at event %d), since %s\n",
		strings.ToUpper(seat[:1])+seat[1:], successor, at, now.UTC().Format(time.RFC3339))
}

// HandedOverLine is what a wait of the old session prints when the seat was
// handed over: nothing past event N is its to answer.
func HandedOverLine(h events.Handover) string {
	return fmt.Sprintf("The %s seat is handed over at %d to %s: answer nothing after event %d, and stop.", h.Seat, h.At, h.Successor, h.At)
}

// HoldLine is what the successor's wait prints when the handover lands: the
// seat is its own, from event N.
func HoldLine(h events.Handover) string {
	return fmt.Sprintf("You hold the %s seat from event %d: the old session answered up to it. Read from there with mw events tail --since %d, "+
		"then arm your waits (mw events wait --for %s --since %d, mw talk wait).", h.Seat, h.At, h.At, h.Seat, h.At)
}

// HandoverWatch looks for a handover of one seat in the log, from the point of
// view of one window: a handover to that window is its own to take, one to any
// other, or when the window is not known, is the end of it.
type HandoverWatch struct {
	Log  EventLog
	Seat string
	// Self is the window's own name, empty when it is not known.
	Self string
}

// Ends reports the first handover of the seat among evs that ends this
// window's session: one whose successor is not Self.
func (w HandoverWatch) Ends(evs []events.Event) (events.Event, events.Handover, bool) {
	for _, ev := range evs {
		if h, ok := events.HandoverOf(ev); ok && h.Seat == w.Seat && (w.Self == "" || h.Successor != w.Self) {
			return ev, h, true
		}
	}
	return events.Event{}, events.Handover{}, false
}

// Since is the first handover after seq that ends this window's session,
// false when the log holds none.
func (w HandoverWatch) Since(ctx context.Context, seq uint64) (events.Handover, bool, error) {
	evs, err := w.Log.Since(ctx, seq)
	if err != nil {
		return events.Handover{}, false, err
	}
	_, h, ok := w.Ends(evs)
	return h, ok, nil
}
