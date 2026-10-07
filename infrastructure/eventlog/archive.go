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
	"sort"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// ArchiveDir is the directory beside the log that holds the events a trim
// moved out of it, one file a day: log-<date>.jsonl.
const ArchiveDir = "archive"

// Log is also the log's archive.
var _ application.EventArchive = (*Log)(nil)

func (l *Log) archiveDir() string { return filepath.Join(filepath.Dir(l.Path), ArchiveDir) }

// Trim implements application.EventArchive. Under the writers' lock it appends
// the log's lines with a seq up to upTo to the day's archive file and fsyncs
// it, then replaces the log by a synced file of the lines left, renamed over
// it, so a reader sees the old log or the new and a crash in between leaves
// the events in both (Archived reads each once). A torn last line is not
// copied to either: it is cut, as Append cuts it. A trim that moves nothing
// writes nothing. The head sidecar is not touched.
func (l *Log) Trim(_ context.Context, upTo uint64, day string) (int, error) {
	unlock, err := l.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	data, err := os.ReadFile(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("reading the event log: %w", err)
	}
	data = data[:bytes.LastIndexByte(data, '\n')+1]
	cut, moved := 0, 0
	for cut < len(data) {
		end := cut + bytes.IndexByte(data[cut:], '\n') + 1
		var e events.Event
		if err := json.Unmarshal(data[cut:end], &e); err != nil {
			return 0, fmt.Errorf("the event log's line after %d bytes: %w", cut, err)
		}
		if e.Seq > upTo {
			break
		}
		cut, moved = end, moved+1
	}
	if moved == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(l.archiveDir(), 0o700); err != nil {
		return 0, fmt.Errorf("making the event archive's directory: %w", err)
	}
	archive, err := os.OpenFile(filepath.Join(l.archiveDir(), "log-"+day+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, fmt.Errorf("opening the event archive: %w", err)
	}
	if _, err := archive.Write(data[:cut]); err == nil {
		err = archive.Sync()
	}
	if cerr := archive.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, fmt.Errorf("writing the event archive: %w", err)
	}
	if err := writeSynced(l.Path, data[cut:]); err != nil {
		return 0, fmt.Errorf("cutting the old events off the event log: %w", err)
	}
	return moved, nil
}

// First implements application.EventArchive: the seq of the log's first whole
// line, 0 when it has none.
func (l *Log) First(context.Context) (uint64, error) {
	f, err := os.Open(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	line, err := bufio.NewReader(f).ReadBytes('\n')
	if errors.Is(err, io.EOF) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var e events.Event
	if err := json.Unmarshal(line, &e); err != nil {
		return 0, fmt.Errorf("reading the event log's first line: %w", err)
	}
	return e.Seq, nil
}

// Archived implements application.EventArchive: the archive files in date
// order, their events with a seq above seq, each seq once.
func (l *Log) Archived(_ context.Context, seq uint64) ([]events.Event, error) {
	files, err := filepath.Glob(filepath.Join(l.archiveDir(), "log-*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []events.Event
	last := seq
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("opening the event archive: %w", err)
		}
		r := bufio.NewReader(f)
		for n := 1; ; n++ {
			line, err := r.ReadBytes('\n')
			if err != nil {
				f.Close()
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, fmt.Errorf("reading %s: %w", path, err)
			}
			var e events.Event
			if err := json.Unmarshal(line, &e); err != nil {
				f.Close()
				return nil, fmt.Errorf("line %d of %s: %w", n, path, err)
			}
			if e.Seq > last {
				out = append(out, e)
				last = e.Seq
			}
		}
	}
	return out, nil
}
