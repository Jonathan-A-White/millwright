// Package eventlog keeps the home's event log: one JSON event per line in
// one file, numbered by a .seq sidecar beside it, every append flocked and
// fsynced. It is the adapter behind application.EventLog, and keeps the
// follower's cursor beside the log (application.FollowCursors).
package eventlog

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// Log is one home's event log, in the file Path.
type Log struct {
	Path string
}

// Log satisfies the port.
var _ application.EventLog = (*Log)(nil)

// New is the log in the file path; its directory is made on the first
// append.
func New(path string) *Log { return &Log{Path: path} }

// seqPath is the sidecar beside the log that holds its head: log.jsonl's is
// log.seq.
func (l *Log) seqPath() string {
	return strings.TrimSuffix(l.Path, filepath.Ext(l.Path)) + ".seq"
}

// MaxEventLine is the most bytes one event's encoded line may take: Append
// refuses a longer one, so no line grows past what a reader can hold.
const MaxEventLine = 1 << 20

// tailChunk is how much of the log's end lastWhole reads at a time; it reads
// further back, chunk by chunk, for a line longer than that.
const tailChunk = 64 << 10

// lockPath is the file whose flock serializes the writers of the log: log.jsonl's
// is log.lock. It is a file of its own, not the log, because a trim replaces
// the log by renaming a new file over it.
func (l *Log) lockPath() string {
	return strings.TrimSuffix(l.Path, filepath.Ext(l.Path)) + ".lock"
}

// lock takes the exclusive flock on the lock file, making its directory first.
func (l *Log) lock() (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return nil, fmt.Errorf("making the event log's directory: %w", err)
	}
	f, err := os.OpenFile(l.lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the event log's lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("locking the event log: %w", err)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Append implements application.EventLog. Under an exclusive flock on the
// log's lock file, it cuts a torn last line (a crash mid-write), takes the head as the
// greater of the sidecar and the last line's seq (a crash between the log's
// fsync and the sidecar's), writes the batch in one write, fsyncs, then
// writes the sidecar.
func (l *Log) Append(_ context.Context, evs []events.Event) (uint64, error) {
	if len(evs) == 0 {
		return l.head()
	}
	unlock, err := l.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, fmt.Errorf("opening the event log: %w", err)
	}
	defer f.Close()

	end, last, err := lastWhole(f)
	if err != nil {
		return 0, err
	}
	head, err := l.sidecar()
	if err != nil {
		return 0, err
	}
	if last > head {
		head = last
	}
	var buf bytes.Buffer
	for _, e := range evs {
		head++
		e.Seq = head
		if err := e.Validate(); err != nil {
			return 0, err
		}
		line, err := json.Marshal(e)
		if err != nil {
			return 0, err
		}
		if len(line) > MaxEventLine {
			return 0, fmt.Errorf("the %s event for %q is %d bytes encoded; an event is at most %d, so it was not written (put the long text in a file and name it)", e.Kind, e.Bead, len(line), MaxEventLine)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := f.Truncate(end); err != nil {
		return 0, fmt.Errorf("cutting a torn line off the event log: %w", err)
	}
	if _, err := f.WriteAt(buf.Bytes(), end); err != nil {
		return 0, fmt.Errorf("writing the event log: %w", err)
	}
	if err := f.Sync(); err != nil {
		return 0, fmt.Errorf("syncing the event log: %w", err)
	}
	if err := writeSynced(l.seqPath(), []byte(strconv.FormatUint(head, 10)+"\n")); err != nil {
		return 0, fmt.Errorf("writing the event log's head: %w", err)
	}
	return head, nil
}

// lastWhole is where the log's last whole line ends, and that line's seq (0
// for an empty log). It reads the log backward from its end, tailChunk at a
// time: first to the last newline (a torn line after it is skipped), then to the
// newline before that, so a line of any length is found, and the file is read
// no further back than that line's start.
func lastWhole(f *os.File) (int64, uint64, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, 0, err
	}
	size := info.Size()
	// pos is where the next chunk ends; end is the offset just past the last
	// newline, -1 until it is found; line is the last line's bytes so far, grown
	// at the front as the chunks come from further back.
	end := int64(-1)
	var line []byte
	for pos := size; pos > 0; {
		from := max(pos-tailChunk, 0)
		chunk := make([]byte, pos-from)
		if _, err := f.ReadAt(chunk, from); err != nil && !errors.Is(err, io.EOF) {
			return 0, 0, fmt.Errorf("reading the event log's end: %w", err)
		}
		if end < 0 {
			cut := bytes.LastIndexByte(chunk, '\n')
			if cut < 0 {
				pos = from
				continue
			}
			end = from + int64(cut) + 1
			chunk = chunk[:cut]
		}
		if cut := bytes.LastIndexByte(chunk, '\n'); cut >= 0 {
			line = append(append([]byte{}, chunk[cut+1:]...), line...)
			break
		}
		line = append(append([]byte{}, chunk...), line...)
		pos = from
	}
	if end < 0 {
		return 0, 0, nil
	}
	var e events.Event
	if err := json.Unmarshal(line, &e); err != nil {
		return 0, 0, fmt.Errorf("reading the event log's last line: %w", err)
	}
	return end, e.Seq, nil
}

// sidecar is the head the .seq file holds, 0 when there is none.
func (l *Log) sidecar() (uint64, error) {
	data, err := os.ReadFile(l.seqPath())
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	seq, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("the event log's head %s holds %q, not a seq", l.seqPath(), data)
	}
	return seq, nil
}

// Since implements application.EventLog. A torn last line, being written as
// it is read or left by a crash, is not an event yet and is skipped. The log is
// in seq order, so a Since past the first event finds where to begin by reading
// the log from its end, and reads only what it returns (mw-gq6.655); Since(0)
// is the whole log.
func (l *Log) Since(_ context.Context, seq uint64) ([]events.Event, error) {
	f, err := os.Open(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening the event log: %w", err)
	}
	defer f.Close()
	var start int64
	if seq > 0 {
		if start, err = startAfter(f, seq); err != nil {
			return nil, err
		}
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("reading the event log: %w", err)
	}
	var out []events.Event
	r := bufio.NewReader(f)
	for n := 1; ; n++ {
		line, err := r.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("reading the event log: %w", err)
		}
		var e events.Event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("line %d after byte %d of %s: %w", n, start, l.Path, err)
		}
		if e.Seq > seq {
			out = append(out, e)
		}
	}
}

// backChunk is how much of the log's end startAfter reads at a time; it grows
// for a line longer than that.
const backChunk = 256 << 10

// startAfter is the byte offset at which the log's lines with a seq above seq
// begin: the end of the last whole line whose seq is at most seq, or 0 when
// there is none. It reads the log backward from its last whole line, a chunk at
// a time, and stops at that line.
func startAfter(f *os.File, seq uint64) (int64, error) {
	end, _, err := lastWhole(f)
	if err != nil {
		return 0, err
	}
	chunk := int64(backChunk)
	for end > 0 {
		from := max(end-chunk, 0)
		buf := make([]byte, end-from)
		if _, err := f.ReadAt(buf, from); err != nil && !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("reading the event log: %w", err)
		}
		whole := buf
		if from > 0 {
			cut := bytes.IndexByte(buf, '\n')
			if cut < 0 || cut == len(buf)-1 {
				// No whole line starts in this chunk: read a bigger one.
				chunk *= 2
				continue
			}
			whole = buf[cut+1:]
			from += int64(cut) + 1
		}
		// whole is the lines from offset from to end, each ending in a newline.
		stop := int64(len(whole))
		for stop > 0 {
			begin := int64(bytes.LastIndexByte(whole[:stop-1], '\n')) + 1
			var e struct {
				Seq uint64 `json:"seq"`
			}
			if err := json.Unmarshal(whole[begin:stop], &e); err != nil {
				return 0, fmt.Errorf("reading the event log backward from byte %d: %w", from+stop, err)
			}
			if e.Seq <= seq {
				return from + stop, nil
			}
			stop = begin
		}
		end = from
	}
	return 0, nil
}

// statePath is the file beside the log that holds what Newest has folded of it:
// log.jsonl's is log.newest.json.
func (l *Log) statePath() string {
	return strings.TrimSuffix(l.Path, filepath.Ext(l.Path)) + ".newest.json"
}

// Newest implements application.EventNewestLog. It reads the state file, folds
// in the events after the seq it covers (only those are read), and leaves the
// result in the file for the next reader. A state file that is absent, cannot be
// read, or covers more than the log holds (a log begun again) is made anew from
// the whole log, once; so is one an older build made, which lacks what a newer
// reader asks of it (EventNewestVersion). The file is only a cache: a failure to write it is not
// the reader's.
func (l *Log) Newest(ctx context.Context) (application.EventNewest, error) {
	head, err := l.head()
	if err != nil {
		return application.EventNewest{}, err
	}
	var n application.EventNewest
	if data, err := os.ReadFile(l.statePath()); err == nil {
		if json.Unmarshal(data, &n) != nil || n.Seq > head || (n.Seq > 0 && n.Version != application.EventNewestVersion) {
			n = application.EventNewest{}
		}
	}
	if n.Seq >= head {
		return n, nil
	}
	evs, err := l.Since(ctx, n.Seq)
	if err != nil {
		return application.EventNewest{}, err
	}
	for _, e := range evs {
		n.Add(e)
	}
	if len(evs) > 0 {
		if data, err := json.Marshal(n); err == nil {
			_ = writeSynced(l.statePath(), append(data, '\n'))
		}
	}
	return n, nil
}

// Head implements application.EventLog.
func (l *Log) Head(context.Context) (uint64, error) { return l.head() }

// head is the sidecar's seq; a log whose sidecar lags (a crash) is read to
// its last line instead.
func (l *Log) head() (uint64, error) {
	head, err := l.sidecar()
	if err != nil {
		return 0, err
	}
	f, err := os.Open(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return head, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	_, last, err := lastWhole(f)
	if err != nil {
		return 0, err
	}
	if last > head {
		head = last
	}
	return head, nil
}

// writeSynced writes data to path by a synced temporary file renamed over
// it, so a reader sees the old or the new, never half.
func writeSynced(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
