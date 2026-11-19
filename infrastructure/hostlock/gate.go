package hostlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/ticketq"
)

// GateFile is the gate lock's name inside its directory.
const GateFile = "gate.lock"

// GateQueueDir is the directory beside the gate lock where each waiter keeps its ticket.
const GateQueueDir = "gate.queue"

// GateCap is the most a close-out waits for this host's gate lock. A rig's
// whole suite can take half an hour, and landings queue behind one another,
// so the cap is far past an ordinary queue; it is there so that a gate stuck
// for good is said, not waited on for ever. What the host gave way in the wait
// is not counted against it, for a Gate given the host's give-way record
// (WithGateGaveWay, mw-gq6.1478).
const GateCap = 3 * time.Hour

// GatePoll is how often a wait for the gate lock looks again.
const GatePoll = 2 * time.Second

// Gate is this host's gate lock: the right to run a rig's tests, which only
// one close-out on the host holds at a time (mw-gq6.388). It is an advisory
// flock on a file in Dir, so the kernel gives it back for a mw that dies
// holding it, and the file says, as a courtesy, whose gate is running. A flock
// has no turn-taking of its own, so waiters queue by ticket (mw-gq6.585): each
// keeps a ticket file, named by when its wait began, in GateQueueDir, locked
// for as long as its owner lives, and only the oldest live ticket may take the
// flock; a Take asked under application.WithQueuePrecedence, a batch's, keeps a
// ticket ahead of every other kind (mw-gq6.1450). It is the adapter behind
// application.GateLock.
type Gate struct {
	Dir string

	cap     time.Duration
	poll    time.Duration
	now     func() time.Time
	gaveWay application.GiveWaySpans
}

// Gate satisfies the port.
var _ application.GateLock = (*Gate)(nil)

// GateOption is a setting of a Gate, given to NewGate.
type GateOption func(*Gate)

// WithGateCap sets the most Take waits in all.
func WithGateCap(cap time.Duration) GateOption { return func(g *Gate) { g.cap = cap } }

// WithGatePoll sets how often Take looks again while it waits.
func WithGatePoll(poll time.Duration) GateOption { return func(g *Gate) { g.poll = poll } }

// WithGateClock sets the clock the age and keep time of the tickets in the queue are
// judged by, and how long Take has waited, for a test that cannot wait ten minutes.
func WithGateClock(now func() time.Time) GateOption { return func(g *Gate) { g.now = now } }

// WithGateGaveWay gives the Gate the host's give-way record: what the host gave
// way while Take waits is not counted against the cap, for the give-way stopped
// the waiter and it waited for nobody then (mw-gq6.1478, as mw-gq6.1458 for a
// check's own waits). Without it every span counts.
func WithGateGaveWay(spans application.GiveWaySpans) GateOption {
	return func(g *Gate) { g.gaveWay = spans }
}

// NewGate is this host's gate lock, kept in dir.
func NewGate(dir string, opts ...GateOption) *Gate {
	g := &Gate{Dir: dir, cap: GateCap, poll: GatePoll, now: time.Now}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *Gate) path() string { return filepath.Join(g.Dir, GateFile) }

// waited is how much of the cap a wait begun at started has spent: the time
// since, less what the host gave way since (mw-gq6.1478). The give-way record
// is read only once the time since has reached the cap, so an ordinary wait
// reads no file on each poll.
func (g *Gate) waited(ctx context.Context, started time.Time) time.Duration {
	now := g.now()
	since := now.Sub(started)
	if since < g.cap || g.gaveWay == nil {
		return since
	}
	return since - min(g.gaveWay.GaveWayBetween(ctx, started, now), since)
}

// Take implements application.GateLock.
func (g *Gate) Take(ctx context.Context, holder string, held func(by string)) (application.GateTurn, error) {
	queue := filepath.Join(g.Dir, GateQueueDir)
	if err := os.MkdirAll(queue, 0o755); err != nil {
		return application.GateTurn{}, fmt.Errorf("making the directory this host's gate lock belongs in: %w", err)
	}
	path := g.path()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return application.GateTurn{}, fmt.Errorf("opening this host's gate lock %s: %w", path, err)
	}

	started := g.now()
	tickets := &ticketq.Queue{Dir: queue, Cap: g.cap, Now: g.now}
	// A waiter that asks it comes back for its place: a ticket kept for it by an
	// earlier call is its own again, and one left at the end of this wait is kept.
	asked := application.QueuePlaceOf(ctx)
	var ticket *ticketq.Ticket
	switch {
	case asked != nil && asked.Key != "":
		ticket, err = tickets.JoinAs(asked.Key, holder, started)
	case application.HasQueuePrecedence(ctx):
		// A batch goes ahead of every single story's check and close-out (mw-gq6.1450).
		ticket, err = tickets.JoinFirst(holder, started)
	default:
		ticket, err = tickets.Join(holder, started)
	}
	if err != nil {
		file.Close()
		return application.GateTurn{}, err
	}
	// Whatever way Take ends, the ticket goes: the lock is taken, or the wait is over.
	defer ticket.Drop()

	var waitedFor string
	for {
		head, headHolder := tickets.Head(ticket.Name())
		var err error
		if head {
			err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				break
			}
			if !errors.Is(err, syscall.EWOULDBLOCK) {
				file.Close()
				return application.GateTurn{}, fmt.Errorf("taking this host's gate lock %s: %w", path, err)
			}
		}
		// The one who held it when the wait began is the one named: a queue
		// behind a long gate hands the lock on, and that first gate is the cause.
		// A waiter behind another waiter names that one while the lock is free.
		if waitedFor == "" {
			if head {
				waitedFor = gateHolder(path)
			} else if by := gateHolder(path); by != "somebody" {
				waitedFor = by
			} else {
				waitedFor = headHolder
			}
			if held != nil {
				held(waitedFor)
			}
		}
		if waited := g.waited(ctx, started); waited >= g.cap {
			file.Close()
			return application.GateTurn{}, fmt.Errorf("this host's gate lock %s is still held by %q after %s: another gate is running long, or one is stuck",
				path, gateHolder(path), waited.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			file.Close()
			if asked != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				place, of := tickets.Place(ticket.Name())
				ticket.Keep(g.now().Add(asked.For))
				asked.Kept(place, of)
			}
			return application.GateTurn{}, fmt.Errorf("waiting for this host's gate lock %s: %w", path, ctx.Err())
		case <-time.After(g.poll):
		}
	}

	// Won, but the waiter would not use it now: give it back and keep the place.
	if asked != nil && asked.Late != nil && asked.Late() {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
		place, of := tickets.Place(ticket.Name())
		ticket.Keep(g.now().Add(asked.For))
		asked.Kept(place, of)
		return application.GateTurn{}, application.ErrPlaceKeptLate
	}

	if err := writeGate(file, holder); err != nil {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
		return application.GateTurn{}, fmt.Errorf("writing whose gate holds this host's gate lock: %w", err)
	}
	return application.GateTurn{
		Holding: &gateHolding{file: file, holder: holder, tickets: tickets, ticket: ticket.Name(), key: askedKey(asked), now: g.now},
		Waited:  g.now().Sub(started),
		For:     waitedFor,
	}, nil
}

// Queued says whose wait each live ticket in the gate queue stands for, oldest
// first, read without taking the lock or a place in the queue (mw-gq6.1510). It
// is what tells a gate that can wait, the nightly suite, whether a close-out is
// waiting for the lock.
func (g *Gate) Queued() []string {
	tickets := &ticketq.Queue{Dir: filepath.Join(g.Dir, GateQueueDir), Cap: g.cap, Now: g.now}
	return tickets.Holders()
}

// Holder says whose gate holds the lock now, read without taking it: empty when
// nobody holds it, even if a holder that died left its name in the file, and
// "somebody" when it is held and the file names no one (mw-gq6.858).
func (g *Gate) Holder() string {
	file, err := os.Open(g.path())
	if err != nil {
		return ""
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return ""
	}
	return gateHolder(g.path())
}

// HeldSince says when the lock was taken by whoever holds it now, read without
// taking it: the time the file was last written, which is when its holder
// named itself in it. It is zero when nobody holds the lock or the time cannot
// be read (mw-gq6.906).
func (g *Gate) HeldSince() time.Time {
	if g.Holder() == "" {
		return time.Time{}
	}
	info, err := os.Stat(g.path())
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// gateHolding is a gate lock somebody has: the open file whose flock it is.
type gateHolding struct {
	file   *os.File
	holder string
	gone   bool

	// What puts the waiter's place back in its queue (KeepPlace): the queue, the
	// key the waiter asked to be known by, and the name of the ticket it won with.
	tickets *ticketq.Queue
	ticket  string
	key     string
	now     func() time.Time
}

func askedKey(asked *application.QueuePlace) string {
	if asked == nil {
		return ""
	}
	return asked.Key
}

// KeepPlace puts the waiter's place back in the queue, kept for the time given
// (application.PlaceKeeper): the lock was won, its ticket dropped, and the
// waiter finds it cannot use the turn after all (mw-gq6.945). It says where the
// place stands; ok is false when the waiter did not ask for a place to be kept
// or the queue could not keep it.
func (h *gateHolding) KeepPlace(kept time.Duration) (place, of int, ok bool) {
	if h.key == "" || h.tickets == nil {
		return 0, 0, false
	}
	place, of, err := h.tickets.Leave(h.ticket, h.key, h.holder, h.now().Add(kept))
	return place, of, err == nil
}

// Waiting implements application.LockWaiters: how many live tickets are queued
// for the lock behind its holder (mw-gq6.1319). The holder's own ticket was
// dropped when it took the lock, so Place finds it absent and counts one past
// the others.
func (h *gateHolding) Waiting() int {
	if h.tickets == nil || h.gone {
		return 0
	}
	_, of := h.tickets.Place(h.ticket)
	return of - 1
}

// Release implements application.Holding. Releasing twice is harmless.
func (h *gateHolding) Release(context.Context) error {
	if h.gone {
		return nil
	}
	h.gone = true
	// Emptied first, so that a lock nobody has does not name its last holder.
	writeErr := writeGate(h.file, "")
	unlockErr := syscall.Flock(int(h.file.Fd()), syscall.LOCK_UN)
	closeErr := h.file.Close()
	return errors.Join(writeErr, unlockErr, closeErr)
}

// HeldBy implements application.Holding.
func (h *gateHolding) HeldBy() string { return h.holder }

func writeGate(file *os.File, text string) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if text == "" {
		return nil
	}
	_, err := file.WriteString(text + "\n")
	return err
}

// gateHolder is whose gate the lock file says is running, read without taking
// anything; "somebody" when it says nobody, as a holder that has only just
// taken the lock has not written yet.
func gateHolder(path string) string {
	said, err := os.ReadFile(path)
	if err == nil {
		if first := strings.TrimSpace(strings.SplitN(string(said), "\n", 2)[0]); first != "" {
			return first
		}
	}
	return "somebody"
}
