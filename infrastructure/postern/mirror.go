package postern

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// Copy implements application.PosternMirrorer: one `rsync -rpR`, run in dir so
// that names stay relative and keep their directories.
func (Mirrorer) Copy(ctx context.Context, dir string, names []string, rsh []string, dest string) error {
	if len(rsh) == 0 {
		rsh = unattendedSSH
	}
	args := append([]string{"-rpR", "--ignore-missing-args", "-e", strings.Join(rsh, " ")}, names...)
	cmd := exec.CommandContext(ctx, "rsync", append(args, dest)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rsync to %s: %w: %s", dest, err, strings.TrimSpace(string(out)))
	}
	return nil
}
