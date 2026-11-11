package procs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// aProc writes one /proc/<pid> with a stat line of the real shape.
func aProc(t *testing.T, root string, pid, ppid, sid int, state, start, cgroup string, argv ...string) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stat := fmt.Sprintf("%d (a (odd) name) %s %d %d %d 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 %s 1000 100 0\n", pid, state, ppid, sid, sid, start)
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("stat", stat)
	write("cgroup", cgroup)
	cmd := ""
	for _, a := range argv {
		cmd += a + "\x00"
	}
	write("cmdline", cmd)
}

func TestFreezerListsStateParentSessionStartAndScope(t *testing.T) {
	root := t.TempDir()
	aProc(t, root, 10, 1, 10, "S", "555", "0::/user.slice/app.slice/app-tmux.slice/tmux-spawn-x.scope\n", "/bin/sh", "-c", "x")
	aProc(t, root, 11, 10, 10, "T", "777", "0::/user.slice/user@1000.service/app.slice/run-r42.scope\n", "go", "test")
	got, err := Freezer{Proc: root}.List(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("listed %+v, %v", got, err)
	}
	by := map[int]int{got[0].Pid: 0, got[1].Pid: 1}
	a, b := got[by[10]], got[by[11]]
	if a.Ppid != 1 || a.Sid != 10 || a.State != "S" || a.Start != "555" || a.Scope || len(a.Argv) != 3 {
		t.Errorf("process 10 = %+v", a)
	}
	if b.Ppid != 10 || b.State != "T" || b.Start != "777" || !b.Scope {
		t.Errorf("process 11 = %+v: want state T, start 777 and a run scope", b)
	}
}

// mw-2891blf.1: a pid another process has now is sent nothing.
func TestFreezerSendsToAProcessWhosePidAndStartBothMatchAndToNoOtherPid(t *testing.T) {
	root := t.TempDir()
	aProc(t, root, 20, 1, 20, "S", "900", "0::/x\n", "sleep")
	var sent []string
	f := Freezer{Proc: root, Signal: func(pid int, sig syscall.Signal) error {
		sent = append(sent, fmt.Sprint(pid, " ", int(sig)))
		return nil
	}}
	ctx := context.Background()
	if ok, err := f.Stop(ctx, 20, "900"); err != nil || !ok {
		t.Fatalf("stop of the same process: %v %v", ok, err)
	}
	if ok, err := f.Continue(ctx, 20, "123"); err != nil || ok {
		t.Fatalf("continue of a reused pid: %v %v, want nothing sent", ok, err)
	}
	if ok, err := f.Continue(ctx, 99, "900"); err != nil || ok {
		t.Fatalf("continue of a pid not there: %v %v", ok, err)
	}
	if want := []string{fmt.Sprint(20, " ", int(syscall.SIGSTOP))}; len(sent) != 1 || sent[0] != want[0] {
		t.Fatalf("sent %v, want only the stop of 20", sent)
	}
	aProc(t, root, 21, 1, 21, "Z", "5", "0::/x\n")
	if ok, _ := f.Stop(ctx, 21, "5"); ok {
		t.Fatal("a zombie was signalled")
	}
}

func TestFreezerStopsAndContinuesARealProcess(t *testing.T) {
	cmd := newSleeper(t)
	f := Freezer{}
	table, err := f.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var start string
	for _, p := range table {
		if p.Pid == cmd.Process.Pid {
			start = p.Start
		}
	}
	if start == "" {
		t.Fatal("the sleeper is not in the table")
	}
	if ok, err := f.Stop(context.Background(), cmd.Process.Pid, start); err != nil || !ok {
		t.Fatalf("stop: %v %v", ok, err)
	}
	if st, _ := readStat(fmt.Sprintf("/proc/%d/stat", cmd.Process.Pid)); st.State != "T" {
		// The kernel stops a sleeping process at once; give it the moment it needs.
		waitState(t, cmd.Process.Pid, "T")
	}
	if ok, err := f.Continue(context.Background(), cmd.Process.Pid, start); err != nil || !ok {
		t.Fatalf("continue: %v %v", ok, err)
	}
	waitState(t, cmd.Process.Pid, "S")
}

func newSleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skipf("no sleep to start: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	return cmd
}

func waitState(t *testing.T, pid int, want string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if st, ok := readStat(fmt.Sprintf("/proc/%d/stat", pid)); ok && st.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d never reached state %s", pid, want)
}
