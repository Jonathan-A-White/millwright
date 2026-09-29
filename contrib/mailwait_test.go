package contrib_test

// This test drives contrib/mail-wait, the wait the Mayor arms so that its
// harness wakes it when mail arrives, whatever sits on its prompt line. It runs
// in the same throwaway world as mail-notify's test (mailnotify_test.go): a
// private tmux socket, a temporary vault and state directory, and stand-ins for
// bd and mw on the front of PATH.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// waiting is one run of the script, started and not yet finished.
type waiting struct {
	f    *factory
	cmd  *exec.Cmd
	out  bytes.Buffer
	done chan error
}

// wait starts contrib/mail-wait in the factory's world, polling every second
// unless the test says otherwise in f.env, and returns without waiting for it.
func (f *factory) wait() *waiting {
	f.t.Helper()
	script, err := filepath.Abs("mail-wait")
	if err != nil {
		f.t.Fatal(err)
	}
	w := &waiting{f: f, done: make(chan error, 1)}
	w.cmd = exec.Command(script)
	w.cmd.Env = []string{
		"PATH=" + f.path("bin") + ":" + os.Getenv("PATH"),
		"HOME=" + f.dir,
		"TMUX_TMPDIR=" + os.Getenv("TMUX_TMPDIR"),
		"LC_ALL=C.UTF-8",
		"MW_TEST_DIR=" + f.dir,
		"MW_VAULT=" + f.path("vault"),
		"MW_TMUX_SOCKET=" + f.socket,
		"MW_MAIL_STATE_DIR=" + f.path("state"),
		"MW_MAIL_WAIT_EVERY=1",
	}
	w.cmd.Env = append(w.cmd.Env, f.env...)
	w.cmd.Stdout = &w.out
	w.cmd.Stderr = &w.out
	if err := w.cmd.Start(); err != nil {
		f.t.Fatal(err)
	}
	go func() { w.done <- w.cmd.Wait() }()
	f.t.Cleanup(func() { _ = w.cmd.Process.Kill() })
	return w
}

// ends waits up to d for the script to finish and returns what it printed. It
// fails the test if the script is still running then.
func (w *waiting) ends(d time.Duration) string {
	w.f.t.Helper()
	select {
	case err := <-w.done:
		if err != nil {
			w.f.t.Fatalf("mail-wait failed: %v\n%s", err, w.out.String())
		}
		return w.out.String()
	case <-time.After(d):
		w.f.t.Fatalf("mail-wait was still waiting after %s; it printed %q", d, w.out.String())
		return ""
	}
}

// stillWaiting fails the test if the script has finished within d.
func (w *waiting) stillWaiting(d time.Duration) {
	w.f.t.Helper()
	select {
	case err := <-w.done:
		w.f.t.Fatalf("mail-wait ended (%v) when nothing was new: %s", err, w.out.String())
	case <-time.After(d):
	}
}

func TestTheWaitEndsOnNewMailWhateverSitsOnThePromptAndTypesNothing(t *testing.T) {
	f := newFactory(t)
	f.mayor("has-text", actingByID)
	f.inbox()

	w := f.wait()
	w.stillWaiting(1500 * time.Millisecond)
	f.inbox("mw-aaa")
	out := w.ends(10 * time.Second)

	if !strings.Contains(out, "mw-aaa") || !strings.Contains(out, "NEW MAIL") {
		t.Fatalf("the wait told the Mayor %q, want the new mail named", out)
	}
	f.nothingMoreTyped("")
	if got := f.announced(); got != "mw-aaa\n" {
		t.Fatalf("recorded ids %q: the wait's own ending is the announcement", got)
	}
}

func TestTheWaitEndsAtOnceOnMailTheNotifierNeverAnnounced(t *testing.T) {
	f := newFactory(t)
	f.write("state/announced", "mw-old\n", 0o644)
	f.inbox("mw-old", "mw-new")

	out := f.wait().ends(10 * time.Second)

	if !strings.Contains(out, "mw-new") {
		t.Fatalf("the wait told the Mayor %q", out)
	}
	if got := f.announced(); got != "mw-new\nmw-old\n" && got != "mw-new\nmw-old" {
		t.Fatalf("recorded ids %q", got)
	}
}

func TestTheWaitIgnoresMailAlreadyAnnouncedAndEndsOnItsTimeLimit(t *testing.T) {
	f := newFactory(t)
	f.write("state/announced", "mw-old\n", 0o644)
	f.inbox("mw-old")
	f.env = append(f.env, "MW_MAIL_WAIT_LIMIT=2")

	out := f.wait().ends(15 * time.Second)

	if !strings.Contains(out, "No new mail") {
		t.Fatalf("the wait printed %q, want it to say it ended on its time limit", out)
	}
	if f.read("state/wait-armed") != "" {
		t.Fatal("a finished wait left its armed marker behind")
	}
}

func TestTheWaitEndsOnARisenPosternCountAndNotesIt(t *testing.T) {
	f := newFactory(t)
	f.posternKey()
	f.posternCount(0)
	f.write("state/postern-count", "0\n", 0o644)

	w := f.wait()
	w.stillWaiting(1500 * time.Millisecond)
	f.posternCount(2)
	out := w.ends(10 * time.Second)

	if !strings.Contains(out, "postern") || !strings.Contains(out, "2 unread") {
		t.Fatalf("the wait told the Mayor %q", out)
	}
	if got := strings.TrimSpace(f.read("state/postern-count")); got != "2" {
		t.Fatalf("postern count noted as %q, want 2", got)
	}
}

func TestTheWaitDoesNotEndOnAPosternCountItsHostAlreadyNoted(t *testing.T) {
	f := newFactory(t)
	f.posternKey()
	f.posternCount(3)
	f.write("state/postern-count", "3\n", 0o644)

	f.wait().stillWaiting(2500 * time.Millisecond)
}

func TestTheWaitMakesNoPosternCallOnAHostWithNoKeyFile(t *testing.T) {
	f := newFactory(t)
	f.env = append(f.env, "MW_MAIL_WAIT_LIMIT=2")

	f.wait().ends(15 * time.Second)

	if f.posternCalls() != 0 {
		t.Fatalf("mw postern was run: %q", f.read("postern.log"))
	}
}

func TestTheWaitRunsNoSyncWhereBeadsIsTheDoltServerItself(t *testing.T) {
	for _, mode := range []string{"backup", "shared", "auto"} {
		t.Run(mode, func(t *testing.T) {
			f := newFactory(t)
			f.env = append(f.env, "MW_MAIL_WAIT_LIMIT=2", "MW_BEADS_SYNC="+mode)
			f.wait().ends(15 * time.Second)
			if f.syncs() != 0 {
				t.Fatalf("the wait ran mw sync on a %s host: %q", mode, f.read("mw.log"))
			}
		})
	}
}

func TestTheWaitReadsTheBeadsSyncModeFromTheConfigFile(t *testing.T) {
	f := newFactory(t)
	f.write(".config/mw/config.toml", "vault = \"/x\"\nbeads_sync = \"backup\" # the desktop\n[hosts]\nbeads_sync = \"remote\"\n", 0o644)
	f.env = append(f.env, "MW_MAIL_WAIT_LIMIT=2")

	f.wait().ends(15 * time.Second)

	if f.syncs() != 0 {
		t.Fatalf("the wait ran mw sync on a backup host: %q", f.read("mw.log"))
	}
}

func TestTheWaitRunsNoSyncOnAnAutoModeFromTheConfigFile(t *testing.T) {
	f := newFactory(t)
	f.write(".config/mw/config.toml", "vault = \"/x\"\nbeads_sync = \"auto\"\n", 0o644)
	f.env = append(f.env, "MW_MAIL_WAIT_LIMIT=2")

	f.wait().ends(15 * time.Second)

	if f.syncs() != 0 {
		t.Fatalf("the wait ran mw sync on an auto host: %q", f.read("mw.log"))
	}
}

func TestTheWaitStillSyncsWhenBeadsSyncIsRemote(t *testing.T) {
	f := newFactory(t)
	f.env = append(f.env, "MW_MAIL_WAIT_LIMIT=2", "MW_BEADS_SYNC=remote")

	f.wait().ends(15 * time.Second)

	if f.syncs() != 1 {
		t.Fatalf("the wait ran mw sync %d times on a remote host, want once: %q", f.syncs(), f.read("mw.log"))
	}
}

func TestTheWaitSyncsOnAHostWithACopyOfItsOwnAndSharesTheNotifiersMarker(t *testing.T) {
	f := newFactory(t)
	f.env = append(f.env, "MW_MAIL_WAIT_LIMIT=3")

	f.wait().ends(15 * time.Second)
	if f.syncs() != 1 {
		t.Fatalf("the wait ran mw sync %d times, want once inside five minutes: %q", f.syncs(), f.read("mw.log"))
	}
	if f.read("state/last-sync") == "" {
		t.Fatal("the wait did not record its sync where the notifier reads it")
	}

	// The notifier's tick a moment later finds the sync recent and runs none.
	f.tick()
	if f.syncs() != 1 {
		t.Fatalf("the notifier synced again inside five minutes: %q", f.read("mw.log"))
	}
}

func TestAnArmedWaitLeavesAHeartbeatAndASecondWaitStepsAside(t *testing.T) {
	f := newFactory(t)
	first := f.wait()
	first.stillWaiting(1500 * time.Millisecond)
	if f.read("state/wait-armed") == "" {
		t.Fatal("an armed wait left no heartbeat for the notifier")
	}

	out := f.wait().ends(10 * time.Second)
	if !strings.Contains(out, "already") {
		t.Fatalf("the second wait printed %q, want it to say one is already armed", out)
	}
	first.stillWaiting(1200 * time.Millisecond)
}

func TestTheWaitScriptIsExecutableAndParses(t *testing.T) {
	info, err := os.Stat("mail-wait")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("contrib/mail-wait is not executable")
	}
	if out, err := exec.Command("sh", "-n", "mail-wait").CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
}

// heartbeat marks a wait as armed just now, or ago before it.
func (f *factory) armedWait(ago time.Duration) {
	f.t.Helper()
	f.write("state/wait-armed", strconv.FormatInt(time.Now().Add(-ago).Unix(), 10)+"\n", 0o644)
}

func TestTheNotifierLeavesMailToAnArmedWaitEvenOnAnIdlePrompt(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.armedWait(10 * time.Second)

	f.tick()

	f.nothingMoreTyped("")
	if got := f.announced(); got != "" {
		t.Fatalf("recorded %q for mail the wait has yet to announce", got)
	}
}

func TestTheNotifierLeavesAPosternRiseToAnArmedWait(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(2)
	f.armedWait(10 * time.Second)

	f.tick()

	f.nothingMoreTyped("")
	if got := f.read("state/postern-count"); got != "" {
		t.Fatalf("noted postern count %q the wait has yet to announce", got)
	}
}

func TestTheNotifierFallsBackToTypingWhenTheWaitsHeartbeatIsStale(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.armedWait(10 * time.Minute)

	f.tick()

	f.typed("New mail for mayor: 1 message(s). Run bd mail inbox.\n")
}

func TestTheNotifierStillTypesTheQuietAlarmWhileAWaitIsArmed(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.nudges([2]string{"long", "story mw-x has run long"})
	f.armedWait(10 * time.Second)

	f.tick()

	f.typed("Quiet alarm for mayor: story mw-x has run long. Run mw status.\n")
}
