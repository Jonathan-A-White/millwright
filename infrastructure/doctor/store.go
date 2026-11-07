// Package doctor is the adapter mw doctor keeps its episode state and its log
// through, and the checks it ships: one plain file per check under a
// directory of its own on this host, a log file beside them, and the
// daemon-reload check, which shells out to systemctl and nothing else.
package doctor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/atomicfile"
)

var (
	_ application.DoctorState = (*Store)(nil)
	_ application.DoctorLog   = (*Store)(nil)
)

// LogFile is the name of the log file kept beside the state files.
const LogFile = "log"

// LogRotateBytes is how big the log may grow before it is rotated: the full
// log becomes LogFile+".1", in place of the one before it, and a fresh one is
// begun (mw-gq6.982). A doctor run adds one line a check, so the log grew by
// megabytes; Read gives both files, and the lines a note reads (a check's
// last few) are in the newest of them.
const LogRotateBytes = 1 << 20

// Store is the adapter: one file per check under Dir, plain "key=value"
// lines, and LogFile beside them for what every run found. Dir is made when
// first written to.
type Store struct {
	Dir string
	// MaxLogBytes is the size at which the log is rotated; 0 is LogRotateBytes.
	MaxLogBytes int64
}

// New is a Store keeping its state and its log in dir.
func New(dir string) *Store { return &Store{Dir: dir} }

func (s *Store) stateFile(check string) string { return filepath.Join(s.Dir, check) }

// Load implements application.DoctorState. A file never written, or holding
// nothing Load can parse, is the zero episode: mw doctor starts counting
// again, which costs at most one damper wait.
func (s *Store) Load(_ context.Context, check string) (application.DoctorEpisode, error) {
	data, err := os.ReadFile(s.stateFile(check))
	if os.IsNotExist(err) {
		return application.DoctorEpisode{}, nil
	}
	if err != nil {
		return application.DoctorEpisode{}, fmt.Errorf("reading %s's doctor state: %w", check, err)
	}

	var episode application.DoctorEpisode
	lines := bufio.NewScanner(strings.NewReader(string(data)))
	for lines.Scan() {
		key, value, found := strings.Cut(lines.Text(), "=")
		if !found {
			continue
		}
		switch key {
		case "firstFaulty":
			episode.FirstFaulty = parseTime(value)
		case "cures":
			if n, err := strconv.Atoi(value); err == nil {
				episode.Cures = n
			}
		case "lastCure":
			episode.LastCure = parseTime(value)
		case "lastCureFailed":
			episode.LastCureFailed = value == "true"
		case "lastOKReason":
			episode.LastOKReason = value
		case "lastCannotTellReason":
			episode.LastCannotTellReason = value
		case "dampedNoted":
			episode.DampedNoted = value == "true"
		case "seenPaths":
			if value != "" {
				episode.SeenPaths = strings.Split(value, "\t")
			}
		}
	}
	return episode, nil
}

// Save implements application.DoctorState, writing the file whole under a
// temp name and moving it into place, so a reader never sees half of it.
func (s *Store) Save(_ context.Context, check string, episode application.DoctorEpisode) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", s.Dir, err)
	}
	path := s.stateFile(check)
	body := fmt.Sprintf("firstFaulty=%s\ncures=%d\nlastCure=%s\nlastCureFailed=%t\nlastOKReason=%s\nlastCannotTellReason=%s\ndampedNoted=%t\nseenPaths=%s\n",
		formatTime(episode.FirstFaulty), episode.Cures, formatTime(episode.LastCure), episode.LastCureFailed, episode.LastOKReason, episode.LastCannotTellReason, episode.DampedNoted, strings.Join(episode.SeenPaths, "\t"))
	if err := atomicfile.Write(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("saving %s's doctor state: %w", check, err)
	}
	return nil
}

// Reset implements application.DoctorState: forgetting a check's episode is
// removing its file.
func (s *Store) Reset(_ context.Context, check string) error {
	if err := os.Remove(s.stateFile(check)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("forgetting %s's doctor state: %w", check, err)
	}
	return nil
}

// Append implements application.DoctorLog: one line, added to the end.
func (s *Store) Append(_ context.Context, line string) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", s.Dir, err)
	}
	path := filepath.Join(s.Dir, LogFile)
	if err := s.rotate(path); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	if _, err := fmt.Fprintln(file, line); err != nil {
		file.Close()
		return fmt.Errorf("appending to %s: %w", path, err)
	}
	return file.Close()
}

// rotate moves the log at path to path+".1" when it has reached the size it
// may grow to; a log that is not there or is smaller is left alone.
func (s *Store) rotate(path string) error {
	max := s.MaxLogBytes
	if max <= 0 {
		max = LogRotateBytes
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < max {
		return nil
	}
	if err := os.Rename(path, path+".1"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rotating %s: %w", path, err)
	}
	return nil
}

// Read implements application.DoctorLog: every line the log holds, and the
// rotated log before it, oldest first; none, and no error, for a log nothing
// has been written to yet.
func (s *Store) Read(_ context.Context) ([]string, error) {
	var lines []string
	for _, name := range []string{LogFile + ".1", LogFile} {
		path := filepath.Join(s.Dir, name)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		if text := strings.TrimRight(string(data), "\n"); text != "" {
			lines = append(lines, strings.Split(text, "\n")...)
		}
	}
	return lines, nil
}

func formatTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// parseTime reads back what formatTime wrote. Anything it cannot parse,
// including "", is the zero time: a state file somebody edited by hand costs
// at most one fresh episode, not a failure.
func parseTime(value string) time.Time {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return at
}
