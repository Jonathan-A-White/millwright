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
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	announcement = "New mail for mayor: %d message(s). Run bd mail inbox.\n"

	// The empty input line of Claude Code: the prompt mark and a no-break space.
	emptyPrompt = `printf '\342\235\257\302\240\n'`
)

// killedElapsed matches the one log line a snapshot the timeout killed
// leaves: a mention of the kill and how many seconds it ran before it.
var killedElapsed = regexp.MustCompile(`killed after \d+s`)

// A pane script for each state the Mayor's window can be in. Each ends in a cat
// that writes whatever is typed into it to $MW_TEST_DIR/typed, so that a test
// can say exactly what reached the window.
var panes = map[string]string{
	"idle":     emptyPrompt,
	"busy":     `printf 'working (esc to interrupt)\n'; ` + emptyPrompt,
	"has-text": `printf '\342\235\257 a half-written thought'`,

	// The screens tmux captured, shared with infrastructure/tmux's test: Claude
	// Code's dim suggested next prompt after the mark, and text a person typed.
	"ghost": `cat "$MW_FIXTURES/ghost-suggestion.txt"`,
	"draft": `cat "$MW_FIXTURES/real-draft.txt"`,
}

// factory is one throwaway world for the script to run in.
type factory struct {
	t      *testing.T
	dir    string // MW_TEST_DIR: stand-ins, their logs, the vault, the state
	socket string
	script string
	env    []string // more of the script's environment, for a test to set
}

// factories counts the factories made in this test binary, to name their sockets.
var factories atomic.Int64

func newFactory(t *testing.T) *factory {
	t.Helper()
	// Everything a factory owns is its own: a temp dir, a tmux socket and an
	// environment passed to the script, never the test process's. So its tests
	// run side by side, and each factory's test calls this exactly once.
	t.Parallel()
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
		t:   t,
		dir: t.TempDir(),
		// A count, not a clock: factories made side by side can read the same
		// nanosecond (WSL2's clock is coarse), and then share one tmux server.
		socket: fmt.Sprintf("mw-test-mailnotify-%d-%d", os.Getpid(), factories.Add(1)),
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
		"case \"$1 $2\" in\n"+
		"\"mail inbox\") cat \"$MW_TEST_DIR/inbox-$3\" 2>/dev/null || cat \"$MW_TEST_DIR/inbox\" ;;\n"+
		"\"vc status\") level=$(cat \"$MW_TEST_DIR/beads-level\" 2>/dev/null || echo lvl-1); "+
		"printf '{\"branch\":\"main\",\"commit\":\"%s\"}\\n' \"$level\" ;;\n"+
		"esac\nexit 0\n", 0o755)
	// The stand-in mw answers `postern view --help` the way cobra does: with
	// the view's own usage line when it has one, and with postern's own help
	// — exit 0 either way — when it does not (a no-view file). A view-sleep
	// file makes `postern view` take that many seconds, and a view-fails file
	// makes it fail.
	f.write("bin/mw", "#!/bin/sh\ncase \"$1\" in\n"+
		"sync) echo \"$*\" >> \"$MW_TEST_DIR/mw.log\" ;;\n"+
		"nudge) echo \"$*\" >> \"$MW_TEST_DIR/nudge.log\"; [ -f \"$MW_TEST_DIR/nudge-output\" ] && cat \"$MW_TEST_DIR/nudge-output\" ;;\n"+
		"postern) echo \"$*\" >> \"$MW_TEST_DIR/postern.log\"\n"+
		"\tcase \"$2 ${3:-}\" in\n"+
		"\t\"view --help\") if [ -f \"$MW_TEST_DIR/no-view\" ]; then printf 'Usage:\\n  mw postern [command]\\n'; else printf 'Usage:\\n  mw postern view [flags]\\n'; fi ;;\n"+
		"\t\"view \") [ -f \"$MW_TEST_DIR/view-sleep\" ] && sleep \"$(cat \"$MW_TEST_DIR/view-sleep\")\"; [ -f \"$MW_TEST_DIR/view-fails\" ] && exit 1 ;;\n"+
		"\t*) cat \"$MW_TEST_DIR/postern-count\" 2>/dev/null ;;\n"+
		"\tesac ;;\n"+
		"esac\nexit 0\n", 0o755)
	// The stand-in systemctl says mw-view-follow.service is active only when a
	// view-follow-active file exists; never the real host's user manager.
	f.write("bin/systemctl", "#!/bin/sh\n[ \"$1 $2\" = \"--user is-active\" ] && [ -f \"$MW_TEST_DIR/view-follow-active\" ]\n", 0o755)
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
func (f *factory) inbox(ids ...string) { f.inboxOf("", ids...) }

// inboxOf sets what `bd mail inbox <mailbox>` lists for one mailbox; with no
// mailbox it sets the listing every mailbox without its own gets.
func (f *factory) inboxOf(mailbox string, ids ...string) {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(id + "  a subject that is not to be typed\n")
	}
	name := "inbox"
	if mailbox != "" {
		name += "-" + mailbox
	}
	f.write(name, b.String(), 0o644)
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

// posternKey makes this host's postern key file exist, at the default path
// `mw postern key` itself would resolve HOME to. Not calling it leaves the
// host with no postern side.
func (f *factory) posternKey() { f.write(".config/mw/postern.key", "fake-key\n", 0o600) }

// servesTheView sets MW_MAIL_VIEW_EVERY to thirty seconds, as the install on
// the host that serves the live view does (the vault's hosts/desktop-move.md
// step 4.3): without it the notifier has no view step at all.
func (f *factory) servesTheView() { f.env = append(f.env, "MW_MAIL_VIEW_EVERY=30") }

// posternCount sets what `mw postern inbox --unread-count` prints.
func (f *factory) posternCount(n int) { f.write("postern-count", strconv.Itoa(n)+"\n", 0o644) }

// beadsLevel sets what `bd vc status --json` reports as the commit: the
// notifier's own stand-in for "the beads changed". Not calling it leaves the
// default, "lvl-1", so a first tick with no prior marker always sees a level
// to record.
func (f *factory) beadsLevel(level string) { f.write("beads-level", level, 0o644) }

// snapshotLevel is the marker the script last recorded for the snapshot it
// wrote or attempted.
func (f *factory) snapshotLevel() string {
	return strings.TrimSuffix(f.read("state/snapshot-level"), "\n")
}

// setSnapshotLastAt rewrites when the snapshot was last attempted, so a test
// can put the interval gate well in the past without sleeping for it.
func (f *factory) setSnapshotLastAt(at time.Time) {
	f.t.Helper()
	f.write("state/snapshot-last", strconv.FormatInt(at.Unix(), 10)+"\n", 0o644)
}

func (f *factory) posternCalls() int { return strings.Count(f.read("postern.log"), "\n") }

// posternSubCalls counts postern.log lines logged as "postern <sub> ...":
// "inbox" or "snapshot", so a test can tell the poll and the snapshot apart
// even though both are logged by the same stand-in case.
func (f *factory) posternSubCalls(sub string) int {
	n := 0
	for _, line := range strings.Split(f.read("postern.log"), "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && fields[1] == sub {
			n++
		}
	}
	return n
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
		"env", "MW_TEST_DIR="+f.dir, "MW_FIXTURES="+fixtures(f.t), f.path("pane.sh"))
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

func TestTheLineCountsTheNewMessagesNotTheWholeInbox(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	var ids, old []string
	for i := 0; i < 231; i++ {
		old = append(old, fmt.Sprintf("mw-old%03d", i))
	}
	ids = append(ids, old...)
	ids = append(ids, "mw-new1", "mw-new2")
	f.inbox(ids...)
	f.write("state/announced", strings.Join(old, "\n")+"\n", 0o644)

	f.tick()

	f.typed(fmt.Sprintf(announcement, 2))
}

// With no record of what was announced, every id in the inbox is new: this is
// the one way the line says the whole inbox's size (mw-gq6.179).
func TestWithNoRecordOfAnnouncedMailEveryIdIsNew(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	var ids []string
	for i := 0; i < 233; i++ {
		ids = append(ids, fmt.Sprintf("mw-old%03d", i))
	}
	f.inbox(ids...)

	f.tick()

	f.typed(fmt.Sprintf(announcement, 233))
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

// fixtures is the directory of captured screens that infrastructure/tmux's own
// test reads too.
func fixtures(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../infrastructure/tmux/testdata")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestADimSuggestionOnTheInputLineIsNotADraftAndIsTypedOver(t *testing.T) {
	f := newFactory(t)
	f.mayor("ghost", actingByID)
	f.inbox("mw-aaa")

	f.tick()

	f.typed(fmt.Sprintf(announcement, 1))
}

func TestATypedDraftOnACapturedScreenIsNeverTypedOver(t *testing.T) {
	f := newFactory(t)
	f.mayor("draft", actingByID)
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
	f.env = append(f.env, "MW_MAIL_LOAD_LIMIT=2")

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

// cores is what nproc says, the limit the notifier defaults to.
func cores(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("nproc").Output()
	if err != nil {
		t.Skipf("nproc: %v", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || n < 1 {
		t.Skipf("nproc printed %q", out)
	}
	return n
}

func TestTheLimitDefaultsToTheCoreCount(t *testing.T) {
	n := cores(t)
	for _, tc := range []struct {
		name    string
		load    string
		skipped bool
	}{
		{"above the core count", fmt.Sprintf("%d.50", n), true},
		{"below the core count", fmt.Sprintf("%d.50", n-1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFactory(t)
			f.mayor("idle", actingByID)
			f.inbox("mw-aaa")
			f.load(tc.load)

			out := f.tick()

			if tc.skipped {
				f.nothingMoreTyped("")
				if !strings.Contains(out, fmt.Sprintf("is above %d; skipping this tick", n)) {
					t.Fatalf("output %q does not name the core count %d as the limit", out, n)
				}
				return
			}
			f.typed(fmt.Sprintf(announcement, 1))
		})
	}
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

// setNudgedFiredAt rewrites when a quiet alarm condition last fired, keeping
// whatever wait the script itself last chose for it, so a test can jump the
// clock without touching the backoff computation.
func (f *factory) setNudgedFiredAt(key string, at time.Time) {
	f.t.Helper()
	wait := "0"
	if fields := strings.Fields(f.read("state/nudged/" + key)); len(fields) >= 2 {
		wait = fields[1]
	}
	f.write("state/nudged/"+key, fmt.Sprintf("%d %s\n", at.Unix(), wait), 0o644)
}

func TestQuietAlarmBacksOffDoublingCappedAtEightHours(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.nudges([2]string{"mw-gq6.30", "mw-gq6.30 in progress 73 min, no mail"})
	line := "Quiet alarm for mayor: mw-gq6.30 in progress 73 min, no mail. Run mw status.\n"
	typed := ""

	// The first time, it is typed at once.
	f.tick()
	typed += line
	f.typed(typed)

	// Just under an hour later: still damped.
	f.setNudgedFiredAt("mw-gq6.30", time.Now().Add(-59*time.Minute))
	f.tick()
	f.nothingMoreTyped(typed)

	// An hour later: fires again, and backs off to two hours.
	f.setNudgedFiredAt("mw-gq6.30", time.Now().Add(-61*time.Minute))
	f.tick()
	typed += line
	f.typed(typed)

	// Under two hours since that: still damped.
	f.setNudgedFiredAt("mw-gq6.30", time.Now().Add(-119*time.Minute))
	f.tick()
	f.nothingMoreTyped(typed)

	// Two hours since: fires, backs off to four hours.
	f.setNudgedFiredAt("mw-gq6.30", time.Now().Add(-121*time.Minute))
	f.tick()
	typed += line
	f.typed(typed)

	// Four hours since: fires, backs off to eight hours, the cap.
	f.setNudgedFiredAt("mw-gq6.30", time.Now().Add(-241*time.Minute))
	f.tick()
	typed += line
	f.typed(typed)

	// Nine hours since: fires again, because the wait is capped at eight
	// hours rather than doubling again to sixteen (which would still be
	// damped at nine).
	f.setNudgedFiredAt("mw-gq6.30", time.Now().Add(-541*time.Minute))
	f.tick()
	typed += line
	f.typed(typed)

	// Under eight hours since that: still damped, confirming the wait held
	// at the cap rather than growing further.
	f.setNudgedFiredAt("mw-gq6.30", time.Now().Add(-479*time.Minute))
	f.tick()
	f.nothingMoreTyped(typed)
}

func TestQuietAlarmResetsWhenConditionClearsAndReturns(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.nudges([2]string{"mw-gq6.30", "mw-gq6.30 in progress 73 min, no mail"})
	line := "Quiet alarm for mayor: mw-gq6.30 in progress 73 min, no mail. Run mw status.\n"

	f.tick()
	f.typed(line)

	// The condition clears: mw nudge says nothing about it.
	f.nudges()
	f.tick()
	f.nothingMoreTyped(line)

	// It returns well inside the hour it would otherwise still be damped
	// for: the damper was reset when the condition cleared, so it is typed
	// again at once.
	f.nudges([2]string{"mw-gq6.30", "mw-gq6.30 in progress 73 min, no mail"})
	f.tick()
	f.typed(line + line)
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

const posternAnnouncement = "New postern message for mayor (%d unread)\n"

func TestPosternPollWithNoKeyFileMakesNoCall(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternCount(2)

	f.tick()

	f.nothingMoreTyped("")
	if f.posternCalls() != 0 {
		t.Fatalf("mw postern was called %q with no postern key file", f.read("postern.log"))
	}
}

func TestPosternPollWithZeroUnreadTypesNothing(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)

	f.tick()

	f.nothingMoreTyped("")
	if n := f.posternSubCalls("inbox"); n != 1 {
		t.Fatalf("mw postern inbox was called %d times, want 1", n)
	}
}

func TestPosternPollWithUnreadTypesOneLine(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(2)

	f.tick()

	f.typed(fmt.Sprintf(posternAnnouncement, 2))
}

func TestPosternPollRepeatsNothingForTheSameCount(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(2)

	f.tick()
	f.typed(fmt.Sprintf(posternAnnouncement, 2))

	f.tick()
	f.nothingMoreTyped(fmt.Sprintf(posternAnnouncement, 2))
	if n := f.posternSubCalls("inbox"); n != 2 {
		t.Fatalf("mw postern inbox was called %d times across two ticks, want 2", n)
	}
}

// The count last noted was 1 (announced), the Mayor read the inbox (a tick sees
// 0), and a new message arrives (the next tick sees 1): that is a first message
// after a read, and is announced (mw-gq6.133).
func TestPosternPollAnnouncesTheFirstMessageAfterTheInboxWasRead(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.write("state/postern-count", "1\n", 0o644)

	f.posternCount(0)
	f.tick()
	f.nothingMoreTyped("")

	f.posternCount(1)
	f.tick()
	f.typed(fmt.Sprintf(posternAnnouncement, 1))

	f.tick()
	f.nothingMoreTyped(fmt.Sprintf(posternAnnouncement, 1))
}

// With the stored count of 1 and no fall seen, a count of 1 is not announced.
func TestPosternPollSaysNothingForTheStoredCountWithNoFall(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.write("state/postern-count", "1\n", 0o644)
	f.posternCount(1)

	f.tick()
	f.tick()

	f.nothingMoreTyped("")
}

// A poll that fails is not a fall: the unread messages are still there.
func TestPosternPollFailureIsNotNotedAsAFall(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.write("state/postern-count", "1\n", 0o644)
	f.write("postern-count", "", 0o644)
	f.tick()

	f.posternCount(1)
	f.tick()

	f.nothingMoreTyped("")
}

func TestPosternSnapshotRunsOnceOnTheFirstTickWithAKeyFile(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)

	f.tick()

	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot was called %d times, want 1", n)
	}
	if got := f.snapshotLevel(); got != "lvl-1" {
		t.Fatalf("recorded snapshot level %q, want lvl-1", got)
	}
}

func TestPosternSnapshotUnchangedBeadsRunsNothingEvenPastTheInterval(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)

	f.tick()
	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot was called %d times on the first tick, want 1", n)
	}

	// Well past the snapshot interval, but the beads have not changed since:
	// still nothing.
	f.setSnapshotLastAt(time.Now().Add(-1 * time.Hour))
	f.tick()

	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot was called %d times with the beads unchanged, want 1", n)
	}
}

func TestPosternSnapshotChangedBeadsRunsOnceThenWaitsOutTheInterval(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)

	f.tick()
	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot was called %d times on the first tick, want 1", n)
	}

	// The beads change again at once, but the snapshot interval has not
	// passed since the last attempt: still nothing.
	f.beadsLevel("lvl-2")
	f.tick()

	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot was called %d times inside the interval, want 1", n)
	}

	// Once both the interval has passed and the beads changed, it runs again.
	f.setSnapshotLastAt(time.Now().Add(-1 * time.Hour))
	f.tick()

	if n := f.posternSubCalls("snapshot"); n != 2 {
		t.Fatalf("mw postern snapshot was called %d times once the interval passed with changed beads, want 2", n)
	}
	if got := f.snapshotLevel(); got != "lvl-2" {
		t.Fatalf("recorded snapshot level %q, want lvl-2", got)
	}
}

// The snapshot's interval is the host's to set (the Governor's decision of
// 2026-09-28, reversing mw-tfne4.16's hard cap): MW_MAIL_SNAPSHOT_EVERY, 600
// seconds when it says nothing, as TestPosternSnapshotChangedBeadsRunsOnceThenWaitsOutTheInterval
// holds.
func TestMWMailSnapshotEverySetsTheSnapshotInterval(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)
	f.env = append(f.env, "MW_MAIL_SNAPSHOT_EVERY=5")

	f.tick()
	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot was called %d times on the first tick, want 1", n)
	}

	// Past a 5-second interval, and the beads have changed: it runs again.
	f.beadsLevel("lvl-2")
	f.setSnapshotLastAt(time.Now().Add(-6 * time.Second))
	f.tick()

	if n := f.posternSubCalls("snapshot"); n != 2 {
		t.Fatalf("mw postern snapshot was called %d times with MW_MAIL_SNAPSHOT_EVERY=5 and 6s gone, want 2", n)
	}
}

func TestAnMWMailSnapshotEveryThatIsNotANumberReadsAsTheDefault(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)
	f.env = append(f.env, "MW_MAIL_SNAPSHOT_EVERY=often")

	f.tick()
	f.beadsLevel("lvl-2")
	f.setSnapshotLastAt(time.Now().Add(-60 * time.Second))
	f.tick()

	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot was called %d times a minute apart, want 1 (the default 600s)", n)
	}
}

func TestPosternSnapshotDoesNotRunWithoutAKeyFile(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternCount(0)

	f.tick()

	if n := f.posternSubCalls("snapshot"); n != 0 {
		t.Fatalf("mw postern snapshot was called %d times with no postern key file, want 0", n)
	}
}

func TestAFailingPosternSnapshotDoesNotStopTheTick(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)
	f.write("bin/mw", "#!/bin/sh\ncase \"$1 $2\" in\n"+
		"\"postern snapshot\") echo \"$*\" >> \"$MW_TEST_DIR/postern.log\"; exit 1 ;;\n"+
		"esac\ncase \"$1\" in\n"+
		"sync) echo \"$*\" >> \"$MW_TEST_DIR/mw.log\" ;;\n"+
		"nudge) echo \"$*\" >> \"$MW_TEST_DIR/nudge.log\"; [ -f \"$MW_TEST_DIR/nudge-output\" ] && cat \"$MW_TEST_DIR/nudge-output\" ;;\n"+
		"postern) echo \"$*\" >> \"$MW_TEST_DIR/postern.log\"; cat \"$MW_TEST_DIR/postern-count\" 2>/dev/null ;;\n"+
		"esac\nexit 0\n", 0o755)
	f.inbox("mw-aaa")

	f.tick()

	f.typed(fmt.Sprintf(announcement, 1))
	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot was called %d times, want 1 (even though it failed)", n)
	}
}

func TestASlowPosternSnapshotIsKilledByItsOwnTimeoutAndDoesNotStopTheTick(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)
	f.env = append(f.env, "MW_MAIL_SNAPSHOT_TIMEOUT=1")
	f.write("bin/mw", "#!/bin/sh\ncase \"$1 $2\" in\n"+
		"\"postern snapshot\") echo \"$*\" >> \"$MW_TEST_DIR/postern.log\"; sleep 5; exit 0 ;;\n"+
		"esac\ncase \"$1\" in\n"+
		"sync) echo \"$*\" >> \"$MW_TEST_DIR/mw.log\" ;;\n"+
		"nudge) echo \"$*\" >> \"$MW_TEST_DIR/nudge.log\"; [ -f \"$MW_TEST_DIR/nudge-output\" ] && cat \"$MW_TEST_DIR/nudge-output\" ;;\n"+
		"postern) echo \"$*\" >> \"$MW_TEST_DIR/postern.log\"; cat \"$MW_TEST_DIR/postern-count\" 2>/dev/null ;;\n"+
		"esac\nexit 0\n", 0o755)
	f.inbox("mw-aaa")

	start := time.Now()
	out := f.tick()
	elapsed := time.Since(start)

	f.typed(fmt.Sprintf(announcement, 1))
	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot was called %d times, want 1 (even though it hung)", n)
	}
	if elapsed >= 4*time.Second {
		t.Fatalf("the tick took %v; a snapshot bounded to a 1s timeout should never have run anywhere near the 5s it tried to sleep", elapsed)
	}
	if !killedElapsed.MatchString(out) {
		t.Fatalf("output %q does not report the kill with its elapsed seconds", out)
	}

	// The next tick, still inside the snapshot interval, does not retry.
	f.tick()
	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot was called %d times after a second tick, want 1 (no retry inside the interval)", n)
	}
}

// TestAKilledPosternSnapshotNeverReportsANegativeElapsedTime covers a wall
// clock that steps back between the two `date` reads the script takes around
// a killed snapshot (start, then the kill): the elapsed seconds it logs must
// clamp at 0, never go negative.
func TestAKilledPosternSnapshotNeverReportsANegativeElapsedTime(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)
	f.env = append(f.env, "MW_MAIL_SNAPSHOT_TIMEOUT=1")
	f.write("bin/date", "#!/bin/sh\n"+
		"n=$(cat \"$MW_TEST_DIR/date-calls\" 2>/dev/null || echo 0)\n"+
		"n=$((n + 1))\n"+
		"echo \"$n\" > \"$MW_TEST_DIR/date-calls\"\n"+
		"if [ \"$n\" -eq 1 ]; then echo 1000000000; else echo 999999997; fi\n", 0o755)
	f.write("bin/mw", "#!/bin/sh\ncase \"$1 $2\" in\n"+
		"\"postern snapshot\") echo \"$*\" >> \"$MW_TEST_DIR/postern.log\"; sleep 5; exit 0 ;;\n"+
		"esac\ncase \"$1\" in\n"+
		"sync) echo \"$*\" >> \"$MW_TEST_DIR/mw.log\" ;;\n"+
		"nudge) echo \"$*\" >> \"$MW_TEST_DIR/nudge.log\"; [ -f \"$MW_TEST_DIR/nudge-output\" ] && cat \"$MW_TEST_DIR/nudge-output\" ;;\n"+
		"postern) echo \"$*\" >> \"$MW_TEST_DIR/postern.log\"; cat \"$MW_TEST_DIR/postern-count\" 2>/dev/null ;;\n"+
		"esac\nexit 0\n", 0o755)
	f.inbox("mw-aaa")

	out := f.tick()

	if !strings.Contains(out, "killed after 0s") {
		t.Fatalf("output %q does not report the kill as 0s when the clock stepped back between the two date reads", out)
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

// viewRuns counts the runs of `mw postern view` itself, not its --help probe.
func (f *factory) viewRuns() int {
	return strings.Count("\n"+f.read("postern.log"), "\npostern view\n")
}

// viewProbes counts the `mw postern view --help` probes.
func (f *factory) viewProbes() int {
	return strings.Count(f.read("postern.log"), "postern view --help\n")
}

// viewLevel is the marker the script last recorded for the view it wrote or
// attempted.
func (f *factory) viewLevel() string {
	return strings.TrimSuffix(f.read("state/view-level"), "\n")
}

// setViewLastAt rewrites when the view was last attempted, so a test can put
// the interval gate in the past without sleeping for it.
func (f *factory) setViewLastAt(at time.Time) {
	f.t.Helper()
	f.write("state/view-last", strconv.FormatInt(at.Unix(), 10)+"\n", 0o644)
}

func TestPosternViewRunsOnTheFirstTickWithAKeyFile(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.servesTheView()
	f.posternKey()
	f.posternCount(0)

	f.tick()

	if n := f.viewRuns(); n != 1 {
		t.Fatalf("mw postern view ran %d times, want 1\n%s", n, f.read("postern.log"))
	}
	if got := f.viewLevel(); got != "lvl-1" {
		t.Fatalf("recorded view level %q, want lvl-1", got)
	}
}

func TestPosternViewDoesNotRunWithoutAKeyFile(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.servesTheView()

	f.tick()

	if f.viewRuns() != 0 || f.viewProbes() != 0 {
		t.Fatalf("mw postern view was asked about with no postern key file: %q", f.read("postern.log"))
	}
}

func TestPosternViewUnchangedBeadsRunsNothingEvenPastTheInterval(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.servesTheView()
	f.posternKey()
	f.posternCount(0)

	f.tick()
	f.setViewLastAt(time.Now().Add(-1 * time.Hour))
	f.tick()

	if n := f.viewRuns(); n != 1 {
		t.Fatalf("mw postern view ran %d times with the beads unchanged, want 1", n)
	}
}

func TestPosternViewChangedBeadsRunsOnceThenWaitsOutItsInterval(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.servesTheView()
	f.posternKey()
	f.posternCount(0)

	f.tick()
	// The beads change at once, but thirty seconds have not passed since the
	// last attempt: nothing.
	f.beadsLevel("lvl-2")
	f.tick()
	if n := f.viewRuns(); n != 1 {
		t.Fatalf("mw postern view ran %d times inside its interval, want 1", n)
	}

	// Past the thirty seconds, with the beads changed: it runs again.
	f.setViewLastAt(time.Now().Add(-31 * time.Second))
	f.tick()
	if n := f.viewRuns(); n != 2 {
		t.Fatalf("mw postern view ran %d times once 31s had passed with changed beads, want 2", n)
	}
	if got := f.viewLevel(); got != "lvl-2" {
		t.Fatalf("recorded view level %q, want lvl-2", got)
	}
	// The snapshot keeps its own, far longer, interval.
	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot ran %d times, want 1", n)
	}
}

func TestPosternViewIsNotAskedForWhereTheHostDoesNotServeIt(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)

	f.tick()

	if f.viewRuns() != 0 || f.viewProbes() != 0 {
		t.Fatalf("mw postern view was asked about with MW_MAIL_VIEW_EVERY unset: %q", f.read("postern.log"))
	}
	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot ran %d times, want 1: the snapshot does not wait on the view", n)
	}
}

func TestMWMailViewEverySetsTheViewInterval(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(0)
	f.env = append(f.env, "MW_MAIL_VIEW_EVERY=120")

	f.tick()
	f.beadsLevel("lvl-2")
	f.setViewLastAt(time.Now().Add(-60 * time.Second))
	f.tick()

	if n := f.viewRuns(); n != 1 {
		t.Fatalf("mw postern view ran %d times a minute apart with MW_MAIL_VIEW_EVERY=120, want 1", n)
	}
}

func TestTheBeadsLevelIsReadOnceATickForTheViewAndTheSnapshotBoth(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.servesTheView()
	f.posternKey()
	f.posternCount(0)

	f.tick()

	if n := strings.Count(f.read("bd.log"), "vc status"); n != 1 {
		t.Fatalf("bd vc status was asked %d times in one tick, want 1:\n%s", n, f.read("bd.log"))
	}
	if f.viewRuns() != 1 || f.posternSubCalls("snapshot") != 1 {
		t.Fatalf("expected both the view and the snapshot on the first tick, got %q", f.read("postern.log"))
	}
}

func TestPosternViewIsSkippedWhereMwHasNoViewCommand(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.servesTheView()
	f.posternKey()
	f.posternCount(0)
	f.write("no-view", "", 0o644)

	out := f.tick()

	if n := f.viewRuns(); n != 0 {
		t.Fatalf("mw postern view ran %d times on an mw without it, want 0", n)
	}
	if n := f.viewProbes(); n != 1 {
		t.Fatalf("mw postern view --help was asked %d times, want once a tick", n)
	}
	if f.read("state/view-last") != "" {
		t.Fatal("recorded a view attempt for an mw that has no view to run")
	}
	if strings.Contains(out, "postern view") {
		t.Fatalf("expected an mw without the view to be passed over silently, got %q", out)
	}
	// The snapshot still runs as ever.
	if n := f.posternSubCalls("snapshot"); n != 1 {
		t.Fatalf("mw postern snapshot ran %d times, want 1", n)
	}
}

func TestAFailingPosternViewIsLoggedAndDoesNotStopTheTick(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.servesTheView()
	f.posternKey()
	f.posternCount(0)
	f.write("view-fails", "", 0o644)
	f.inbox("mw-aaa")

	out := f.tick()

	f.typed(fmt.Sprintf(announcement, 1))
	if n := f.viewRuns(); n != 1 {
		t.Fatalf("mw postern view ran %d times, want 1 (even though it failed)", n)
	}
	if !strings.Contains(out, "mw postern view did not finish cleanly") {
		t.Fatalf("output %q does not log the failed view", out)
	}
}

func TestASlowPosternViewIsKilledByItsOwnTimeoutAndNotRetriedInsideItsInterval(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.servesTheView()
	f.posternKey()
	f.posternCount(0)
	f.env = append(f.env, "MW_MAIL_VIEW_TIMEOUT=1")
	f.write("view-sleep", "5", 0o644)
	f.inbox("mw-aaa")

	start := time.Now()
	out := f.tick()
	elapsed := time.Since(start)

	f.typed(fmt.Sprintf(announcement, 1))
	if elapsed >= 4*time.Second {
		t.Fatalf("the tick took %v; a view bounded to a 1s timeout should never have run near the 5s it tried to sleep", elapsed)
	}
	if !strings.Contains(out, "mw postern view exceeded 1s") || !killedElapsed.MatchString(out) {
		t.Fatalf("output %q does not report the view's kill with its elapsed seconds", out)
	}

	f.beadsLevel("lvl-2")
	f.tick()
	if n := f.viewRuns(); n != 1 {
		t.Fatalf("mw postern view ran %d times inside its interval after a kill, want 1", n)
	}
}

// aboveTheLimit puts the host over a limit of 2 with mail waiting and a view
// due, so a test can see what a tick skipped for load still does.
func (f *factory) aboveTheLimit() {
	f.mayor("idle", actingByID)
	f.servesTheView()
	f.posternKey()
	f.posternCount(0)
	f.inbox("mw-aaa")
	f.load("3.50")
	f.env = append(f.env, "MW_MAIL_LOAD_LIMIT=2")
}

func TestASkippedTickPublishesAViewGoneStale(t *testing.T) {
	f := newFactory(t)
	f.aboveTheLimit()
	f.setViewLastAt(time.Now().Add(-6 * time.Minute))
	f.beadsLevel("lvl-2")

	out := f.tick()

	if n := f.viewRuns(); n != 1 {
		t.Fatalf("mw postern view ran %d times on a tick skipped for load with a view 6 minutes old, want 1\n%s", n, f.read("postern.log"))
	}
	if got := f.viewLevel(); got != "lvl-2" {
		t.Fatalf("recorded view level %q, want lvl-2", got)
	}
	if !regexp.MustCompile(`load 3\.50 is above 2; skipping this tick but publishing the view, \d+s stale`).MatchString(out) {
		t.Fatalf("output %q does not say the view is published on a skipped tick", out)
	}
	f.nothingMoreTyped("")
	if f.syncs() != 0 || f.bdCalls() != 1 || f.posternSubCalls("inbox") != 0 || f.posternSubCalls("snapshot") != 0 || f.read("nudge.log") != "" {
		t.Fatalf("a tick skipped for load did more than the view: mw %q, bd %q, postern %q", f.read("mw.log"), f.read("bd.log"), f.read("postern.log"))
	}
}

func TestASkippedTickLeavesAFreshViewAlone(t *testing.T) {
	f := newFactory(t)
	f.aboveTheLimit()
	f.setViewLastAt(time.Now().Add(-2 * time.Minute))
	f.beadsLevel("lvl-2")

	out := f.tick()

	f.nothingMoreTyped("")
	if f.viewRuns() != 0 || f.viewProbes() != 0 || f.syncs() != 0 || f.bdCalls() != 0 || f.posternCalls() != 0 {
		t.Fatalf("a tick skipped for load with a fresh view ran mw %q, bd %q, postern %q", f.read("mw.log"), f.read("bd.log"), f.read("postern.log"))
	}
	if strings.Contains(out, "publishing the view") {
		t.Fatalf("output %q says it publishes a fresh view", out)
	}
}

func TestASkippedTickPublishesNothingWhenTheBeadsHaveNotChanged(t *testing.T) {
	f := newFactory(t)
	f.aboveTheLimit()
	f.setViewLastAt(time.Now().Add(-6 * time.Minute))
	f.write("state/view-level", "lvl-1\n", 0o644)

	f.tick()

	if f.viewRuns() != 0 {
		t.Fatalf("mw postern view ran with the beads unchanged: %q", f.read("postern.log"))
	}
}

func TestTheViewMaxAgeIsTheHostsToSet(t *testing.T) {
	f := newFactory(t)
	f.aboveTheLimit()
	f.env = append(f.env, "MW_MAIL_VIEW_MAX_AGE=60")
	f.setViewLastAt(time.Now().Add(-2 * time.Minute))
	f.beadsLevel("lvl-2")

	f.tick()

	if n := f.viewRuns(); n != 1 {
		t.Fatalf("mw postern view ran %d times with MW_MAIL_VIEW_MAX_AGE=60 and a view 2 minutes old, want 1", n)
	}
}

func TestASkippedTickHasNoViewOnAHostThatDoesNotServeIt(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.load("3.50")
	f.env = append(f.env, "MW_MAIL_LOAD_LIMIT=2")

	out := f.tick()

	if f.posternCalls() != 0 || strings.Contains(out, "publishing the view") {
		t.Fatalf("a host with no MW_MAIL_VIEW_EVERY ran postern %q, said %q", f.read("postern.log"), out)
	}
}

// With mw-view-follow.service active the view step is retired: it says so
// once, and runs nothing and asks mw nothing, tick after tick.
func TestPosternViewStepIsRetiredWhileTheFollowServiceIsActive(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.servesTheView()
	f.posternKey()
	f.posternCount(0)
	f.write("view-follow-active", "", 0o644)

	out := f.tick()
	f.beadsLevel("lvl-2")
	f.setViewLastAt(time.Now().Add(-1 * time.Hour))
	out += f.tick()

	if f.viewRuns() != 0 || f.viewProbes() != 0 {
		t.Fatalf("the view step ran with the follow service active: %q", f.read("postern.log"))
	}
	if n := strings.Count(out, "retired, mw postern view --follow does it"); n != 1 {
		t.Fatalf("said the step is retired %d times over two ticks, want once:\n%s", n, out)
	}
}

// The step comes back, and says nothing, once the service is not active.
func TestPosternViewStepRunsAgainWhenTheFollowServiceStops(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.servesTheView()
	f.posternKey()
	f.posternCount(0)
	f.write("view-follow-active", "", 0o644)
	f.tick()
	if err := os.Remove(f.path("view-follow-active")); err != nil {
		t.Fatal(err)
	}

	f.tick()

	if n := f.viewRuns(); n != 1 {
		t.Fatalf("mw postern view ran %d times after the service stopped, want 1", n)
	}
}
