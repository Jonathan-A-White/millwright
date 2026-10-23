package contrib_test

// These tests drive contrib/mw-under-load (mw-gq6.483): one bounded way to run
// a check under load, so no Builder starts sixteen busy loops of its own. The
// host's load and cores are read from settings, never from the box the test
// runs on, and a few loops live for a second at a time at most.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// underLoad starts contrib/mw-under-load with a made-up load and core count.
// The stand-in load file is written once per call.
func underLoad(t *testing.T, load string, cores int, extra []string, args ...string) *exec.Cmd {
	t.Helper()
	dir := t.TempDir()
	loadavg := filepath.Join(dir, "loadavg")
	if err := os.WriteFile(loadavg, []byte(load+" 0.50 0.50 1/100 12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := command(t, "./mw-under-load", args...)
	cmd.Env = append(os.Environ(),
		"MW_UNDER_LOAD_LOADAVG="+loadavg,
		"MW_UNDER_LOAD_CORES="+strconv.Itoa(cores),
		"MW_UNDER_LOAD_MAX=",
		"MW_UNDER_LOAD_SECONDS=",
	)
	cmd.Env = append(cmd.Env, extra...)
	return cmd
}

// loopsOf lists the pids of the busy loops (the timeout around each) that the
// mw-under-load with this pid started and that are still alive.
func loopsOf(t *testing.T, pid int) []string {
	t.Helper()
	out, _ := command(t, "pgrep", "-f", "mw-under-load-loop "+strconv.Itoa(pid)+"$").Output()
	var loops []string
	for _, p := range strings.Fields(string(out)) {
		comm, err := os.ReadFile("/proc/" + p + "/comm")
		if err == nil && strings.TrimSpace(string(comm)) == "timeout" {
			loops = append(loops, p)
		}
	}
	return loops
}

// anyLeft reports every process still carrying this run's loop marker.
func anyLeft(t *testing.T, pid int) string {
	out, _ := command(t, "pgrep", "-f", "mw-under-load-loop "+strconv.Itoa(pid)+"$").Output()
	return strings.TrimSpace(string(out))
}

// waitFor polls for cond until settleBudget has passed, by the clock: a poll
// that itself runs slowly (a pgrep over a busy host) does not stretch it.
func waitFor(cond func() bool) bool {
	return waitWithin(settleBudget, cond)
}

// waitWithin polls for cond until d has passed.
func waitWithin(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitLoops waits until the mw-under-load with this pid has n busy loops, each
// already running as `timeout`: the command can start before the last loop has
// exec'd, so a count taken the moment the command is up is short on a busy box.
// It returns what it last saw, and stops early when there are more than n.
func waitLoops(t *testing.T, pid, n int) []string {
	t.Helper()
	var got []string
	waitFor(func() bool {
		got = loopsOf(t, pid)
		return len(got) >= n
	})
	return got
}

// killLoops ends any loop of this mw-under-load still alive, so a test that
// found loops outliving a KILL does not leave them running to their deadline.
func killLoops(t *testing.T, pid int) {
	t.Helper()
	out, _ := command(t, "pgrep", "-f", "mw-under-load-loop "+strconv.Itoa(pid)+"$").Output()
	for _, p := range strings.Fields(string(out)) {
		if n, err := strconv.Atoi(p); err == nil {
			_ = syscall.Kill(n, syscall.SIGKILL)
		}
	}
}

// startHolding starts a mw-under-load whose command writes its pid to a file,
// then waits for a release file the test makes, and returns once it has started.
func startHolding(t *testing.T, load string, cores int, extra ...string) (*exec.Cmd, string) {
	t.Helper()
	dir := t.TempDir()
	started, release := filepath.Join(dir, "started"), filepath.Join(dir, "release")
	script := `echo $$ >"$1"; n=0; while [ ! -f "$2" ] && [ $n -lt 300 ]; do n=$((n+1)); sleep 0.1; done`
	cmd := underLoad(t, load, cores, extra, "sh", "-c", script, "x", started, release)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(release, nil, 0o644)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		// The command may outlive a KILLed mw-under-load: let it see the
		// release before the temporary directory is removed.
		if raw, err := os.ReadFile(started); err == nil {
			waitFor(func() bool { _, err := os.Stat("/proc/" + strings.TrimSpace(string(raw))); return err != nil })
		}
	})
	if !waitFor(func() bool { raw, err := os.ReadFile(started); return err == nil && len(raw) > 0 }) {
		t.Fatal("the command never started")
	}
	return cmd, release
}

func TestUnderLoadStartsAtMostFourLoopsAndEndsThemWhenTheCommandEnds(t *testing.T) {
	cmd, release := startHolding(t, "1.00", 16)
	pid := cmd.Process.Pid
	if got := waitLoops(t, pid, 4); len(got) != 4 {
		t.Fatalf("a host with 15 free cores should get 4 loops, got %d: %v", len(got), got)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the command succeeded, mw-under-load should too: %v", err)
	}
	if left := anyLeft(t, pid); left != "" {
		t.Fatalf("loops left after the command ended: %s", left)
	}
}

func TestUnderLoadStartsAsManyLoopsAsWholeFreeCores(t *testing.T) {
	cmd, _ := startHolding(t, "4.50", 6)
	if got := waitLoops(t, cmd.Process.Pid, 1); len(got) != 1 {
		t.Fatalf("1.5 free cores should give 1 loop, got %d: %v", len(got), got)
	}
}

func TestUnderLoadStartsNoLoopOnAHostAtOrOverItsCoresAndRunsTheCommandPlain(t *testing.T) {
	for _, load := range []string{"8.00", "44.12"} {
		cmd := underLoad(t, load, 8, nil, "sh", "-c", `echo ran; exit 3`)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 3 {
			t.Fatalf("load %s: wanted the command's exit 3, got %v", load, err)
		}
		if strings.TrimSpace(string(out)) != "ran" {
			t.Fatalf("load %s: the command did not run plain: %q", load, out)
		}
		if !strings.Contains(stderr.String(), "at or over its 8 cores") || !strings.Contains(stderr.String(), "no busy loops") {
			t.Fatalf("load %s: it should say so: %q", load, stderr.String())
		}
	}
}

func TestUnderLoadStartsNoLoopWhenLessThanOneCoreIsFree(t *testing.T) {
	cmd := underLoad(t, "7.40", 8, nil, "true")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "no busy loops") {
		t.Fatalf("it should say it ran plain: %q", stderr.String())
	}
}

func TestUnderLoadKeepsTheCommandsInputAndExitStatus(t *testing.T) {
	cmd := underLoad(t, "0.10", 2, []string{"MW_UNDER_LOAD_MAX=1"}, "sh", "-c", `cat; exit 5`)
	cmd.Stdin = strings.NewReader("hello\n")
	out, err := cmd.Output()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 5 {
		t.Fatalf("wanted exit 5, got %v", err)
	}
	if string(out) != "hello\n" {
		t.Fatalf("the command lost its stdin: %q", out)
	}
}

func TestUnderLoadEndsEveryLoopWhenItIsKilledWithTermOrInt(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		cmd, _ := startHolding(t, "1.00", 8, "MW_UNDER_LOAD_MAX=2")
		pid := cmd.Process.Pid
		if got := waitLoops(t, pid, 2); len(got) != 2 {
			t.Fatalf("%v: wanted 2 loops before the kill, got %v", sig, got)
		}
		if err := cmd.Process.Signal(sig); err != nil {
			t.Fatal(err)
		}
		_ = cmd.Wait()
		if !waitFor(func() bool { return anyLeft(t, pid) == "" }) {
			t.Fatalf("%v: loops left after mw-under-load was killed: %s", sig, anyLeft(t, pid))
		}
	}
}

// A KILL cannot be trapped, so the loops must see their parent gone. Their own
// deadline is set far past the wait, so a loop that only ends at its deadline
// fails here: the wait is settleBudget (30 s) however loaded the host, the
// deadline 300 s. (With the deadline at 3 s, as this test once had it, a loop
// with no parent check at all ended in time and the test passed: mw-gq6.715.)
func TestUnderLoadLoopsEndOnTheirOwnWhenItIsKilledOutright(t *testing.T) {
	cmd, release := startHolding(t, "1.00", 8, "MW_UNDER_LOAD_MAX=2", "MW_UNDER_LOAD_SECONDS=300")
	pid := cmd.Process.Pid
	t.Cleanup(func() { killLoops(t, pid) })
	if got := waitLoops(t, pid, 2); len(got) != 2 {
		t.Fatalf("wanted 2 loops before the kill, got %v", got)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	_ = os.WriteFile(release, nil, 0o644)
	start := time.Now()
	if !waitFor(func() bool { return anyLeft(t, pid) == "" }) {
		t.Fatalf("loops outlived a KILLed mw-under-load by %v: %s", time.Since(start).Round(time.Millisecond), anyLeft(t, pid))
	}
	t.Logf("the loops saw their parent gone and ended %v after the KILL", time.Since(start).Round(time.Millisecond))
}

func TestUnderLoadEachLoopCarriesItsOwnDeadline(t *testing.T) {
	cmd, _ := startHolding(t, "1.00", 8, "MW_UNDER_LOAD_MAX=1", "MW_UNDER_LOAD_SECONDS=99999")
	loops := waitLoops(t, cmd.Process.Pid, 1)
	if len(loops) != 1 {
		t.Fatalf("wanted 1 loop, got %v", loops)
	}
	line, err := os.ReadFile("/proc/" + loops[0] + "/cmdline")
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimRight(string(line), "\x00"), "\x00")
	if len(argv) < 2 || argv[0] != "timeout" || argv[1] != "1800" {
		t.Fatalf("a loop should run under timeout 1800 (30 min at most), got %q", argv)
	}
}

func TestUnderLoadLoopsEndAtTheirDeadlineWhileTheCommandStillRuns(t *testing.T) {
	cmd, _ := startHolding(t, "1.00", 8, "MW_UNDER_LOAD_MAX=2", "MW_UNDER_LOAD_SECONDS=1")
	pid := cmd.Process.Pid
	if !waitFor(func() bool { return len(loopsOf(t, pid)) == 0 }) {
		t.Fatalf("loops still running long after their 1 s deadline: %v", loopsOf(t, pid))
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("the command should still be running when the loops reach their deadline: %v", err)
	}
}

func TestUnderLoadScriptIsExecutableAndParses(t *testing.T) {
	info, err := os.Stat("mw-under-load")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("contrib/mw-under-load is not executable")
	}
	if out, err := command(t, "sh", "-n", "mw-under-load").CombinedOutput(); err != nil {
		t.Fatalf("does not parse: %v %s", err, out)
	}
}

// A command that ends at once has its loops stopped the moment they start,
// when a TERM can be lost before `timeout` takes it up: mw-under-load must
// still end with its command, not wait out the loops' deadline. Eight runs
// side by side, ten times over, make the busy box the gate saw.
func TestUnderLoadEndsAtOnceWhenTheCommandEndsAsItsLoopsStart(t *testing.T) {
	failed := make(chan string, 80)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if msg := endsWithItsCommand(t); msg != "" {
					failed <- msg
					return
				}
			}
		}()
	}
	wg.Wait()
	close(failed)
	for msg := range failed {
		t.Error(msg)
	}
}

// endsWithItsCommand runs `true` beside four loops and says what went wrong, if anything.
func endsWithItsCommand(t *testing.T) string {
	cmd := underLoad(t, "1.00", 16, []string{"MW_UNDER_LOAD_SECONDS=600"}, "true")
	if err := cmd.Start(); err != nil {
		return err.Error()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	// The loops' deadline is 600 s, well past this wait: a mw-under-load that
	// waited out its loops would still be waiting here. 45 s keeps under the
	// 60 s scriptDeadline that command() puts on the run.
	select {
	case err := <-done:
		if err != nil {
			return fmt.Sprintf("the command succeeded, mw-under-load should too: %v", err)
		}
	case <-time.After(45 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		killLoops(t, cmd.Process.Pid)
		return "mw-under-load still waiting on its loops 45 s after its command ended"
	}
	// mw-under-load has stopped its loops and waited for them, but a loop whose
	// TERM was lost before `timeout` took it up (see its header) is a busy shell
	// that ends once it is next scheduled: seen on the Laptop at load 115 of 20
	// cores, a `timeout` left for a moment after mw-under-load had exited and gone
	// within 300 ms. So the loops get the settle budget to be gone, not one look.
	if !waitFor(func() bool { return anyLeft(t, cmd.Process.Pid) == "" }) {
		left := anyLeft(t, cmd.Process.Pid)
		killLoops(t, cmd.Process.Pid)
		return "loops left after the command ended: " + left
	}
	return ""
}
