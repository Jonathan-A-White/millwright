package hostlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// PassFile is the grist mill's pass lock, by its name inside the grist state
// directory: held for a whole `mw grist grind` pass, so two passes never
// answer the same grist.
const PassFile = "pass.lock"

// GrindFile is the name of grind slot n's lock inside the grist state
// directory. The mill takes one slot while each grind runs, up to its
// `[grist] concurrency` of them: the mill's own limit, which neither waits
// for the host's Builders nor takes a place in the cap `mw dispatch` keeps.
func GrindFile(n int) string { return fmt.Sprintf("grind-%d.lock", n) }

// GrindSlots is the locks of a host's grind slots, one for each grind the mill
// may run at once, the first slot first.
func GrindSlots(dir string, concurrency int) []application.GristLock {
	slots := make([]application.GristLock, 0, max(concurrency, 1))
	for n := range max(concurrency, 1) {
		slots = append(slots, NewTry(dir, GrindFile(n)))
	}
	return slots
}

// DispatchFile is the lock a whole `mw dispatch` run holds, by its name inside
// the dispatch state directory.
const DispatchFile = "lock"

// DefaultSettle is how long TryTake keeps trying before it says the lock is
// held: long enough to outlast a Held look taken at the same instant, short
// enough to be no wait at all beside a grind.
const DefaultSettle = 500 * time.Millisecond

// Try is a lock taken without waiting for its holder: a file in Dir, named
// Name, flocked.
type Try struct {
	Dir  string
	Name string
	// settle and poll are how long TryTake keeps trying, and how often.
	settle time.Duration
	poll   time.Duration
}

// Try satisfies the port.
var _ application.GristLock = (*Try)(nil)

// NewTry is the lock file name in dir.
func NewTry(dir, name string) *Try {
	return &Try{Dir: dir, Name: name, settle: DefaultSettle, poll: 25 * time.Millisecond}
}

func (t *Try) path() string { return filepath.Join(t.Dir, t.Name) }

// TryTake implements application.GristLock. A lock somebody holds is
// reported not taken, never waited for: only a moment's Held look is waited
// out.
//
// The lock lives only as long as release is reachable: the open file is held
// by release alone, and the garbage collector closes a file nothing refers to,
// which lets go of the lock. Keep release and always defer it.
func (t *Try) TryTake(ctx context.Context) (func(), bool, error) {
	if err := os.MkdirAll(t.Dir, 0o700); err != nil {
		return nil, false, fmt.Errorf("making the directory the lock %s belongs in: %w", t.path(), err)
	}
	file, err := os.OpenFile(t.path(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("opening the lock %s: %w", t.path(), err)
	}
	started := time.Now()
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				file.Close()
			}, true, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			file.Close()
			return nil, false, fmt.Errorf("taking the lock %s: %w", t.path(), err)
		}
		if time.Since(started) >= t.settle {
			file.Close()
			return nil, false, nil
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, false, fmt.Errorf("taking the lock %s: %w", t.path(), ctx.Err())
		case <-time.After(t.poll):
		}
	}
}

// Held implements application.GristLock: a shared flock tried for a moment
// and let go. A lock file that is not there is not held.
func (t *Try) Held(context.Context) (bool, error) {
	file, err := os.Open(t.path())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("opening the lock %s: %w", t.path(), err)
	}
	defer file.Close()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("looking at the lock %s: %w", t.path(), err)
	}
	syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false, nil
}
