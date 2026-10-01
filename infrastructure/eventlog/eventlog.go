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

// tailRead is how much of the log's end Append reads to find the last
// line's seq: far more than one event.
const tailRead = 64 << 10

// Append implements application.EventLog. Under an exclusive flock on the
// log, it cuts a torn last line (a crash mid-write), takes the head as the
// greater of the sidecar and the last line's seq (a crash between the log's
// fsync and the sidecar's), writes the batch in one write, fsyncs, then
// writes the sidecar.
func (l *Log) Append(_ context.Context, evs []events.Event) (uint64, error) {
	if len(evs) == 0 {
		return l.head()
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return 0, fmt.Errorf("making the event log's directory: %w", err)
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, fmt.Errorf("opening the event log: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return 0, fmt.Errorf("locking the event log: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

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
// for an empty log).
func lastWhole(f *os.File) (int64, uint64, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, 0, err
	}
	size := info.Size()
	from := size - tailRead
	if from < 0 {
		from = 0
	}
	tail := make([]byte, size-from)
	if _, err := f.ReadAt(tail, from); err != nil && !errors.Is(err, io.EOF) {
		return 0, 0, fmt.Errorf("reading the event log's end: %w", err)
	}
	cut := bytes.LastIndexByte(tail, '\n')
	if cut < 0 {
		if from > 0 {
			return 0, 0, fmt.Errorf("the event log's last %d bytes hold no whole line", tailRead)
		}
		return 0, 0, nil
	}
	end := from + int64(cut) + 1
	whole := tail[:cut]
	line := whole[bytes.LastIndexByte(whole, '\n')+1:]
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
// it is read or left by a crash, is not an event yet and is skipped.
func (l *Log) Since(_ context.Context, seq uint64) ([]events.Event, error) {
	f, err := os.Open(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening the event log: %w", err)
	}
	defer f.Close()
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
			return nil, fmt.Errorf("line %d of %s: %w", n, l.Path, err)
		}
		if e.Seq > seq {
			out = append(out, e)
		}
	}
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
