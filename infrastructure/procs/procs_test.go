package procs_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/procs"
)

// fakeProc writes one process of the fake table: its comm and the Uid line of
// its status, real uid first, as the kernel writes it.
func fakeProc(t *testing.T, proc, pid, comm string, uid int) {
	t.Helper()
	dir := filepath.Join(proc, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm), 0o644); err != nil {
		t.Fatal(err)
	}
	status := fmt.Sprintf("Name:\t%s\nUmask:\t0022\nUid:\t%d\t%d\t%d\t%d\nGid:\t%d\t%d\t%d\t%d\n", strings.TrimSpace(comm), uid, uid, uid, uid, uid, uid, uid, uid)
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mw-9rp4hu.1: two Linux users on one host each count their own harness
// processes, not the other's.
func TestCountIsOnlyTheCurrentUsersProcesses(t *testing.T) {
	proc := t.TempDir()
	fakeProc(t, proc, "201", "claude\n", 1000)
	fakeProc(t, proc, "202", "claude\n", 1000)
	fakeProc(t, proc, "203", "claude\n", 1001)
	fakeProc(t, proc, "204", "claude\n", 1001)
	fakeProc(t, proc, "205", "claude\n", 1001)
	fakeProc(t, proc, "206", "bash\n", 1000)

	for uid, want := range map[int]int{1000: 2, 1001: 3, 1002: 0} {
		got, err := procs.Harness{Proc: proc, Uid: func() int { return uid }}.Count(context.Background())
		if err != nil || got != want {
			t.Errorf("uid %d counted %d (%v), want %d", uid, got, err, want)
		}
	}
}

func TestCountIsTheNumberedEntriesWhoseNameIsTheHarnesss(t *testing.T) {
	proc := t.TempDir()
	uid := os.Getuid()
	for pid, comm := range map[string]string{"101": "claude\n", "102": "claude\n", "103": "bash\n", "104": "claude-sidecar\n"} {
		fakeProc(t, proc, pid, comm, uid)
	}
	// Not a process: no number; a process gone: no comm.
	if err := os.MkdirAll(filepath.Join(proc, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "self", "comm"), []byte("claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proc, "105"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := procs.Harness{Proc: proc}.Count(context.Background())
	if err != nil || got != 2 {
		t.Fatalf("counted %d (%v), want 2", got, err)
	}
}

func TestCountOfNoProcessTableIsAnError(t *testing.T) {
	if _, err := (procs.Harness{Proc: filepath.Join(t.TempDir(), "none")}).Count(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

// fakeCmdline gives a process of the fake table its argv, NUL-separated as the
// kernel writes it.
func fakeCmdline(t *testing.T, proc, pid string, argv ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(proc, pid, "cmdline"), []byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mw-gq6.394: the heartbeat of a story is the user's own `mw next --heartbeat
// <story>`, and no other process, story or user's.
func TestBeatingIsTheUsersHeartbeatsOfThatStory(t *testing.T) {
	proc := t.TempDir()
	fakeProc(t, proc, "301", "mw\n", 1000)
	fakeCmdline(t, proc, "301", "/home/u/bin/mw", "next", "--heartbeat", "mw-x.1")
	fakeProc(t, proc, "302", "mw\n", 1000)
	fakeCmdline(t, proc, "302", "/home/u/bin/mw", "next", "--heartbeat", "mw-x.2")
	fakeProc(t, proc, "303", "mw\n", 1001)
	fakeCmdline(t, proc, "303", "/home/u/bin/mw", "next", "--heartbeat", "mw-x.1")
	// The close-out itself, and a zombie with nothing left of its cmdline.
	fakeProc(t, proc, "304", "mw\n", 1000)
	fakeCmdline(t, proc, "304", "/home/u/bin/mw", "next", "mw-x.1")
	fakeProc(t, proc, "305", "mw\n", 1000)
	fakeCmdline(t, proc, "305")

	got, err := procs.Heartbeats{Proc: proc, Uid: func() int { return 1000 }}.Beating(context.Background(), "mw-x.1")
	if err != nil || len(got) != 1 || got[0] != 301 {
		t.Errorf("Beating(mw-x.1) = %v (%v), want [301]", got, err)
	}
	got, err = procs.Heartbeats{Proc: proc, Uid: func() int { return 1000 }}.Beating(context.Background(), "mw-x.3")
	if err != nil || len(got) != 0 {
		t.Errorf("Beating(mw-x.3) = %v (%v), want none", got, err)
	}
}
