package claude

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// grindVersionTimeout is the longest a grind waits for its program to say its
// version. A version not read in that time is left out of the grind's record;
// the grind runs all the same.
const grindVersionTimeout = 10 * time.Second

// grindVersionMax is the most of the version's line a grind keeps.
const grindVersionMax = 120

// grindVersions is what the grinder's program said of its own version, kept for
// the install it was read from: the program's file, after its links, with that
// file's size and time. Claude Code updates itself by moving its link to a new
// file, so a grind after an update reads the version again, and every other
// grind costs a stat, not a run of the program (mw-gq6.1712).
type grindVersions struct {
	mu      sync.Mutex
	install string
	version string
}

// read is what program prints for `--version`, its first line, or "" when
// it cannot be read: a failure is kept for the install too, so a program that
// cannot say is not asked again before every grind.
func (v *grindVersions) read(ctx context.Context, program string) string {
	install := programInstall(program)
	v.mu.Lock()
	defer v.mu.Unlock()
	if install != "" && install == v.install {
		return v.version
	}
	v.install, v.version = install, readGrindVersion(ctx, program)
	return v.version
}

// programInstall names the file program runs and its size and time, or ""
// when it cannot be found.
func programInstall(program string) string {
	path, err := exec.LookPath(program)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s %d %d", path, info.Size(), info.ModTime().UnixNano())
}

// readGrindVersion runs `program --version` in a process group of its own,
// killed with the group at grindVersionTimeout, and keeps its first line.
func readGrindVersion(ctx context.Context, program string) string {
	ctx, cancel := context.WithTimeout(ctx, grindVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(out.String()), "\n")
	line = strings.TrimSpace(line)
	if len(line) > grindVersionMax {
		line = line[:grindVersionMax]
	}
	return line
}
