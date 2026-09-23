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
)

var (
	_ application.DoctorState = (*Store)(nil)
	_ application.DoctorLog   = (*Store)(nil)
)

// LogFile is the name of the log file kept beside the state files.
const LogFile = "log"

// Store is the adapter: one file per check under Dir, plain "key=value"
// lines, and LogFile beside them for what every run found. Dir is made when
// first written to.
type Store struct {
	Dir string
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
	whole := path + ".new"
	body := fmt.Sprintf("firstFaulty=%s\ncures=%d\nlastCure=%s\n",
		formatTime(episode.FirstFaulty), episode.Cures, formatTime(episode.LastCure))
	if err := os.WriteFile(whole, []byte(body), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", whole, err)
	}
	if err := os.Rename(whole, path); err != nil {
		return fmt.Errorf("moving %s into place: %w", whole, err)
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

// Read implements application.DoctorLog: every line the log holds, oldest
// first; none, and no error, for a log nothing has been written to yet.
func (s *Store) Read(_ context.Context) ([]string, error) {
	path := filepath.Join(s.Dir, LogFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
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
