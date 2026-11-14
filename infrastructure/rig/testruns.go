package rig

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// TestRunsPoll is how often a start that waits for a test-runner slot looks
// again, and TestRunsNoticeEvery how often a wait that goes on says again who it
// waits for.
const (
	TestRunsPoll        = 2 * time.Second
	TestRunsNoticeEvery = time.Minute
)

// TestRunsPrefix names a slot's lock file inside the slots' directory.
const TestRunsPrefix = "test-run-"

// TestRuns is this host's limit on how many test runners mw has going at once
// (builder_test_runs, mw-gq6.1303): a gate's part, an after-landing command and a
// Builder's own test run through mw each take one of its slots before they start
// the runner, and give it back when it ends. builder_jobs caps the workers inside
// one run; this caps the runs, which the gate lock does not (it holds only the
// gate).
//
// A slot is an advisory flock on a file in Dir, as the merge slot and the gate
// lock are, so the kernel gives it back for a mw that dies holding it. The file
// says, as a courtesy, who holds it and since when, so a start that has to wait
// can name what it waits for.
//
// A nil *TestRuns, or one whose limit is 0 or less, is no limit: Take returns at
// once. That is every host that has not set builder_test_runs.
type TestRuns struct {
	dir   string
	limit int
	poll  time.Duration
	every time.Duration
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
	// notice, when set, is told each line a wait says, besides the progress writer
	// of the context the start was asked with.
	notice func(said string)
}

// TestRunsOption is a setting of a TestRuns, given to NewTestRuns.
type TestRunsOption func(*TestRuns)

// WithTestRunsClock sets the clock a wait is kept by: what time it is, and how a
// wait of one poll is passed (it returns early with the context's error when the
// context gives up). It is for a test that cannot wait for a real run to end.
func WithTestRunsClock(now func() time.Time, sleep func(ctx context.Context, d time.Duration) error) TestRunsOption {
	return func(t *TestRuns) { t.now, t.sleep = now, sleep }
}

// WithTestRunsNotice has each line a wait says given to say as well, so that a
// start nobody gave a progress writer is not a silent one.
func WithTestRunsNotice(say func(string)) TestRunsOption {
	return func(t *TestRuns) { t.notice = say }
}

// NewTestRuns is the limit of limit test runners at once, its slots kept in dir.
func NewTestRuns(dir string, limit int, opts ...TestRunsOption) *TestRuns {
	t := &TestRuns{dir: dir, limit: limit, poll: TestRunsPoll, every: TestRunsNoticeEvery, now: time.Now, sleep: sleepFor}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *TestRuns) path(n int) string {
	return filepath.Join(t.dir, fmt.Sprintf("%s%d.lock", TestRunsPrefix, n))
}

// Take waits until one of the host's slots is free and takes it for holder, and
// returns what gives it back. A wait names the holders of the slots in the words
// the gate lock's wait does: who, since when and how long ago. It ends with the
// context's error when the context does, taking nothing.
//
// The wait is not counted against any time of the run's own: a caller takes the
// slot before it starts its limit.
func (t *TestRuns) Take(ctx context.Context, holder string) (func(), error) {
	if t == nil || t.limit <= 0 {
		return func() {}, nil
	}
	if err := os.MkdirAll(t.dir, 0o755); err != nil {
		return nil, fmt.Errorf("making the directory this host's test-runner slots belong in: %w", err)
	}
	var began, lastSaid time.Time
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("waiting for one of this host's %d test-runner slots: %w", t.limit, err)
		}
		release, err := t.tryEach(holder)
		if err != nil {
			return nil, err
		}
		if release != nil {
			return release, nil
		}
		now := t.now()
		if began.IsZero() {
			began = now
		}
		if lastSaid.IsZero() || now.Sub(lastSaid) >= t.every {
			lastSaid = now
			t.say(ctx, t.heldLine(now, began, holder))
		}
		if err := t.sleep(ctx, t.poll); err != nil {
			return nil, fmt.Errorf("waiting for one of this host's %d test-runner slots: %w", t.limit, err)
		}
	}
}

// tryEach takes the first free slot, nil when all of them are held.
func (t *TestRuns) tryEach(holder string) (func(), error) {
	for n := 0; n < t.limit; n++ {
		path := t.path(n)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return nil, fmt.Errorf("opening this host's test-runner slot %s: %w", path, err)
		}
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			file.Close()
			if err == syscall.EWOULDBLOCK {
				continue
			}
			return nil, fmt.Errorf("taking this host's test-runner slot %s: %w", path, err)
		}
		if err := writeSlot(file, fmt.Sprintf("%s\n%s\n", holder, t.now().UTC().Format(time.RFC3339))); err != nil {
			syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			file.Close()
			return nil, fmt.Errorf("writing whose run holds this host's test-runner slot %s: %w", path, err)
		}
		var done bool
		return func() {
			if done {
				return
			}
			done = true
			// Emptied first, so that a slot nobody has does not name its last holder.
			_ = writeSlot(file, "")
			syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			file.Close()
		}, nil
	}
	return nil, nil
}

func writeSlot(file *os.File, text string) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if text == "" {
		return nil
	}
	_, err := file.WriteString(text)
	return err
}

// slotHolder is who holds slot n and since when, read without taking it:
// "somebody" and the zero time when its file says nobody (a holder that has only
// just taken it has not written yet).
func (t *TestRuns) slotHolder(n int) (holder string, since time.Time) {
	said, err := os.ReadFile(t.path(n))
	if err != nil {
		return "somebody", time.Time{}
	}
	lines := strings.Split(string(said), "\n")
	if first := strings.TrimSpace(lines[0]); first != "" {
		holder = first
	} else {
		holder = "somebody"
	}
	if len(lines) > 1 {
		since, _ = time.Parse(time.RFC3339, strings.TrimSpace(lines[1]))
	}
	return holder, since
}

// heldLine is the line a wait says: who holds each slot, since when and for how
// long, and what this run does next. The words are the gate lock's own.
func (t *TestRuns) heldLine(now, began time.Time, holder string) string {
	var held []string
	for n := 0; n < t.limit; n++ {
		by, since := t.slotHolder(n)
		if since.IsZero() {
			held = append(held, "held by "+by)
			continue
		}
		held = append(held, fmt.Sprintf("held by %s, since %s (%s ago)", by, since.UTC().Format("15:04:05Z"), now.Sub(since).Round(time.Second)))
	}
	return fmt.Sprintf("this host's %d test-runner slots (builder_test_runs) are all taken: %s; %s has waited %s and starts when one ends",
		t.limit, strings.Join(held, "; "), holder, now.Sub(began).Round(time.Second))
}

// say gives a line to the progress writer of ctx and to the notice, if there are any.
func (t *TestRuns) say(ctx context.Context, line string) {
	if progress := application.RunProgressOf(ctx); progress != nil {
		_, _ = io.WriteString(progress, "  wait    "+line+"\n")
	}
	if t.notice != nil {
		t.notice(line)
	}
}
