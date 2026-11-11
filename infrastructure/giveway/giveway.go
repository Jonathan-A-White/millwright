// Package giveway keeps what mw giveway keeps on a host: giveway.json, the
// marker, and giveway.log, one JSON line to an event, both in the host's state
// directory; and reads the host's tmux panes. Nothing here reaches the network.
package giveway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/atomicfile"
)

// MarkerFile and LogFile are the two files, in Store.Dir.
const (
	MarkerFile = "giveway.json"
	LogFile    = "giveway.log"
)

// Store is the marker and the log in Dir.
type Store struct{ Dir string }

var (
	_ application.GiveWayMarkers = Store{}
	_ application.GiveWayLog     = Store{}
)

// Read implements application.GiveWayMarkers. A file that is not a marker is an
// error: the host would otherwise give way to nobody in silence.
func (s Store) Read(context.Context) (application.GiveWayMarker, bool, error) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, MarkerFile))
	if errors.Is(err, fs.ErrNotExist) {
		return application.GiveWayMarker{}, false, nil
	}
	if err != nil {
		return application.GiveWayMarker{}, false, err
	}
	var marker application.GiveWayMarker
	if err := json.Unmarshal(raw, &marker); err != nil {
		return application.GiveWayMarker{}, false, fmt.Errorf("%s is not a give-way marker (remove it to let the host go on): %w", filepath.Join(s.Dir, MarkerFile), err)
	}
	return marker, true, nil
}

// Write implements application.GiveWayMarkers: whole, beside the file and
// renamed over it, so a reader never sees half of it.
func (s Store) Write(_ context.Context, marker application.GiveWayMarker) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(s.Dir, MarkerFile), append(raw, '\n'), 0o644)
}

// Remove implements application.GiveWayMarkers.
func (s Store) Remove(context.Context) error {
	err := os.Remove(filepath.Join(s.Dir, MarkerFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Append implements application.GiveWayLog: one line, added to the end.
func (s Store) Append(_ context.Context, line string) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, LogFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(strings.TrimRight(line, "\n") + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// Tail implements application.GiveWayLog: the last n lines, oldest first; no log
// is no lines.
func (s Store) Tail(_ context.Context, n int) ([]string, error) {
	f, err := os.Open(filepath.Join(s.Dir, LogFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scan.Scan() {
		lines = append(lines, scan.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return lines, scan.Err()
}

// Panes lists the panes of a tmux server (application.GiveWayPanes).
type Panes struct {
	// Program is tmux; empty is "tmux". Socket, when set, is -L's name.
	Program string
	Socket  string
	// Deadline bounds the call; zero is three seconds.
	Deadline time.Duration
}

var _ application.GiveWayPanes = Panes{}

// Panes implements application.GiveWayPanes. No server, or no tmux, is no panes.
func (p Panes) Panes(ctx context.Context) ([]application.GiveWayPane, error) {
	program := p.Program
	if program == "" {
		program = "tmux"
	}
	if _, err := exec.LookPath(program); err != nil {
		return nil, nil
	}
	deadline := p.Deadline
	if deadline == 0 {
		deadline = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	args := []string{"list-panes", "-a", "-F", "#{session_name}|#{pane_pid}|#{pane_dead}"}
	if p.Socket != "" {
		args = append([]string{"-L", p.Socket}, args...)
	}
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		said := errOut.String()
		if strings.Contains(said, "no server running") || strings.Contains(said, "No such file or directory") || strings.Contains(said, "error connecting") {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-panes: %w: %s", err, strings.TrimSpace(said))
	}
	return ParsePanes(out.String()), nil
}

// ParsePanes reads list-panes output, a pane to a line, "session|pid|dead". The
// session's name may itself hold a bar, so the two fields are read from the end.
func ParsePanes(printed string) []application.GiveWayPane {
	var panes []application.GiveWayPane
	for _, line := range strings.Split(printed, "\n") {
		line = strings.TrimSpace(line)
		last := strings.LastIndex(line, "|")
		if last < 0 {
			continue
		}
		mid := strings.LastIndex(line[:last], "|")
		if mid < 0 {
			continue
		}
		pid, err := strconv.Atoi(line[mid+1 : last])
		if err != nil {
			continue
		}
		dead := line[last+1:] == "1"
		if dead {
			pid = 0
		}
		panes = append(panes, application.GiveWayPane{Session: line[:mid], Pid: pid, Dead: dead})
	}
	return panes
}
