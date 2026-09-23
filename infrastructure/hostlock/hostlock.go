// Package hostlock is the flock a host takes around its own beads sync, so
// that two syncs on one host — the dispatch tick, the Millhand's tick, its
// own sync, and the sync mw next runs after a landing — never both run the
// beads half at once. It is the adapter behind application.HostLock.
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

// File is the lock's name inside its directory.
const File = "sync.lock"

// DefaultWait is how long Take waits for the lock before giving up, when
// nothing says otherwise: long enough for an ordinary overlap — a tick and a
// session's own sync landing within the same few seconds — to clear, short
// enough that a sync stuck for good is said plainly rather than hung on
// forever.
const DefaultWait = 120 * time.Second

// DefaultPoll is how often Take looks again while it waits.
const DefaultPoll = 200 * time.Millisecond

// Lock is this host's sync lock, kept as a file in Dir.
type Lock struct {
	Dir  string
	wait time.Duration
	poll time.Duration
}

// Lock satisfies the port.
var _ application.HostLock = (*Lock)(nil)

// Option is a setting of a Lock, given to New.
type Option func(*Lock)

// WithWait sets how long Take waits for the lock before giving up.
func WithWait(wait time.Duration) Option { return func(l *Lock) { l.wait = wait } }

// WithPoll sets how often Take looks again while it waits.
func WithPoll(poll time.Duration) Option { return func(l *Lock) { l.poll = poll } }

// New is this host's sync lock, kept in dir.
func New(dir string, opts ...Option) *Lock {
	l := &Lock{Dir: dir, wait: DefaultWait, poll: DefaultPoll}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// path is where the lock file lives.
func (l *Lock) path() string { return filepath.Join(l.Dir, File) }

// Take implements application.HostLock.
func (l *Lock) Take(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("making the directory this host's sync lock belongs in: %w", err)
	}
	path := l.path()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening this host's sync lock %s: %w", path, err)
	}

	started := time.Now()
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				file.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			file.Close()
			return nil, fmt.Errorf("taking this host's sync lock %s: %w", path, err)
		}
		if waited := time.Since(started); waited >= l.wait {
			file.Close()
			return nil, fmt.Errorf("this host's sync lock %s is still held after %s: another sync is running long, or one is stuck",
				path, waited.Round(time.Second))
		}

		select {
		case <-ctx.Done():
			file.Close()
			return nil, fmt.Errorf("waiting for this host's sync lock %s: %w", path, ctx.Err())
		case <-time.After(l.poll):
		}
	}
}
