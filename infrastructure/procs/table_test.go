package procs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeProc writes one process into a fake /proc: its status (uid, parent,
// state), its command line, and its working directory as a link.
func fakeProc(t *testing.T, proc string, pid, ppid, uid int, cwd string, argv ...string) {
	t.Helper()
	dir := filepath.Join(proc, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	status := fmt.Sprintf("Name:\tx\nState:\tS (sleeping)\nPPid:\t%d\nUid:\t%d\t%d\t%d\t%d\n", ppid, uid, uid, uid, uid)
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cwd != "" {
		if err := os.Symlink(cwd, filepath.Join(dir, "cwd")); err != nil {
			t.Fatal(err)
		}
	}
}

// mw-gq6.409: the table lists the user's processes with command line and
// working directory, a deleted cwd without its mark, and never the process mw
// runs as, a process it descends from, or another user's.
func TestListReadsTheUsersProcessesButNotMwOrItsAncestorsOrAnotherUsers(t *testing.T) {
	proc := t.TempDir()
	fakeProc(t, proc, 1, 0, 0, "/", "init")
	fakeProc(t, proc, 100, 1, 1000, "/home/u", "claude", "-p", "mentions /tmp/tester-mw-x.1/ in its prompt")
	fakeProc(t, proc, 200, 100, 1000, "/tmp/tester-mw-x.1/app", "mw", "next", "mw-x.1")
	fakeProc(t, proc, 300, 1, 1000, "/tmp/tester-mw-x.1/app (deleted)", "node", "/tmp/tester-mw-x.1/app/node_modules/.bin/vite", "preview")
	fakeProc(t, proc, 400, 1, 2000, "/tmp/tester-mw-x.1/app", "node", "someone else's")

	got, err := Table{Proc: proc, Uid: func() int { return 1000 }, Self: 200}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Pid != 300 {
		t.Fatalf("expected only process 300 listed, got %+v", got)
	}
	if got[0].Cwd != "/tmp/tester-mw-x.1/app" || got[0].Argv[len(got[0].Argv)-1] != "preview" {
		t.Errorf("expected the cwd without its deleted mark and the whole command line, got %+v", got[0])
	}
	if !got[0].Under("/tmp/tester-mw-x.1/") || got[0].Under("/tmp/tester-mw-x.10/") {
		t.Errorf("expected process 300 under /tmp/tester-mw-x.1/ and not under /tmp/tester-mw-x.10/")
	}
}

// A process that ends on TERM is not killed; one that outlasts the grace is.
func TestStopTermsFirstAndKillsOnlyWhatOutlastsTheGrace(t *testing.T) {
	proc := t.TempDir()
	fakeProc(t, proc, 11, 1, 1000, "", "polite")
	fakeProc(t, proc, 12, 1, 1000, "", "stubborn")
	var sent []string
	slept := 0
	table := Table{
		Proc: proc,
		Signal: func(pid int, sig syscall.Signal) error {
			sent = append(sent, fmt.Sprintf("%d:%v", pid, sig))
			if sig == syscall.SIGKILL || pid == 11 {
				return os.RemoveAll(filepath.Join(proc, fmt.Sprint(pid)))
			}
			return nil
		},
		Sleep: func(time.Duration) { slept++ },
	}
	for _, pid := range []int{11, 12} {
		if err := table.Stop(context.Background(), pid); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"11:terminated", "12:terminated", "12:killed"}
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("expected signals %v, got %v", want, sent)
	}
	if slept != 50 {
		t.Errorf("expected 50 looks (five seconds, a tenth at a time) for the stubborn one, got %d", slept)
	}
}

// A process already gone is no error, and a zombie counts as gone.
func TestStopOfAProcessAlreadyGoneIsNoError(t *testing.T) {
	proc := t.TempDir()
	table := Table{Proc: proc, Signal: func(int, syscall.Signal) error { return syscall.ESRCH }, Sleep: func(time.Duration) {}}
	if err := table.Stop(context.Background(), 77); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	fakeProc(t, proc, 78, 1, 1000, "", "zombie")
	status := filepath.Join(proc, "78", "status")
	if err := os.WriteFile(status, []byte("State:\tZ (zombie)\nUid:\t1000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var sent int
	table.Signal = func(int, syscall.Signal) error { sent++; return nil }
	if err := table.Stop(context.Background(), 78); err != nil || sent != 1 {
		t.Errorf("expected TERM only and no error for a zombie, got %d signals and %v", sent, err)
	}
}
