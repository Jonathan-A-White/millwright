package beads_test

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

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// alive reports whether a process is still running, without disturbing it.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// readPidWhenWritten polls for a pid file a stand-in bd writes once it and its
// child have started, so the test does not race the shell script's own startup.
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

// stubbornBd is a stand-in bd that starts a background child of its own
// (standing in for the git a real `bd sync` starts underneath) and then hangs,
// ignoring nothing in particular — it is SIGKILL, not a trap it could dodge,
// that is expected to end it. It writes its own pid and its child's, so the
// test can tell whether either is still there afterwards.
func stubbornBd(t *testing.T) (program, mainPid, childPid string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	mainPid = filepath.Join(dir, "main.pid")
	childPid = filepath.Join(dir, "child.pid")
	program = filepath.Join(dir, "bd-stubborn")
	script := fmt.Sprintf(`#!/bin/sh
echo $$ > %q
( while true; do sleep 0.05; done ) &
echo $! > %q
wait
`, mainPid, childPid)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, mainPid, childPid
}

func TestACancelledSyncKillsBdAndWhateverItStarted(t *testing.T) {
	program, mainPidFile, childPidFile := stubbornBd(t)
	gateway := beads.New(t.TempDir(), beads.WithProgram(program))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- gateway.Sync(ctx) }()

	mainPid := readPidWhenWritten(t, mainPidFile)
	childPid := readPidWhenWritten(t, childPidFile)
	if !alive(mainPid) || !alive(childPid) {
		t.Fatal("expected the stand-in and its child to be running before the cancel")
	}

	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a cancelled sync to report an error")
		}
		if !strings.Contains(err.Error(), "stopped") {
			t.Fatalf("expected the failure to say the sync was stopped, got %q", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the cancelled sync to return within a few seconds, not hang")
	}

	deadline := time.Now().Add(3 * time.Second)
	for (alive(mainPid) || alive(childPid)) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(mainPid) {
		t.Errorf("expected the bd stand-in (pid %d) to be gone once the sync was cancelled", mainPid)
	}
	if alive(childPid) {
		t.Errorf("expected the child it started (pid %d) to be gone too, not orphaned", childPid)
	}
}

// lingeringBd is a stand-in bd that takes a moment before it exits on its
// own, standing in for a real bd command doing real work — mw must not
// report the call finished before bd itself actually has.
func lingeringBd(t *testing.T, delay time.Duration) (program string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "bd-lingering")
	script := fmt.Sprintf("#!/bin/sh\nsleep %f\n", delay.Seconds())
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program
}

// TestSyncDoesNotReturnBeforeTheBdItStartedExits is here because of
// mw-gq6.113: the Laptop's journal showed a bd process still running after
// mw-dispatch.service had already stopped. Investigating found the run
// function in beads.go (g.run) already waits out cmd.Run() for the direct
// child it starts on every ordinary, uncancelled exit — this pins that down
// so a future change to run cannot regress it quietly. It does not reach the
// actual cause: bd 1.3.0 ships its own anonymous usage-metrics reporting
// (`bd metrics`), which — when due — re-executes itself as a background
// flusher (internal/metrics, env BD_IS_FLUSHER, posting to
// BEADS_METRICS_ENDPOINT) that mw never started directly and has no way to
// wait for or reach; that grandchild is what journald was seeing.
func TestSyncDoesNotReturnBeforeTheBdItStartedExits(t *testing.T) {
	const delay = 300 * time.Millisecond
	gateway := beads.New(t.TempDir(), beads.WithProgram(lingeringBd(t, delay)))

	start := time.Now()
	if err := gateway.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if elapsed := time.Since(start); elapsed < delay {
		t.Fatalf("Sync returned after %s, before the stand-in bd's %s sleep had run: "+
			"mw must wait for the bd it starts to actually exit, not report done early", elapsed, delay)
	}
}
