package contrib_test

// This test drives contrib/mail-notify, the script that types one line into the
// live Mayor's tmux window when mail arrives. It never touches tmux's default
// server, the real vault, the real beads or the real mw: the script is run with
// a private tmux socket (MW_TMUX_SOCKET), a temporary vault and state
// directory, and stand-ins for bd and mw on the front of PATH. Nothing here may
// be changed to run without them.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	announcement = "New mail for mayor: %d message(s). Run bd mail inbox.\n"

	// The empty input line of Claude Code: the prompt mark and a no-break space.
	emptyPrompt = `printf '\342\235\257\302\240\n'`
)

// A pane script for each state the Mayor's window can be in. Each ends in a cat
// that writes whatever is typed into it to $MW_TEST_DIR/typed, so that a test
// can say exactly what reached the window.
var panes = map[string]string{
	"idle":     emptyPrompt,
	"busy":     `printf 'working (esc to interrupt)\n'; ` + emptyPrompt,
	"has-text": `printf '\342\235\257 a half-written thought'`,
}

// factory is one throwaway world for the script to run in.
type factory struct {
	t      *testing.T
	dir    string // MW_TEST_DIR: stand-ins, their logs, the vault, the state
	socket string
	script string
	env    []string // more of the script's environment, for a test to set
}

func newFactory(t *testing.T) *factory {
	t.Helper()
	for _, program := range []string{"tmux", "flock"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skipf("%s is not on PATH", program)
		}
	}
	script, err := filepath.Abs("mail-notify")
	if err != nil {
		t.Fatal(err)
	}
	f := &factory{
		t:      t,
		dir:    t.TempDir(),
		socket: fmt.Sprintf("mw-test-mailnotify-%d-%d", os.Getpid(), time.Now().UnixNano()),
		script: script,
	}
	t.Cleanup(func() {
		// kill-server fails when the server has already stopped: that is fine.
		_ = exec.Command("tmux", "-L", f.socket, "kill-server").Run()
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), f.socket))
	})

	f.write("bin/bd", "#!/bin/sh\necho \"$*\" >> \"$MW_TEST_DIR/bd.log\"\n"+
		"[ \"$1 $2\" = \"mail inbox\" ] && cat \"$MW_TEST_DIR/inbox\"\nexit 0\n", 0o755)
	f.write("bin/mw", "#!/bin/sh\ncase \"$1\" in\n"+
		"sync) echo \"$*\" >> \"$MW_TEST_DIR/mw.log\" ;;\n"+
		"nudge) echo \"$*\" >> \"$MW_TEST_DIR/nudge.log\"; [ -f \"$MW_TEST_DIR/nudge-output\" ] && cat \"$MW_TEST_DIR/nudge-output\" ;;\n"+
		"esac\nexit 0\n", 0o755)
	f.write("vault/.mayor-acting", "", 0o644)
	f.write("loadavg", "0.10 0.10 0.10 1/100 1\n", 0o644)
	f.inbox()
	return f
}

func (f *factory) path(rel string) string { return filepath.Join(f.dir, rel) }

func (f *factory) write(rel, content string, mode os.FileMode) {
	f.t.Helper()
	p := f.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		f.t.Fatal(err)
	}
}

// read is a file's content, or "" when it is not there.
func (f *factory) read(rel string) string {
	b, err := os.ReadFile(f.path(rel))
	if err != nil {
		return ""
	}
	return string(b)
}

// inbox sets what `bd mail inbox` lists, one message id a line, each followed
// by a subject as the real one prints it.
func (f *factory) inbox(ids ...string) {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(id + "  a subject that is not to be typed\n")
	}
	f.write("inbox", b.String(), 0o644)
}

func (f *factory) load(l string) { f.write("loadavg", l+" 0.10 0.10 1/100 1\n", 0o644) }

// nudges sets what `mw nudge` prints, one clause a line, key and text tab
// separated, as the real one would: mw nudge's own output shape.
func (f *factory) nudges(rows ...[2]string) {
	var b strings.Builder
	for _, row := range rows {
		b.WriteString(row[0] + "\t" + row[1] + "\n")
	}
	f.write("nudge-output", b.String(), 0o644)
}

// announced is the ids the script has recorded as told.
func (f *factory) announced() string { return f.read("state/announced") }

func (f *factory) tmux(args ...string) string {
	f.t.Helper()
	out, err := exec.Command("tmux", append([]string{"-L", f.socket}, args...)...).CombinedOutput()
	if err != nil {
		f.t.Fatalf("tmux %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// mayor starts the Mayor's window on the private server, in the named state,
// and says in .mayor-acting where it is. It returns the window id. The state
// is one of panes; in the acting text, ID and INDEX stand for the window's id
// and index.
func (f *factory) mayor(state, acting string) string {
	f.t.Helper()
	f.write("pane.sh", "#!/bin/sh\n"+panes[state]+"\nexec cat > \"$MW_TEST_DIR/typed\"\n", 0o755)
	// A window of another name first, so the Mayor's is neither index 0 nor the
	// first the server lists.
	f.tmux("new-session", "-d", "-s", "factory", "-n", "shell", "-x", "100", "-y", "20", "sleep 600")
	out := f.tmux("new-window", "-d", "-P", "-F", "#{window_id} #{window_index}", "-n", "mayor-2026-09-19-10",
		"env", "MW_TEST_DIR="+f.dir, f.path("pane.sh"))
	var id, index string
	if _, err := fmt.Sscan(out, &id, &index); err != nil {
		f.t.Fatalf("reading the new window from %q: %v", out, err)
	}
	f.write("vault/.mayor-acting", strings.NewReplacer("ID", id, "INDEX", index).Replace(acting)+"\n", 0o644)

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(f.tmux("capture-pane", "-p", "-t", id), "❯") {
		if time.Now().After(deadline) {
			f.t.Fatalf("the pane never drew its prompt: %q", f.tmux("capture-pane", "-p", "-t", id))
		}
		time.Sleep(20 * time.Millisecond)
	}
	return id
}

// tick runs the script once, as the timer would.
func (f *factory) tick() string {
	f.t.Helper()
	cmd := exec.Command(f.script)
	cmd.Env = []string{
		"PATH=" + f.path("bin") + ":" + os.Getenv("PATH"),
		"HOME=" + f.dir,
		"TMUX_TMPDIR=" + os.Getenv("TMUX_TMPDIR"),
		"LC_ALL=C.UTF-8",
		"MW_TEST_DIR=" + f.dir,
		"MW_VAULT=" + f.path("vault"),
		"MW_TMUX_SOCKET=" + f.socket,
		"MW_MAIL_STATE_DIR=" + f.path("state"),
		"MW_MAIL_LOADAVG_FILE=" + f.path("loadavg"),
	}
	cmd.Env = append(cmd.Env, f.env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("mail-notify failed: %v\n%s", err, out)
	}
	return string(out)
}

// typed is what has reached the Mayor's window as input. Typing goes through a
// tty and a cat, so it is given a moment to arrive.
func (f *factory) typed(want string) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.read("typed") != want {
		if time.Now().After(deadline) {
			f.t.Fatalf("the window was typed %q, want %q", f.read("typed"), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// nothingMoreTyped is the negative: after the script has finished, and input has had
// time to arrive, the window has been typed nothing beyond want.
func (f *factory) nothingMoreTyped(want string) {
	f.t.Helper()
	time.Sleep(700 * time.Millisecond)
	if got := f.read("typed"); got != want {
		f.t.Fatalf("the window was typed %q, want %q", got, want)
	}
}

func (f *factory) syncs() int      { return strings.Count(f.read("mw.log"), "\n") }
func (f *factory) bdCalls() int    { return strings.Count(f.read("bd.log"), "\n") }
func (f *factory) nudgeCalls() int { return strings.Count(f.read("nudge.log"), "\n") }

const actingByID = "Mayor after handoff 10 (window ID)"

func TestNewMailAndAnIdlePromptTypesOneLineAndRecordsTheIds(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa", "mw-bbb")

	f.tick()

	f.typed(fmt.Sprintf(announcement, 2))
	if got := f.announced(); got != "mw-aaa\nmw-bbb\n" {
		t.Fatalf("recorded ids %q", got)
	}
	if got := f.read("mw.log"); got != "sync\n" {
		t.Fatalf("mw was run as %q, want one `mw sync`", got)
	}
	if got := f.read("bd.log"); got != "mail inbox mayor\n" {
		t.Fatalf("bd was run as %q, want one `bd mail inbox mayor`", got)
	}
}

func TestASecondTickAfterAnnouncingTypesNothingAgain(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa", "mw-bbb")

	f.tick()
	f.typed(fmt.Sprintf(announcement, 2))
	f.tick()

	f.nothingMoreTyped(fmt.Sprintf(announcement, 2))
	if f.syncs() != 1 {
		t.Fatalf("the second tick synced again inside five minutes: %q", f.read("mw.log"))
	}

	// Only the mail that is new since is announced, and by how many.
	f.inbox("mw-aaa", "mw-bbb", "mw-ccc")
	f.tick()
	f.typed(fmt.Sprintf(announcement, 2) + fmt.Sprintf(announcement, 1))
	if got := f.announced(); got != "mw-aaa\nmw-bbb\nmw-ccc\n" {
		t.Fatalf("recorded ids %q", got)
	}
}

func TestTextOnTheInputLineIsNeverTypedOverAndTheIdsAreNotRecorded(t *testing.T) {
	f := newFactory(t)
	f.mayor("has-text", actingByID)
	f.inbox("mw-aaa")

	f.tick()

	f.nothingMoreTyped("")
	if got := f.announced(); got != "" {
		t.Fatalf("recorded %q for mail that was never announced", got)
	}
}

func TestABusyPaneIsLeftAloneAndTheIdsAreNotRecorded(t *testing.T) {
	f := newFactory(t)
	f.mayor("busy", actingByID)
	f.inbox("mw-aaa")

	f.tick()

	f.nothingMoreTyped("")
	if got := f.announced(); got != "" {
		t.Fatalf("recorded %q for mail that was never announced", got)
	}
}

func TestNoNewMailTypesNothing(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)

	f.tick() // an empty inbox
	f.nothingMoreTyped("")

	// The real inbox says so in a sentence, and the sentence is not an id.
	f.write("inbox", "no unread mail for mayor\n", 0o644)
	f.tick()
	f.nothingMoreTyped("")
	if got := f.announced(); got != "" {
		t.Fatalf("recorded %q for an empty inbox", got)
	}

	f.inbox("mw-aaa")
	f.write("state/announced", "mw-aaa\n", 0o644) // already told
	f.tick()
	f.nothingMoreTyped("")
}

func TestLoadAboveTheLimitRunsNothing(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.load("3.50")

	f.tick()

	f.nothingMoreTyped("")
	if f.syncs() != 0 || f.bdCalls() != 0 {
		t.Fatalf("ran mw %q and bd %q with the load at 3.50", f.read("mw.log"), f.read("bd.log"))
	}
}

func TestTheLimitIsTheHostsToSet(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.load("3.50")
	f.env = append(f.env, "MW_MAIL_LOAD_LIMIT=4")

	f.tick()

	f.typed(fmt.Sprintf(announcement, 1))
}

func TestSyncsNoMoreOftenThanEveryFiveMinutes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ago   time.Duration
		syncs int
	}{
		{"never synced", -1, 1},
		{"synced a moment ago", 100 * time.Second, 0},
		{"synced just over five minutes ago", 301 * time.Second, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFactory(t)
			f.mayor("idle", actingByID)
			if tc.ago >= 0 {
				f.write("state/last-sync", strconv.FormatInt(time.Now().Add(-tc.ago).Unix(), 10)+"\n", 0o644)
			}

			f.tick()

			if got := f.syncs(); got != tc.syncs {
				t.Fatalf("mw ran %d times, want %d", got, tc.syncs)
			}
		})
	}
}

func TestATickWhileAnotherRunsDoesNothing(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	if err := os.MkdirAll(f.path("state"), 0o755); err != nil {
		t.Fatal(err)
	}
	holder := exec.Command("flock", f.path("state/lock"), "sleep", "10")
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Process.Kill(); _ = holder.Wait() })
	time.Sleep(300 * time.Millisecond) // for flock to take the lock

	f.tick()

	f.nothingMoreTyped("")
	if f.syncs() != 0 || f.bdCalls() != 0 {
		t.Fatalf("a second tick ran mw %q and bd %q", f.read("mw.log"), f.read("bd.log"))
	}
}

func TestTheMayorsWindowIsFoundFromWhateverTheActingFileSays(t *testing.T) {
	for name, acting := range map[string]string{
		"its id":            "Mayor after handoff 10 (window ID)",
		"its index":         "Mayor after handoff 10 (tmux window INDEX)",
		"its index by hash": "Mayor 10, tmux window #INDEX mayor",
		"its name":          "mayor-2026-09-19-10",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t)
			f.mayor("idle", acting)
			f.inbox("mw-aaa")

			f.tick()

			f.typed(fmt.Sprintf(announcement, 1))
		})
	}
}

func TestAnActingFileThatNamesNoWindowTypesNothing(t *testing.T) {
	for name, acting := range map[string]string{
		"nobody":                "",
		"a window that is gone": "Mayor after handoff 10 (window @9999)",
		"an index that is gone": "Mayor after handoff 10 (tmux window 42)",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t)
			f.mayor("idle", acting)
			f.inbox("mw-aaa")

			f.tick()

			f.nothingMoreTyped("")
			if got := f.announced(); got != "" {
				t.Fatalf("recorded %q for mail that was never announced", got)
			}
		})
	}
}

func TestNoTmuxServerAtAllTypesNothingAndStartsNothing(t *testing.T) {
	f := newFactory(t)
	f.inbox("mw-aaa")

	f.tick()

	if out, err := exec.Command("tmux", "-L", f.socket, "list-sessions").CombinedOutput(); err == nil {
		t.Fatalf("the script started a tmux server: %s", out)
	}
	if got := f.announced(); got != "" {
		t.Fatalf("recorded %q for mail that was never announced", got)
	}
}

func TestQuietAlarmTypesWhatMwNudgeSaysWithNoNewMail(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.nudges([2]string{"mw-gq6.30", "mw-gq6.30 in progress 73 min, no mail"})

	f.tick()

	f.typed("Quiet alarm for mayor: mw-gq6.30 in progress 73 min, no mail. Run mw status.\n")
}

func TestQuietAlarmCombinesEveryClauseMwNudgeGivesInOneLine(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.nudges(
		[2]string{"mw-gq6.30", "mw-gq6.30 in progress 73 min, no mail"},
		[2]string{"host:laptop", "laptop last synced 31 min ago"},
	)

	f.tick()

	f.typed("Quiet alarm for mayor: mw-gq6.30 in progress 73 min, no mail; laptop last synced 31 min ago. Run mw status.\n")
}

func TestQuietAlarmSaysNothingWhenMwNudgeSaysNothing(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)

	f.tick()

	f.nothingMoreTyped("")
	if f.nudgeCalls() != 1 {
		t.Fatalf("expected mw nudge to be asked once, got %d", f.nudgeCalls())
	}
}

func TestQuietAlarmIsDampedForAnHourPerCondition(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.nudges([2]string{"mw-gq6.30", "mw-gq6.30 in progress 73 min, no mail"})

	f.tick()
	f.typed("Quiet alarm for mayor: mw-gq6.30 in progress 73 min, no mail. Run mw status.\n")

	f.tick()
	f.nothingMoreTyped("Quiet alarm for mayor: mw-gq6.30 in progress 73 min, no mail. Run mw status.\n")
	if f.nudgeCalls() != 2 {
		t.Fatalf("expected mw nudge to still be asked on the second tick, got %d", f.nudgeCalls())
	}

	// A different condition is not damped by the first's having fired.
	f.nudges([2]string{"host:laptop", "laptop last synced 31 min ago"})
	f.tick()
	f.typed("Quiet alarm for mayor: mw-gq6.30 in progress 73 min, no mail. Run mw status.\n" +
		"Quiet alarm for mayor: laptop last synced 31 min ago. Run mw status.\n")
}

func TestQuietAlarmAndNewMailEachTypeTheirOwnLine(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.nudges([2]string{"mw-gq6.30", "mw-gq6.30 in progress 73 min, no mail"})

	f.tick()

	f.typed(fmt.Sprintf(announcement, 1) + "Quiet alarm for mayor: mw-gq6.30 in progress 73 min, no mail. Run mw status.\n")
	if got := f.announced(); got != "mw-aaa\n" {
		t.Fatalf("recorded ids %q", got)
	}
}

func TestTheScriptIsExecutableAndParses(t *testing.T) {
	info, err := os.Stat("mail-notify")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("contrib/mail-notify is not executable")
	}
	if out, err := exec.Command("sh", "-n", "mail-notify").CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
}
