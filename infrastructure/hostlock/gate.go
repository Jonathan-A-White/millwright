package hostlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// GateFile is the gate lock's name inside its directory.
const GateFile = "gate.lock"

// GateQueueDir is the directory beside the gate lock where each waiter keeps its ticket.
const GateQueueDir = "gate.queue"

// GateCap is the most a close-out waits for this host's gate lock. A rig's
// whole suite can take half an hour, and landings queue behind one another,
// so the cap is far past an ordinary queue; it is there so that a gate stuck
// for good is said, not waited on for ever.
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
// flock. It is the adapter behind application.GateLock.
type Gate struct {
	Dir string

	cap  time.Duration
	poll time.Duration
	now  func() time.Time
}

// Gate satisfies the port.
var _ application.GateLock = (*Gate)(nil)

// GateOption is a setting of a Gate, given to NewGate.
type GateOption func(*Gate)

// WithGateCap sets the most Take waits in all.
func WithGateCap(cap time.Duration) GateOption { return func(g *Gate) { g.cap = cap } }

// WithGatePoll sets how often Take looks again while it waits.
func WithGatePoll(poll time.Duration) GateOption { return func(g *Gate) { g.poll = poll } }

// NewGate is this host's gate lock, kept in dir.
func NewGate(dir string, opts ...GateOption) *Gate {
	g := &Gate{Dir: dir, cap: GateCap, poll: GatePoll, now: time.Now}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *Gate) path() string { return filepath.Join(g.Dir, GateFile) }

// ticketSeq tells apart the tickets one process makes in the same instant.
var ticketSeq atomic.Uint64

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
	ticket, err := g.takeTicket(queue, holder, started)
	if err != nil {
		file.Close()
		return application.GateTurn{}, err
	}
	// Whatever way Take ends, the ticket goes: the lock is taken, or the wait is over.
	defer ticket.drop()

	var waitedFor string
	for {
		head, headHolder := g.head(queue, ticket.name)
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
		if waited := g.now().Sub(started); waited >= g.cap {
			file.Close()
			return application.GateTurn{}, fmt.Errorf("this host's gate lock %s is still held by %q after %s: another gate is running long, or one is stuck",
				path, gateHolder(path), waited.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			file.Close()
			return application.GateTurn{}, fmt.Errorf("waiting for this host's gate lock %s: %w", path, ctx.Err())
		case <-time.After(g.poll):
		}
	}

	if err := writeGate(file, holder); err != nil {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
		return application.GateTurn{}, fmt.Errorf("writing whose gate holds this host's gate lock: %w", err)
	}
	return application.GateTurn{
		Holding: &gateHolding{file: file, holder: holder},
		Waited:  g.now().Sub(started),
		For:     waitedFor,
	}, nil
}

// gateTicket is a waiter's place in the queue: a file in the queue directory
// named by when its wait began, flocked by its owner so that a dead owner shows
// as a ticket nobody holds.
type gateTicket struct {
	path string
	name string
	file *os.File
}

// takeTicket joins the queue. The ticket is made under a name the queue does not
// list, locked, and only then renamed into place, so that nobody finds it
// unlocked and takes it for a dead waiter's.
func (g *Gate) takeTicket(queue, holder string, started time.Time) (*gateTicket, error) {
	name := fmt.Sprintf("%020d-%010d-%d", started.UnixNano(), ticketSeq.Add(1), os.Getpid())
	building, err := os.CreateTemp(queue, ".ticket-")
	if err != nil {
		return nil, fmt.Errorf("making a ticket in this host's gate queue %s: %w", queue, err)
	}
	fail := func(err error) (*gateTicket, error) {
		building.Close()
		os.Remove(building.Name())
		return nil, fmt.Errorf("making a ticket in this host's gate queue %s: %w", queue, err)
	}
	if err := syscall.Flock(int(building.Fd()), syscall.LOCK_EX); err != nil {
		return fail(err)
	}
	if _, err := building.WriteString(holder + "\n"); err != nil {
		return fail(err)
	}
	final := filepath.Join(queue, name)
	if err := os.Rename(building.Name(), final); err != nil {
		return fail(err)
	}
	return &gateTicket{path: final, name: name, file: building}, nil
}

// drop gives the ticket back.
func (t *gateTicket) drop() {
	os.Remove(t.path)
	t.file.Close()
}

// head says whether the ticket named mine is the first live one in the queue,
// and whose ticket is first. Tickets of waiters that are gone, or older than the
// cap, are skipped and cleared on the way.
func (g *Gate) head(queue, mine string) (bool, string) {
	entries, err := os.ReadDir(queue)
	if err != nil {
		return true, "somebody" // no queue to read: wait on the flock alone
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			g.clearUnfinished(filepath.Join(queue, entry.Name()))
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		if name == mine {
			return true, "somebody"
		}
		if g.skipped(queue, name) {
			continue
		}
		return false, gateHolder(filepath.Join(queue, name))
	}
	// Mine is not listed (cleared from under me): nothing stands before it.
	return true, "somebody"
}

// skipped says whether the ticket called name no longer counts, and removes it
// when it does not: its owner is gone, which is to say nobody holds its flock,
// or its wait began longer ago than the cap, so its owner has given up.
func (g *Gate) skipped(queue, name string) bool {
	path := filepath.Join(queue, name)
	if began, ok := ticketBegan(name); ok && g.now().Sub(began) >= g.cap {
		os.Remove(path)
		return true
	}
	file, err := os.Open(path)
	if err != nil {
		return true // gone already
	}
	defer file.Close()
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
		os.Remove(path)
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return true
	}
	return false
}

// clearUnfinished removes a ticket that was being made when its maker died: far
// older than the moment it takes to make one, and held by nobody.
func (g *Gate) clearUnfinished(path string) {
	info, err := os.Stat(path)
	if err != nil || g.now().Sub(info.ModTime()) < time.Minute {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
		os.Remove(path)
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}
}

// ticketBegan is when the wait a ticket stands for began, read from its name.
func ticketBegan(name string) (time.Time, bool) {
	var nanos int64
	if _, err := fmt.Sscanf(name, "%d-", &nanos); err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, nanos), true
}

// gateHolding is a gate lock somebody has: the open file whose flock it is.
type gateHolding struct {
	file   *os.File
	holder string
	gone   bool
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
