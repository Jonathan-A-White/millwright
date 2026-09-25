package vault_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// alive reports whether a process is still running, without disturbing it.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// readPidWhenWritten polls for a pid file a stand-in git writes once it and
// its child have started, so the test does not race the shell script's own
// startup.
func readPidWhenWritten(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) != "" {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("reading pid from %s: %v", path, err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return 0
}

// stubbornGit puts a `git` on PATH that starts a background child of its own —
// standing in for the network process a real fetch runs underneath — and then
// hangs, so a test can check that cancelling mw's context takes both with it.
func stubbornGit(t *testing.T) (mainPid, childPid string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for git is a shell script")
	}
	dir := t.TempDir()
	mainPid = filepath.Join(dir, "main.pid")
	childPid = filepath.Join(dir, "child.pid")
	script := fmt.Sprintf(`#!/bin/sh
echo $$ > %q
( while true; do sleep 0.05; done ) &
echo $! > %q
wait
`, mainPid, childPid)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for git: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return mainPid, childPid
}

func TestACancelledPushKillsGitAndWhateverItStarted(t *testing.T) {
	mainPidFile, childPidFile := stubbornGit(t)
	files := vault.New(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := files.Push(ctx); done <- err }()

	mainPid := readPidWhenWritten(t, mainPidFile)
	childPid := readPidWhenWritten(t, childPidFile)
	if !alive(mainPid) || !alive(childPid) {
		t.Fatal("expected the stand-in and its child to be running before the cancel")
	}

	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a cancelled push to report an error")
		}
		if !strings.Contains(err.Error(), "stopped") {
			t.Fatalf("expected the failure to say it was stopped, got %q", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the cancelled push to return within a few seconds, not hang")
	}

	deadline := time.Now().Add(3 * time.Second)
	for (alive(mainPid) || alive(childPid)) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(mainPid) {
		t.Errorf("expected the git stand-in (pid %d) to be gone once cancelled", mainPid)
	}
	if alive(childPid) {
		t.Errorf("expected the child it started (pid %d) to be gone too, not orphaned", childPid)
	}
}

// fakeGitEnv puts a `git` on PATH that writes the two low-speed variables it
// was given to a file, and exits 0, so a test can check what the vault's git
// runner set without a real stalled connection to trigger them for real.
func fakeGitEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	seen := filepath.Join(dir, "env-seen")
	script := fmt.Sprintf(`#!/bin/sh
printf 'limit=%%s time=%%s\n' "$GIT_HTTP_LOW_SPEED_LIMIT" "$GIT_HTTP_LOW_SPEED_TIME" > %q
echo 0
exit 0
`, seen)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for git: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return seen
}

func TestVaultGitCallsCarryTheLowSpeedLimit(t *testing.T) {
	seen := fakeGitEnv(t)

	if _, err := vault.New(t.TempDir()).Push(context.Background()); err != nil {
		t.Fatalf("pushing: %v", err)
	}

	got, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("reading what the stand-in saw: %v", err)
	}
	if want := "limit=1000 time=60\n"; string(got) != want {
		t.Fatalf("expected the vault's git runner to set %q, got %q", want, got)
	}
}

// fakeStalledGit prints the message curl gives when a transfer sits under the
// low-speed limit for the low-speed window, so this test checks that message
// reaches mw rather than assuming it does.
func fakeStalledGit(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'error: RPC failed; curl 28 Operation too slow. " +
		"Less than 1000 bytes/sec transferred the last 60 seconds' >&2\nexit 128\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for git: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestAStalledFetchFailsNamingTheStallRatherThanHanging(t *testing.T) {
	fakeStalledGit(t)

	done := make(chan struct{})
	var err error
	go func() {
		_, err = vault.New(t.TempDir()).Push(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the stalled fetch to fail promptly, not hang")
	}
	if err == nil {
		t.Fatal("expected a stalled fetch to be reported as a failure")
	}
	if !strings.Contains(err.Error(), "too slow") {
		t.Fatalf("expected the failure to name the stall, got %q", err)
	}
}
