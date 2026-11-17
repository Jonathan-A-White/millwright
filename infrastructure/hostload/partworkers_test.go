package hostload_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
)

// aProcess puts a process into a fake /proc: its command line, and the children
// each of its threads started.
func aProcess(t *testing.T, proc, pid string, args []string, children map[string]string) {
	t.Helper()
	dir := filepath.Join(proc, pid)
	if err := os.MkdirAll(filepath.Join(dir, "task", pid), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.Join(args, "\x00")+"\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	for task, kids := range children {
		if err := os.MkdirAll(filepath.Join(dir, "task", task), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "task", task, "children"), []byte(kids), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// mw-gq6.1422: the reading counts the factory's own part workers running their
// part, so that a wait for a calm host can take their share off.
func TestProcCountsThePartWorkersRunningTheirPart(t *testing.T) {
	dir := t.TempDir()
	load := filepath.Join(dir, "loadavg")
	if err := os.WriteFile(load, []byte("26.00 22.00 20.00 9/2000 1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proc := filepath.Join(dir, "proc")
	worker := []string{"/home/u/millwright/bin/mw", "check", "mw-a.1", "--part", "2", "--part-worker", "--no-slot"}
	// Two running their part's command, one of them from a thread other than its first.
	aProcess(t, proc, "100", worker, map[string]string{"100": "101 "})
	aProcess(t, proc, "110", worker, map[string]string{"110": "", "113": "112 "})
	// One that takes the merge slot, so is not a --no-slot worker.
	aProcess(t, proc, "200", []string{"/home/u/millwright/bin/mw", "check", "mw-b.1", "--part", "3", "--part-worker", "--wait", "1h0m0s"}, map[string]string{"200": "201 "})
	// One with no child yet: waiting for a test-runner slot, its part not started.
	aProcess(t, proc, "300", worker, map[string]string{"300": ""})
	// A search that names the flags is not a worker.
	aProcess(t, proc, "400", []string{"grep", "--part-worker", "--no-slot"}, map[string]string{"400": "401 "})
	// The call that started the workers is not one.
	aProcess(t, proc, "500", []string{"/home/u/millwright/bin/mw", "check", "mw-a.1", "--part", "2"}, map[string]string{"500": "501 "})
	if err := os.MkdirAll(filepath.Join(proc, "self"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := hostload.Proc{File: load, Cores: 20, ProcDir: proc}.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.PartWorkers != 2 {
		t.Errorf("expected the two part workers running their part counted, got %d", got.PartWorkers)
	}
	if got.Busy() || !got.OverCores() {
		t.Errorf("expected 26 of 20 with two part workers over its cores but not busy, got %+v", got)
	}
}

func TestProcCountsNoPartWorkerWhereThereIsNoProcToRead(t *testing.T) {
	load := filepath.Join(t.TempDir(), "loadavg")
	if err := os.WriteFile(load, []byte("26.00 22.00 20.00 9/2000 1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := hostload.Proc{File: load, Cores: 20, ProcDir: filepath.Join(t.TempDir(), "none")}.Load(context.Background())
	if err != nil || got.PartWorkers != 0 || !got.Busy() {
		t.Errorf("expected a reading with no part worker counted and the host busy, got %+v, %v", got, err)
	}
}
