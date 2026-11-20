package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// aProc writes one process into a stand-in /proc: its stat line (the name in
// parentheses may hold spaces and parentheses), its command line and the
// directory it works in.
func aProc(t *testing.T, proc string, pid, ppid int, comm, state, cwd string, argv ...string) {
	t.Helper()
	dir := filepath.Join(proc, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stat := strconv.Itoa(pid) + " (" + comm + ") " + state + " " + strconv.Itoa(ppid) + " 1 1 0 -1 4194560\n"
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	cmdline := ""
	for _, a := range argv {
		cmdline += a + "\x00"
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644); err != nil {
		t.Fatal(err)
	}
	if cwd != "" {
		if err := os.Symlink(cwd, filepath.Join(dir, "cwd")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunnableProcsReadsEachProcessWithItsParentStateAndDirectory(t *testing.T) {
	proc := t.TempDir()
	aProc(t, proc, 100, 1, "claude", "S", "/work/tree", "claude", "--print")
	aProc(t, proc, 200, 100, "sh (burn) x", "R", "/tmp", "sh", "/tmp/burn575.sh")
	aProc(t, proc, 300, 100, "timeout", "D", "/tmp", "timeout", "150", "x")
	if err := os.MkdirAll(filepath.Join(proc, "self"), 0o755); err != nil { // not a pid
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 5, 0, 0, 0, time.UTC)
	snap, err := doctor.RunnableProcs{ProcDir: proc, Now: func() time.Time { return now }}.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snap.At.Equal(now) || len(snap.Processes) != 3 {
		t.Fatalf("expected three processes at %v, got %+v", now, snap)
	}
	got := map[int]bool{}
	for _, p := range snap.Processes {
		switch p.Pid {
		case 100:
			if p.Ppid != 1 || p.Running || p.Cwd != "/work/tree" || len(p.Argv) != 2 {
				t.Errorf("pid 100 read wrongly: %+v", p)
			}
		case 200:
			if p.Ppid != 100 || !p.Running {
				t.Errorf("a process in state R is running, and its name may hold spaces and parentheses: %+v", p)
			}
		case 300:
			if p.Running {
				t.Errorf("a process waiting on a disk is not running: %+v", p)
			}
		}
		got[p.Pid] = true
	}
	if !got[100] || !got[200] || !got[300] {
		t.Errorf("missing a process: %v", got)
	}
}

func TestRunnableProcsKeepsAProcessWhoseDirectoryCannotBeRead(t *testing.T) {
	proc := t.TempDir()
	aProc(t, proc, 400, 1, "daemon", "R", "")
	snap, err := doctor.RunnableProcs{ProcDir: proc}.Snapshot(context.Background())
	if err != nil || len(snap.Processes) != 1 || snap.Processes[0].Cwd != "" || !snap.Processes[0].Running {
		t.Fatalf("a process with no readable directory is still counted: %+v, %v", snap, err)
	}
}

func TestRunnableProcsReadsTheRealTableOfThisHost(t *testing.T) {
	snap, err := doctor.RunnableProcs{}.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	self := os.Getpid()
	for _, p := range snap.Processes {
		if p.Pid == self {
			if !p.Running {
				t.Errorf("the test itself is running: %+v", p)
			}
			return
		}
	}
	t.Fatalf("the test's own pid %d is not in the table of %d processes", self, len(snap.Processes))
}
