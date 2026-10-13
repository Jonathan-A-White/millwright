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
)

// GateFile is the gate lock's name inside its directory.
const GateFile = "gate.lock"

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
// holding it, and the file says, as a courtesy, whose gate is running. It is
// the adapter behind application.GateLock.
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

// Take implements application.GateLock.
func (g *Gate) Take(ctx context.Context, holder string, held func(by string)) (application.GateTurn, error) {
	if err := os.MkdirAll(g.Dir, 0o755); err != nil {
		return application.GateTurn{}, fmt.Errorf("making the directory this host's gate lock belongs in: %w", err)
	}
	path := g.path()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return application.GateTurn{}, fmt.Errorf("opening this host's gate lock %s: %w", path, err)
	}

	started := g.now()
	var waitedFor string
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			file.Close()
			return application.GateTurn{}, fmt.Errorf("taking this host's gate lock %s: %w", path, err)
		}
		// The one who held it when the wait began is the one named: a queue
		// behind a long gate hands the lock on, and that first gate is the cause.
		if waitedFor == "" {
			waitedFor = gateHolder(path)
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
