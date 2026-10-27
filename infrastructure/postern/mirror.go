package postern

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
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Mirrorer is the real application.PosternMirrorer: rsync for the copy and, for
// the index of another host, ssh.
type Mirrorer struct{}

var _ application.PosternMirrorer = Mirrorer{}

// unattendedSSH is the remote shell for a destination that names none: a timer
// has nobody to answer a prompt.
var unattendedSSH = []string{"ssh", "-o", "BatchMode=yes"}

// LastIndexTime implements application.PosternMirrorer. The time is the last
// line's firstSeen; a line without one, or not JSON, is an error rather than an
// answer, so that it is never taken for an old index.
func (Mirrorer) LastIndexTime(ctx context.Context, ssh []string, dir string) (time.Time, bool, error) {
	return lastRecordTime(ctx, ssh, filepath.Join(dir, application.PosternIndexFile), "firstSeen")
}

// LastGrindTime implements application.PosternMirrorer. The time is the last
// line's time, under the same rule as LastIndexTime's.
func (Mirrorer) LastGrindTime(ctx context.Context, ssh []string, dir string) (time.Time, bool, error) {
	return lastRecordTime(ctx, ssh, filepath.Join(dir, application.GristRecordFile), "time")
}

// DirExists implements application.PosternMirrorer.
func (Mirrorer) DirExists(_ context.Context, dir string) (bool, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

// lastRecordTime is the time in field of the last line of the JSON-lines file
// at path, on the host reached by ssh (this one when ssh is empty).
func lastRecordTime(ctx context.Context, ssh []string, path, field string) (time.Time, bool, error) {
	var last string
	if len(ssh) == 0 {
		var err error
		if last, err = lastLine(path); err != nil {
			return time.Time{}, false, err
		}
	} else {
		args := append(append([]string{}, ssh[1:]...), "test -f '"+path+"' && tail -n 1 '"+path+"'")
		cmd := exec.CommandContext(ctx, ssh[0], args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			// test -f failing is the answer "no file", and it says nothing.
			if _, exited := err.(*exec.ExitError); exited && strings.TrimSpace(stderr.String()) == "" && len(out) == 0 {
				return time.Time{}, false, nil
			}
			return time.Time{}, false, fmt.Errorf("%s: %w: %s", strings.Join(ssh, " "), err, strings.TrimSpace(stderr.String()))
		}
		last = strings.TrimSpace(string(out))
	}
	if last == "" {
		return time.Time{}, false, nil
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(last), &line); err != nil {
		return time.Time{}, false, fmt.Errorf("the last line of %s has no %s time", path, field)
	}
	raw, _ := line[field].(string)
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || at.IsZero() {
		return time.Time{}, false, fmt.Errorf("the last line of %s has no %s time", path, field)
	}
	return at, true, nil
}

// lastLine is the last non-empty line of path, "" when there is none or no file.
func lastLine(path string) (string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	last := ""
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for lines.Scan() {
		if text := strings.TrimSpace(lines.Text()); text != "" {
			last = text
		}
	}
	return last, lines.Err()
}

// rsyncVanished is rsync's exit status for "some source files vanished before
// they could be transferred": postern removing a blob mid-copy.
const rsyncVanished = 24

// Copy implements application.PosternMirrorer: one `rsync -rpR`, run in dir so
// that names stay relative and keep their directories. Exit 24 is a
// *application.CopyVanished, not a failure; exit 23 and every other code fail.
func (Mirrorer) Copy(ctx context.Context, dir string, names []string, rsh []string, dest string) error {
	if len(rsh) == 0 {
		rsh = unattendedSSH
	}
	args := append([]string{"-rpR", "--ignore-missing-args", "-e", strings.Join(rsh, " ")}, names...)
	cmd := exec.CommandContext(ctx, "rsync", append(args, dest)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == rsyncVanished {
			return &application.CopyVanished{Files: max(1, strings.Count(string(out), "file has vanished:"))}
		}
		return fmt.Errorf("rsync to %s: %w: %s", dest, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ReadMillCursor implements application.PosternMirrorer.
func (Mirrorer) ReadMillCursor(_ context.Context, dir string) (time.Time, bool, error) {
	text, err := os.ReadFile(filepath.Join(dir, application.MillCursorFile))
	if os.IsNotExist(err) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(text)))
	if err != nil {
		// A cursor that cannot be read is no cursor: the next copy is whole.
		return time.Time{}, false, nil
	}
	return at, true, nil
}

// WriteMillCursor implements application.PosternMirrorer. It writes beside the
// file and renames, so that a reader never sees half of it.
func (Mirrorer) WriteMillCursor(_ context.Context, dir string, at time.Time) error {
	path := filepath.Join(dir, application.MillCursorFile)
	if err := os.WriteFile(path+".tmp", []byte(at.UTC().Format(time.RFC3339Nano)+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// MillNames implements application.PosternMirrorer. It walks the run directories
// here, on this host, and not over ssh: a stat a file is cheap where the files
// are, and it is the walk of rsync's file list on a loaded far end that costs.
func (Mirrorer) MillNames(ctx context.Context, dir string, since time.Time) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		switch name := entry.Name(); {
		case name == application.MillCursorFile || name == application.MillCursorFile+".tmp":
		case name == application.MillRunsDir && !since.IsZero() && entry.IsDir():
		default:
			names = append(names, name)
		}
	}
	if since.IsZero() {
		return names, nil
	}
	runs, err := os.ReadDir(filepath.Join(dir, application.MillRunsDir))
	if os.IsNotExist(err) {
		return names, nil
	}
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		changed, err := changedSince(filepath.Join(dir, application.MillRunsDir, run.Name()), since)
		if err != nil {
			return nil, err
		}
		if changed {
			names = append(names, application.MillRunsDir+"/"+run.Name())
		}
	}
	return names, nil
}

// changedSince reports whether path, or anything under it, changed after since.
// A file that goes while it is walked is not an error.
func changedSince(path string, since time.Time) (bool, error) {
	changed := false
	err := filepath.WalkDir(path, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		info, err := entry.Info()
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.ModTime().After(since) {
			changed = true
			return filepath.SkipAll
		}
		return nil
	})
	return changed, err
}
