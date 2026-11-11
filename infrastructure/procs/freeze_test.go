package procs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
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

// aTask adds a thread to a fake /proc/<pid>.
func aTask(t *testing.T, root string, pid, tid int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, fmt.Sprint(pid), "task", fmt.Sprint(tid)), 0o755); err != nil {
		t.Fatal(err)
	}
}

// mw-2891blf.8: the cwd and the story of the environment are what find a Builder's
// orphan, so the listing reads them.
func TestFreezerListsTheCwdAndTheStoryOfTheEnvironment(t *testing.T) {
	root := t.TempDir()
	aProc(t, root, 30, 1, 30, "S", "11", "0::/x\n", "node", "dev")
	if err := os.Symlink("/home/g/.mw-worktrees/mw-a.1/web", filepath.Join(root, "30", "cwd")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "30", "environ"), []byte("PATH=/bin\x00MW_STORY=mw-a.1\x00MW_STORYBOARD=no\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	aProc(t, root, 31, 1, 31, "S", "12", "0::/x\n", "sleep")
	got, err := Freezer{Proc: root}.List(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("listed %+v, %v", got, err)
	}
	for _, p := range got {
		switch p.Pid {
		case 30:
			if p.Cwd != "/home/g/.mw-worktrees/mw-a.1/web" || p.Story != "mw-a.1" {
				t.Errorf("process 30 = %+v, want its cwd and story", p)
			}
		case 31:
			if p.Cwd != "" || p.Story != "" {
				t.Errorf("process 31 = %+v, want no cwd and no story (nothing readable)", p)
			}
		}
	}
}

// mw-2891blf.6: lowering renices every thread to 19 and sets the io class to idle,
// and only for the process whose pid and start time are what the caller saw.
func TestFreezerLowersEveryThreadOfAProcessWhosePidAndStartMatch(t *testing.T) {
	root := t.TempDir()
	aProc(t, root, 40, 1, 1, "S", "900", "0::/x\n", "go", "build")
	aTask(t, root, 40, 40)
	aTask(t, root, 40, 41)
	aTask(t, root, 40, 42)
	var reniced, idled []int
	f := Freezer{Proc: root,
		Renice: func(tid, nice int) error {
			if nice != 19 {
				t.Errorf("reniced %d to %d, want 19", tid, nice)
			}
			reniced = append(reniced, tid)
			return nil
		},
		IoIdle: func(tid int) error { idled = append(idled, tid); return nil },
	}
	ctx := context.Background()
	if ok, err := f.Lower(ctx, 40, "123"); err != nil || ok {
		t.Fatalf("lower of a reused pid: %v %v, want nothing done", ok, err)
	}
	if ok, err := f.Lower(ctx, 99, "900"); err != nil || ok {
		t.Fatalf("lower of a pid not there: %v %v", ok, err)
	}
	if len(reniced) != 0 {
		t.Fatalf("reniced %v before a matching lower", reniced)
	}
	if ok, err := f.Lower(ctx, 40, "900"); err != nil || !ok {
		t.Fatalf("lower: %v %v", ok, err)
	}
	sort.Ints(reniced)
	sort.Ints(idled)
	want := []int{40, 41, 42}
	if !reflect.DeepEqual(reniced, want) || !reflect.DeepEqual(idled, want) {
		t.Fatalf("reniced %v and idled %v, want every thread %v", reniced, idled, want)
	}
}

// A process with no task directory to read (or a thread that ended) is lowered
// by its pid, and a thread that has ended since the listing is no error.
func TestFreezerLowerSurvivesAThreadThatEnded(t *testing.T) {
	root := t.TempDir()
	aProc(t, root, 41, 1, 1, "S", "5", "0::/x\n", "x")
	var reniced []int
	f := Freezer{Proc: root,
		Renice: func(tid, nice int) error {
			reniced = append(reniced, tid)
			return syscall.ESRCH
		},
		IoIdle: func(int) error { return syscall.ESRCH },
	}
	if ok, err := f.Lower(context.Background(), 41, "5"); err != nil || ok {
		t.Fatalf("lower of a process that ended: %v %v, want no error and nothing done", ok, err)
	}
	if !reflect.DeepEqual(reniced, []int{41}) {
		t.Fatalf("reniced %v, want the pid itself", reniced)
	}
}

func TestFreezerLowersARealProcess(t *testing.T) {
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
	if ok, err := f.Lower(context.Background(), cmd.Process.Pid, start); err != nil || !ok {
		t.Skipf("this kernel does not let the test lower its own child: %v %v", ok, err)
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	fields := strings.Fields(text[strings.LastIndex(text, ")")+1:])
	if fields[16] != "19" {
		t.Fatalf("nice = %s, want 19", fields[16])
	}
}

// The whole of give way over a fake /proc: a Builder's tool that setsid left
// behind, and a dispatch in a service cgroup, with the signals recorded.
func TestGiveWayOverAFakeProcStopsTheOrphanAndOnlyLowersTheDispatch(t *testing.T) {
	root := t.TempDir()
	aProc(t, root, 100, 1, 100, "S", "1", "0::/user.slice/tmux.scope\n", "tmux: server")
	aProc(t, root, 200, 100, 200, "S", "2", "0::/x\n", "/bin/sh")
	aProc(t, root, 201, 200, 200, "S", "3", "0::/x\n", "claude")
	aProc(t, root, 202, 200, 200, "S", "4", "0::/x\n", "mw", "next", "--heartbeat", "mw-a.1")
	aProc(t, root, 300, 1, 300, "S", "5", "0::/x\n", "node", "dev")
	if err := os.Symlink("/home/g/.mw-worktrees/mw-a.1/web", filepath.Join(root, "300", "cwd")); err != nil {
		t.Fatal(err)
	}
	aProc(t, root, 400, 1, 400, "S", "6", "0::/user.slice/mw-dispatch.service\n", "mw", "dispatch")
	aProc(t, root, 401, 400, 400, "S", "7", "0::/user.slice/mw-dispatch.service\n", "go", "build")
	aProc(t, root, 500, 1, 500, "S", "8", "0::/x\n", "firefox")
	aProc(t, root, 900, 1, 900, "R", "9", "0::/x\n", "mw", "giveway", "on")
	for _, pid := range []int{400, 401} {
		aTask(t, root, pid, pid)
	}
	var sent, reniced []string
	freezer := Freezer{Proc: root,
		Signal: func(pid int, sig syscall.Signal) error {
			sent = append(sent, fmt.Sprint(int(sig), " ", pid))
			return nil
		},
		Renice: func(tid, nice int) error { reniced = append(reniced, fmt.Sprint(tid)); return nil },
		IoIdle: func(int) error { return nil },
		Uid:    func() int { return os.Getuid() },
	}
	store := &memStore{}
	way := application.GiveWay{
		Processes: freezer, Panes: fixedPanes{{Session: "mw-a_1", Pid: 200}}, Marker: store, Log: store,
		Host: "desktop", Self: 900,
	}
	if err := way.On(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(sent)
	sort.Strings(reniced)
	stop := fmt.Sprint(int(syscall.SIGSTOP), " ")
	if want := []string{stop + "201", stop + "300"}; !reflect.DeepEqual(sent, want) {
		t.Fatalf("signals sent %v, want a stop of the harness 201 and the orphan 300 only", sent)
	}
	if want := []string{"400", "401"}; !reflect.DeepEqual(reniced, want) {
		t.Fatalf("reniced %v, want the dispatch and its build %v", reniced, want)
	}
}

type fixedPanes []application.GiveWayPane

func (f fixedPanes) Panes(context.Context) ([]application.GiveWayPane, error) { return f, nil }

type memStore struct {
	marker *application.GiveWayMarker
	lines  []string
}

func (m *memStore) Read(context.Context) (application.GiveWayMarker, bool, error) {
	if m.marker == nil {
		return application.GiveWayMarker{}, false, nil
	}
	return *m.marker, true, nil
}
func (m *memStore) Write(_ context.Context, marker application.GiveWayMarker) error {
	m.marker = &marker
	return nil
}
func (m *memStore) Remove(context.Context) error { m.marker = nil; return nil }
func (m *memStore) Append(_ context.Context, line string) error {
	m.lines = append(m.lines, line)
	return nil
}
func (m *memStore) Tail(context.Context, int) ([]string, error) { return m.lines, nil }
