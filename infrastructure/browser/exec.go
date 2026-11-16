package browser

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ExecLauncher starts real programs, each in a session of its own so it
// outlives the mw that started it, its output appended to its Log (mode 600).
type ExecLauncher struct {
	// Grace is how long Stop waits after SIGTERM before SIGKILL; 0 is 5s.
	Grace time.Duration
}

// Start starts p and returns its pid. A goroutine waits for it, so a program
// that ends while this mw still runs is not left a zombie.
func (l ExecLauncher) Start(p Program) (int, error) {
	log, err := os.OpenFile(p.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer log.Close()
	cmd := exec.Command(p.Path, p.Args...)
	if p.Env != nil {
		cmd.Env = p.Env
	}
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go func() { _ = cmd.Wait() }()
	return cmd.Process.Pid, nil
}

// Running reports whether pid is alive, not a zombie, and its command line
// carries marker.
func (l ExecLauncher) Running(pid int, marker string) bool {
	if pid <= 0 {
		return false
	}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	// The state is the field after the command's closing parenthesis.
	if i := bytes.LastIndexByte(stat, ')'); i < 0 || i+2 >= len(stat) || stat[i+2] == 'Z' || stat[i+2] == 'X' {
		return false
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false
	}
	return strings.Contains(string(bytes.ReplaceAll(cmdline, []byte{0}, []byte{' '})), marker)
}

// Stop sends SIGTERM to pid's process group, waits up to Grace for pid to end,
// then sends SIGKILL to the group.
func (l ExecLauncher) Stop(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("no pid to stop")
	}
	grace := l.Grace
	if grace <= 0 {
		grace = 5 * time.Second
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !l.Running(pid, "") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	for end := time.Now().Add(2 * time.Second); l.Running(pid, "") && time.Now().Before(end); {
		time.Sleep(20 * time.Millisecond)
	}
	if l.Running(pid, "") {
		return fmt.Errorf("pid %d still runs after SIGKILL", pid)
	}
	return nil
}

// FindChromium is the Chromium to run: the first of chromium, chromium-browser,
// google-chrome and google-chrome-stable on PATH, else the newest Chromium
// Playwright installed under home's ~/.cache/ms-playwright.
func FindChromium(home string, lookPath func(string) (string, error)) (string, error) {
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := lookPath(name); err == nil {
			return p, nil
		}
	}
	cache := filepath.Join(home, ".cache", "ms-playwright")
	found, _ := filepath.Glob(filepath.Join(cache, "chromium-*", "chrome-linux*", "chrome"))
	type build struct {
		rev  int
		path string
	}
	var builds []build
	for _, p := range found {
		rel, _ := filepath.Rel(cache, p)
		rev, err := strconv.Atoi(strings.TrimPrefix(strings.Split(rel, string(filepath.Separator))[0], "chromium-"))
		if err != nil {
			continue
		}
		builds = append(builds, build{rev, p})
	}
	if len(builds) == 0 {
		return "", fmt.Errorf("no Chromium found: none of chromium, chromium-browser, google-chrome is on PATH and %s holds no chromium-*/chrome-linux*/chrome (npx playwright install chromium puts one there)", cache)
	}
	sort.Slice(builds, func(i, j int) bool { return builds[i].rev > builds[j].rev })
	return builds[0].path, nil
}
