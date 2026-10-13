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
