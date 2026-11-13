package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// mw-2891blf.26: the Windows unlock task runs `wsl.exe --exec mw giveway on`, which
// gives mw no login environment: a PATH without ~/.local/bin (so no bd) and none of
// beads.env. mw gives itself both before it does anything else.

func aBareEnvironmentWithABeadsEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config", "mw"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := "# the served beads\nBEADS_DOLT_SERVER_HOST=10.88.0.2\nexport BEADS_DOLT_SERVER_PORT=3307\nBEADS_DOLT_PASSWORD=\"s3 cret\"\nBEADS_DOLT_SERVER_USER='mw'\n\nnot a assignment\n"
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "beads.env"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	for _, name := range []string{"BEADS_DOLT_SERVER_HOST", "BEADS_DOLT_SERVER_PORT", "BEADS_DOLT_PASSWORD", "BEADS_DOLT_SERVER_USER"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	return home
}

func TestLoginEnvironmentPutsBdsDirectoryOnPathAndReadsBeadsEnv(t *testing.T) {
	home := aBareEnvironmentWithABeadsEnv(t)
	t.Setenv("BEADS_DOLT_SERVER_PORT", "9999") // already set: beads.env does not override it

	ensureLoginEnvironment()

	path := os.Getenv("PATH")
	if !strings.HasPrefix(path, "/usr/bin:/bin:") || !strings.Contains(":"+path+":", ":"+filepath.Join(home, ".local", "bin")+":") {
		t.Errorf("PATH = %q, want the old PATH kept first and %s/.local/bin added", path, home)
	}
	for _, dir := range []string{"/usr/local/bin", "/snap/bin"} {
		if !strings.Contains(":"+path+":", ":"+dir+":") {
			t.Errorf("PATH = %q lacks %s", path, dir)
		}
	}
	if strings.Count(path, "/usr/bin") != 1 {
		t.Errorf("PATH = %q names /usr/bin more than once", path)
	}
	for name, want := range map[string]string{
		"BEADS_DOLT_SERVER_HOST": "10.88.0.2",
		"BEADS_DOLT_SERVER_PORT": "9999",
		"BEADS_DOLT_PASSWORD":    "s3 cret",
		"BEADS_DOLT_SERVER_USER": "mw",
	} {
		if got := os.Getenv(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestLoginEnvironmentWithNoBeadsEnvOnlyFixesPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "")
	ensureLoginEnvironment()
	if !strings.Contains(":"+os.Getenv("PATH")+":", ":/usr/bin:") {
		t.Errorf("PATH = %q, want the standard directories", os.Getenv("PATH"))
	}
}

func TestEveryGiveWayVerbRunsWithTheLoginEnvironment(t *testing.T) {
	aBareEnvironmentWithABeadsEnv(t)
	was := loginEnvironment
	loginEnvironment = ensureLoginEnvironment
	t.Cleanup(func() { loginEnvironment = was })
	t.Setenv("MW_GIVEWAY_DIR", t.TempDir())
	t.Setenv("MW_HOST", "desktop")
	if out, err := runGiveWay("giveway", "mark", "--by", "windows-unlock"); err != nil {
		t.Fatalf("mw giveway mark: %v: %s", err, out)
	}
	if os.Getenv("BEADS_DOLT_PASSWORD") != "s3 cret" || !strings.Contains(os.Getenv("PATH"), ".local/bin") {
		t.Errorf("mw giveway ran without the login environment: PATH=%q password set=%v", os.Getenv("PATH"), os.Getenv("BEADS_DOLT_PASSWORD") != "")
	}
}

// mw-2891blf.26, AC 2: the real mw, run with a bare environment as wsl.exe --exec
// runs it (a PATH that names no tmux, no beads.env read), stops the processes below a
// Builder session's pane. The tmux server is a private one (its own TMUX_TMPDIR) and
// the run names its session, so nothing of the host's own is touched.
func TestMwGiveWayWithABareEnvironmentStopsTheSessionsProcesses(t *testing.T) {
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux on this host")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "mw")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building mw: %v\n%s", err, out)
	}
	home := filepath.Join(dir, "home")
	tmp := filepath.Join(dir, "tmux")
	state := filepath.Join(dir, "state")
	for _, d := range []string{home, tmp, state} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bare := []string{"HOME=" + home, "PATH=/nonexistent", "TMUX_TMPDIR=" + tmp, "MW_HOST=desktop", "MW_GIVEWAY_DIR=" + state}
	pidFile := filepath.Join(dir, "pid")
	start := exec.Command(tmux, "new-session", "-d", "-s", "mw-a_1", "/bin/sh -c '/bin/sleep 600 & echo $! > "+pidFile+"; wait'")
	start.Env = bare
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		kill := exec.Command(tmux, "kill-server")
		kill.Env = bare
		kill.Run()
	})
	var pid string
	for i := 0; i < 100 && pid == ""; i++ {
		if raw, err := os.ReadFile(pidFile); err == nil {
			pid = strings.TrimSpace(string(raw))
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if pid == "" {
		t.Fatal("the session's process did not start")
	}
	stateOf := func() string {
		raw, _ := os.ReadFile("/proc/" + pid + "/stat")
		fields := strings.Fields(string(raw[strings.LastIndex(string(raw), ")")+1:]))
		if len(fields) == 0 {
			return "gone"
		}
		return fields[0]
	}
	if got := stateOf(); got == "T" {
		t.Fatalf("process %s is stopped before give way", pid)
	}

	on := exec.Command(bin, "giveway", "on", "--session", "mw-a_1", "--by", "windows-unlock")
	on.Env = bare
	if out, err := on.CombinedOutput(); err != nil {
		t.Fatalf("mw giveway on with a bare environment: %v\n%s", err, out)
	}
	if got := stateOf(); got != "T" {
		t.Errorf("process %s of the session is in state %q after mw giveway on, want T (stopped)", pid, got)
	}
	off := exec.Command(bin, "giveway", "off", "--session", "mw-a_1")
	off.Env = bare
	if out, err := off.CombinedOutput(); err != nil {
		t.Fatalf("mw giveway off with a bare environment: %v\n%s", err, out)
	}
	if got := stateOf(); got == "T" {
		t.Errorf("process %s is still stopped after mw giveway off", pid)
	}
}
