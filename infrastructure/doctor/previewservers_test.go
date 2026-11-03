package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// aFakeProcess makes /proc/<pid> under proc with the command line and the
// working directory given (the link's own text, so " (deleted)" may be put on).
func aFakeProcess(t *testing.T, proc, pid, cwd string, args ...string) {
	t.Helper()
	dir := filepath.Join(proc, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.Join(args, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cwd, filepath.Join(dir, "cwd")); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewServersListsViteAndNpmPreviewsWithPortAndDirectory(t *testing.T) {
	proc := t.TempDir()
	aFakeProcess(t, proc, "760426", "/home/j/.mw-worktrees/mw-a.1 (deleted)", "node", "/home/j/.mw-worktrees/mw-a.1/node_modules/.bin/vite", "preview", "--port", "5205", "--strictPort")
	aFakeProcess(t, proc, "848355", "/home/j/.mw-worktrees/mw-b.2", "npm", "run", "preview", "--port", "5367", "--strictPort")
	aFakeProcess(t, proc, "848366", "/home/j/.mw-worktrees/mw-b.2", "sh", "-c", "vite preview --port=5367 --strictPort")
	aFakeProcess(t, proc, "900", "/home/j/postern", "vite", "preview")
	aFakeProcess(t, proc, "901", "/home/j/x", "vite", "build")
	aFakeProcess(t, proc, "902", "/home/j/x", "vim", "preview")
	aFakeProcess(t, proc, "903", "/home/j/x", "claude", "-p", "please run vite preview and check it")
	aFakeProcess(t, proc, "904", "/home/j/x", "npm", "run", "build")
	if err := os.MkdirAll(filepath.Join(proc, "self"), 0o755); err != nil {
		t.Fatal(err)
	}

	servers, err := PreviewServers{ProcDir: proc}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]application.PreviewServer{}
	for _, s := range servers {
		got[s.Pid] = s
	}
	if len(got) != 4 || got[760426].Port != 5205 || !got[760426].CwdDeleted || got[760426].Cwd != "/home/j/.mw-worktrees/mw-a.1" {
		t.Fatalf("expected the four previews, the first with port 5205 and a deleted directory, got %+v", got)
	}
	if got[848355].Port != 5367 || got[848355].CwdDeleted || got[848366].Port != 5367 || got[900].Port != 0 {
		t.Errorf("wrong ports or directories: %+v", got)
	}
}

func TestPreviewServersStopsOnlyThePidItWasListedAs(t *testing.T) {
	proc := t.TempDir()
	aFakeProcess(t, proc, "10", "/w/mw-a.1", "vite", "preview", "--port", "5205")
	aFakeProcess(t, proc, "11", "/w/mw-b.2", "vite", "preview", "--port", "5206")
	var signalled []int
	servers := PreviewServers{ProcDir: proc, Patience: time.Second, Signal: func(pid int, sig syscall.Signal) error {
		if sig != syscall.SIGTERM {
			t.Errorf("expected SIGTERM first, got %v", sig)
		}
		signalled = append(signalled, pid)
		return os.RemoveAll(filepath.Join(proc, "10"))
	}}
	listed, _ := servers.List(context.Background())
	var first application.PreviewServer
	for _, s := range listed {
		if s.Pid == 10 {
			first = s
		}
	}

	if err := servers.Stop(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if len(signalled) != 1 || signalled[0] != 10 {
		t.Fatalf("expected only pid 10 signalled, got %v", signalled)
	}
	if err := servers.Stop(context.Background(), first); err != nil || len(signalled) != 1 {
		t.Errorf("a second stop of a pid already gone is a no-op: err %v, signalled %v", err, signalled)
	}
	// A pid reused by another command is not touched.
	reused := first
	reused.Pid = 11
	if err := servers.Stop(context.Background(), reused); err != nil || len(signalled) != 1 {
		t.Errorf("a pid that is another command now is left alone: err %v, signalled %v", err, signalled)
	}
}

func TestPreviewServersKillsOneThatIgnoresTheFirstSignal(t *testing.T) {
	proc := t.TempDir()
	aFakeProcess(t, proc, "10", "/w/mw-a.1", "vite", "preview", "--port", "5205")
	var signals []syscall.Signal
	servers := PreviewServers{ProcDir: proc, Patience: 100 * time.Millisecond, Signal: func(pid int, sig syscall.Signal) error {
		signals = append(signals, sig)
		if sig == syscall.SIGKILL {
			return os.RemoveAll(filepath.Join(proc, "10"))
		}
		return nil
	}}
	listed, _ := servers.List(context.Background())
	if err := servers.Stop(context.Background(), listed[0]); err != nil {
		t.Fatal(err)
	}
	if len(signals) != 2 || signals[1] != syscall.SIGKILL {
		t.Errorf("expected SIGTERM then SIGKILL, got %v", signals)
	}
}
