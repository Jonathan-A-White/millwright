// Package stampqueue keeps the chain stamps a landing queues and the ones the
// chain-stamp job has sent, as two files on this host: pending.jsonl and
// sent.jsonl in one directory. Host state, never the vault.
package stampqueue

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

var _ application.StampQueue = (*Queue)(nil)

const (
	pendingFile = "pending.jsonl"
	sentFile    = "sent.jsonl"
	lockFile    = "lock"
)

// Queue is the application.StampQueue kept in the directory dir: one JSON
// line per stamp. pending.jsonl is appended to by a landing and rewritten, a
// temporary file renamed over it, by the job; a flock on a file beside them
// keeps the two from losing one another's work. A stamp already in sent.jsonl
// is never reported pending, whatever pending.jsonl still says: a stamp that
// was sent and not yet taken off the queue is not sent twice.
type Queue struct{ dir string }

// New is the queue in dir, made when it is first written.
func New(dir string) *Queue { return &Queue{dir: dir} }

// Append implements application.StampQueue.
func (q *Queue) Append(_ context.Context, stamp domain.Stamp) error {
	return q.locked(func() error {
		line, err := json.Marshal(application.QueuedStamp{Stamp: stamp})
		if err != nil {
			return err
		}
		return appendLine(filepath.Join(q.dir, pendingFile), line)
	})
}

// Pending implements application.StampQueue.
func (q *Queue) Pending(_ context.Context) ([]application.QueuedStamp, error) {
	var pending []application.QueuedStamp
	err := q.locked(func() error {
		var err error
		pending, err = q.readPending()
		return err
	})
	return pending, err
}

// MarkFailed implements application.StampQueue.
func (q *Queue) MarkFailed(_ context.Context, stamp domain.Stamp, why string) error {
	return q.locked(func() error {
		pending, err := q.readPending()
		if err != nil {
			return err
		}
		for i := range pending {
			if pending[i].Stamp == stamp {
				pending[i].Attempts++
				pending[i].LastError = why
			}
		}
		return q.writePending(pending)
	})
}

// MarkSent implements application.StampQueue. The stamp is written to
// sent.jsonl first, so that a failure between the two files leaves it
// recorded as sent, never lost.
func (q *Queue) MarkSent(_ context.Context, stamp domain.Stamp, txid string, at time.Time) error {
	return q.locked(func() error {
		pending, err := q.readPending()
		if err != nil {
			return err
		}
		attempts := 1
		kept := pending[:0:0]
		for _, p := range pending {
			if p.Stamp == stamp {
				attempts = p.Attempts + 1
				continue
			}
			kept = append(kept, p)
		}
		line, err := json.Marshal(application.SentStamp{Stamp: stamp, Txid: txid, SentAt: at.UTC(), Attempts: attempts})
		if err != nil {
			return err
		}
		if err := appendLine(filepath.Join(q.dir, sentFile), line); err != nil {
			return err
		}
		return q.writePending(kept)
	})
}

// locked runs do holding the directory's lock, making the directory first.
func (q *Queue) locked(do func() error) error {
	if err := os.MkdirAll(q.dir, 0o700); err != nil {
		return fmt.Errorf("making %s: %w", q.dir, err)
	}
	lock, err := os.OpenFile(filepath.Join(q.dir, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("opening the stamp queue's lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking the stamp queue: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return do()
}

// readPending reads pending.jsonl without the stamps sent.jsonl holds. A line
// that is not JSON is skipped, not an error.
func (q *Queue) readPending() ([]application.QueuedStamp, error) {
	sent := map[domain.Stamp]bool{}
	if err := eachLine(filepath.Join(q.dir, sentFile), func(raw []byte) {
		var s application.SentStamp
		if json.Unmarshal(raw, &s) == nil {
			sent[s.Stamp] = true
		}
	}); err != nil {
		return nil, err
	}
	var pending []application.QueuedStamp
	err := eachLine(filepath.Join(q.dir, pendingFile), func(raw []byte) {
		var p application.QueuedStamp
		if json.Unmarshal(raw, &p) == nil && !sent[p.Stamp] {
			pending = append(pending, p)
		}
	})
	return pending, err
}

// writePending replaces pending.jsonl with pending, through a temporary file.
func (q *Queue) writePending(pending []application.QueuedStamp) error {
	path := filepath.Join(q.dir, pendingFile)
	tmp, err := os.CreateTemp(q.dir, pendingFile+".*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	for _, p := range pending {
		line, err := json.Marshal(p)
		if err != nil {
			tmp.Close()
			return err
		}
		if _, err := tmp.Write(append(line, '\n')); err != nil {
			tmp.Close()
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

func appendLine(path string, line []byte) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return file.Close()
}

// eachLine calls do with every line of path; a file that is not there has
// none.
func eachLine(path string, do func([]byte)) error {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		do(scanner.Bytes())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return nil
}
