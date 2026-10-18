// Package ticketq is a queue of tickets in a directory, so that waiters on an
// advisory flock take it first come, first served (mw-gq6.585, mw-gq6.586). A
// flock has no turn-taking of its own: whoever polls it first after it is let go
// takes it, and a waiter can be overtaken again and again. The gate lock and a
// rig's merge slot each keep a queue and let only the head try their flock.
package ticketq

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Queue is the directory where waiters keep their tickets. A ticket is a file
// named by when its owner's wait began, flocked by the owner for as long as it
// waits, so that a dead owner shows as a ticket nobody holds.
type Queue struct {
	Dir string
	// Cap is how long a wait counts: a ticket whose wait began longer ago has
	// been given up on by its owner, and is skipped.
	Cap time.Duration
	// Now is the clock the age of a ticket is judged by.
	Now func() time.Time
}

// Ticket is a waiter's place in the queue.
type Ticket struct {
	path string
	name string
	file *os.File
}

// Name is the ticket's file name, which orders the queue.
func (t *Ticket) Name() string { return t.name }

// seq tells apart the tickets one process makes in the same instant.
var seq atomic.Uint64

// Join takes a ticket for a wait that began at started. The ticket is made under
// a name the queue does not list, locked, and only then renamed into place, so
// that nobody finds it unlocked and takes it for a dead waiter's. holder, a
// courtesy, is written in it for whoever waits behind.
func (q *Queue) Join(holder string, started time.Time) (*Ticket, error) {
	if err := os.MkdirAll(q.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("making the ticket queue %s: %w", q.Dir, err)
	}
	name := fmt.Sprintf("%020d-%010d-%d", started.UnixNano(), seq.Add(1), os.Getpid())
	building, err := os.CreateTemp(q.Dir, ".ticket-")
	if err != nil {
		return nil, fmt.Errorf("making a ticket in the queue %s: %w", q.Dir, err)
	}
	fail := func(err error) (*Ticket, error) {
		building.Close()
		os.Remove(building.Name())
		return nil, fmt.Errorf("making a ticket in the queue %s: %w", q.Dir, err)
	}
	if err := syscall.Flock(int(building.Fd()), syscall.LOCK_EX); err != nil {
		return fail(err)
	}
	if _, err := building.WriteString(holder + "\n"); err != nil {
		return fail(err)
	}
	final := filepath.Join(q.Dir, name)
	if err := os.Rename(building.Name(), final); err != nil {
		return fail(err)
	}
	return &Ticket{path: final, name: name, file: building}, nil
}

// Drop gives the ticket back.
func (t *Ticket) Drop() {
	os.Remove(t.path)
	t.file.Close()
}

// Head says whether the ticket named mine is the first live one in the queue, and
// whose ticket is first (or "somebody" when it does not say). Tickets of waiters
// that are gone, or older than the cap, are skipped and cleared on the way.
func (q *Queue) Head(mine string) (bool, string) {
	entries, err := os.ReadDir(q.Dir)
	if err != nil {
		return true, "somebody" // no queue to read: wait on the flock alone
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			q.clearUnfinished(filepath.Join(q.Dir, entry.Name()))
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		if name == mine {
			return true, "somebody"
		}
		if q.skipped(name) {
			continue
		}
		return false, ticketHolder(filepath.Join(q.Dir, name))
	}
	// Mine is not listed (cleared from under me): nothing stands before it.
	return true, "somebody"
}

// skipped says whether the ticket called name no longer counts, and removes it
// when it does not: its owner is gone, which is to say nobody holds its flock,
// or its wait began longer ago than the cap, so its owner has given up.
func (q *Queue) skipped(name string) bool {
	path := filepath.Join(q.Dir, name)
	if began, ok := began(name); ok && q.Now().Sub(began) >= q.Cap {
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
func (q *Queue) clearUnfinished(path string) {
	info, err := os.Stat(path)
	if err != nil || q.Now().Sub(info.ModTime()) < time.Minute {
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

// began is when the wait a ticket stands for began, read from its name.
func began(name string) (time.Time, bool) {
	var nanos int64
	if _, err := fmt.Sscanf(name, "%d-", &nanos); err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, nanos), true
}

// ticketHolder is whose wait a ticket stands for, as the ticket says.
func ticketHolder(path string) string {
	said, err := os.ReadFile(path)
	if err == nil {
		if first := strings.TrimSpace(strings.SplitN(string(said), "\n", 2)[0]); first != "" {
			return first
		}
	}
	return "somebody"
}
