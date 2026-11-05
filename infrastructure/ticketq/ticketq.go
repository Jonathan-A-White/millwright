// Package ticketq is a queue of tickets in a directory, so that waiters on an
// advisory flock take it first come, first served (mw-gq6.585, mw-gq6.586). A
// flock has no turn-taking of its own: whoever polls it first after it is let go
// takes it, and a waiter can be overtaken again and again. The gate lock and a
// rig's merge slot each keep a queue and let only the head try their flock.
package ticketq

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	kept bool
}

// Name is the ticket's file name, which orders the queue. Its first field is
// the time the wait began as the queue saw it: the owner's clock, raised past the
// newest ticket already queued (stamp), so that names order by join.
func (t *Ticket) Name() string { return t.name }

// seq tells apart the tickets one process makes in the same instant.
var seq atomic.Uint64

// Join takes a ticket for a wait that began at started. The ticket is made under
// a name the queue does not list, locked, and only then renamed into place, so
// that nobody finds it unlocked and takes it for a dead waiter's. holder, a
// courtesy, is written in it for whoever waits behind.
func (q *Queue) Join(holder string, started time.Time) (*Ticket, error) {
	return q.join(holder, "", started)
}

func (q *Queue) join(holder, key string, started time.Time) (*Ticket, error) {
	return q.joinNamed(holder, key, fmt.Sprintf("%020d-%010d-%d", q.stamp(started), seq.Add(1), os.Getpid()))
}

// stamp is the number a new ticket is named by: the time the wait began, in
// nanoseconds, but never at or below the newest ticket already in the queue, so
// that a waiter joining later is never ahead of one that joined earlier, whatever
// the clock that named them says (mw-gq6.976). A wall clock that steps back, or
// another host's clock that runs behind, would otherwise sort a later join first
// and let it pass the queue. The raise is a nanosecond past the newest name, so
// the age of a ticket against the cap is out by no more than the step. A name
// further ahead than the cap is not a clock but a stale ticket's, and is not
// followed; it would age the new ticket out of the cap's reach.
func (q *Queue) stamp(started time.Time) int64 {
	stamp := started.UnixNano()
	entries, err := os.ReadDir(q.Dir)
	if err != nil {
		return stamp
	}
	limit := q.Now().Add(q.Cap)
	var newest int64
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		when, ok := began(entry.Name())
		if !ok || !when.Before(limit) {
			continue
		}
		newest = max(newest, when.UnixNano())
	}
	if newest >= stamp {
		stamp = newest + 1
	}
	return stamp
}

func (q *Queue) joinNamed(holder, key, name string) (*Ticket, error) {
	if err := os.MkdirAll(q.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("making the ticket queue %s: %w", q.Dir, err)
	}
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
	if _, err := building.WriteString(ticketText(holder, key, time.Time{})); err != nil {
		return fail(err)
	}
	final := filepath.Join(q.Dir, name)
	if err := os.Rename(building.Name(), final); err != nil {
		return fail(err)
	}
	return &Ticket{path: final, name: name, file: building}, nil
}

// Drop gives the ticket back. A ticket that was kept (Keep) is not given back: it
// stands until its keep time passes or its key takes it up again.
func (t *Ticket) Drop() {
	if t.kept {
		return
	}
	os.Remove(t.path)
	t.file.Close()
}

// ticketText is what a ticket says: its holder, a courtesy for whoever waits
// behind, then the key its owner takes it up again by and, for a ticket kept
// after its owner, until when it is kept.
func ticketText(holder, key string, keepUntil time.Time) string {
	text := holder + "\n"
	if key != "" {
		text += keyWord + " " + key + "\n"
	}
	if !keepUntil.IsZero() {
		text += keepWord + " " + strconv.FormatInt(keepUntil.UnixNano(), 10) + "\n"
	}
	return text
}

const (
	keyWord  = "key"
	keepWord = "keep"
)

// ticketRead is what a ticket's file says.
type ticketRead struct {
	holder    string
	key       string
	keepUntil time.Time
}

func readTicket(file *os.File) ticketRead {
	var read ticketRead
	said, err := io.ReadAll(io.NewSectionReader(file, 0, 4096))
	if err != nil {
		return read
	}
	for i, line := range strings.Split(string(said), "\n") {
		line = strings.TrimSpace(line)
		word, rest, _ := strings.Cut(line, " ")
		switch {
		case i == 0:
			read.holder = line
		case word == keyWord:
			read.key = rest
		case word == keepWord:
			if nanos, err := strconv.ParseInt(rest, 10, 64); err == nil {
				read.keepUntil = time.Unix(0, nanos)
			}
		}
	}
	return read
}

// JoinAs is Join for a waiter that may come back for its place: key names the
// waiter (the same on every call that is the same wait), and a ticket an earlier
// call of that waiter kept (Keep), not yet past its keep time, is taken up again
// in place of a new one: the same name, so the same place in the queue, not the
// back of it.
func (q *Queue) JoinAs(key, holder string, started time.Time) (*Ticket, error) {
	if ticket := q.takeUp(key); ticket != nil {
		return ticket, nil
	}
	return q.join(holder, key, started)
}

// takeUp finds the ticket kept under key and holds it again, or returns nil.
func (q *Queue) takeUp(key string) *Ticket {
	entries, err := os.ReadDir(q.Dir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(q.Dir, entry.Name())
		if began, ok := began(entry.Name()); ok && q.Now().Sub(began) >= q.Cap {
			continue
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		// Looked at before it is locked, so that a waiter polling the queue (which
		// takes each unheld ticket's flock for a moment) is the only thing in the way
		// of a ticket that is ours, and then only for a moment.
		if read := readTicket(file); read.key != key || read.keepUntil.IsZero() {
			file.Close()
			continue
		}
		locked := false
		for try := 0; try < 40 && !locked; try++ {
			if locked = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil; !locked {
				time.Sleep(5 * time.Millisecond)
			}
		}
		if !locked {
			file.Close()
			continue
		}
		read := readTicket(file)
		if read.key != key || read.keepUntil.IsZero() || !q.Now().Before(read.keepUntil) {
			syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			file.Close()
			continue
		}
		// Held again, and no longer kept.
		if err := rewrite(file, ticketText(read.holder, key, time.Time{})); err != nil {
			syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			file.Close()
			continue
		}
		return &Ticket{path: path, name: entry.Name(), file: file}
	}
	return nil
}

// Keep ends the wait the ticket stands for without giving up its place: the
// ticket stays in the queue after its owner, and counts as live until, so that
// its owner, run again, takes it up again by its key (JoinAs). The queue walk
// treats it as it does a live waiter. A ticket made with Join has no key and is
// kept as an owner nobody can take up, which still holds its place until then.
func (t *Ticket) Keep(until time.Time) {
	if t.kept {
		return
	}
	read := readTicket(t.file)
	// Written before the flock goes, so that nobody who finds the ticket unheld
	// finds it unkept.
	if rewrite(t.file, ticketText(read.holder, read.key, until)) != nil {
		return
	}
	t.kept = true
	syscall.Flock(int(t.file.Fd()), syscall.LOCK_UN)
	t.file.Close()
}

// Leave puts a ticket back in the queue after its owner, for a waiter that won
// its turn, had its ticket (called name) dropped, and finds it cannot use the
// turn: a ticket of the same name, so the same place, under key, kept until
// until, for the waiter run again to take up (JoinAs). It says where the place
// stands among the live tickets.
func (q *Queue) Leave(name, key, holder string, until time.Time) (place, of int, err error) {
	ticket, err := q.joinNamed(holder, key, name)
	if err != nil {
		return 0, 0, err
	}
	place, of = q.Place(ticket.Name())
	ticket.Keep(until)
	return place, of, nil
}

// rewrite replaces what the file says.
func rewrite(file *os.File, text string) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err := file.WriteAt([]byte(text), 0)
	return err
}

// Place says where the ticket named mine stands among the live tickets: its
// place, counting from one, and how many live tickets there are in all, its own
// included. Tickets of waiters that are gone are cleared on the way, as Head does.
func (q *Queue) Place(mine string) (place, of int) {
	entries, err := os.ReadDir(q.Dir)
	if err != nil {
		return 1, 1
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		if name != mine && q.skipped(name) {
			continue
		}
		of++
		if name == mine {
			place = of
		}
	}
	if place == 0 {
		place, of = of+1, of+1
	}
	return place, of
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
		defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		// Nobody holds it, but its owner may have kept it to come back for it.
		if until := readTicket(file).keepUntil; !until.IsZero() && q.Now().Before(until) {
			return false
		}
		os.Remove(path)
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
