package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	millwright "github.com/Jonathan-A-White/millwright"
)

// wslFlagHere makes a temp HOME the "WSL" the flag is written into, names this
// host "desktop", and polls fast. It returns the directory the Windows task
// would see as \\wsl.localhost\...\.local\state\mw.
func wslFlagHere(t *testing.T, reach map[string]string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	realHosts, realPoll := hostTrimHostsFn, wslFlagPoll
	hostTrimHostsFn = func() (string, map[string]string, error) { return "desktop", reach, nil }
	wslFlagPoll = 10 * time.Millisecond
	t.Cleanup(func() { hostTrimHostsFn, wslFlagPoll = realHosts, realPoll })
	dir := filepath.Join(home, ".local", "state", "mw")
	return dir
}

// fakeWatcher plays the Windows task: it waits for the flag, takes it (deletes
// it first, as the task does), and writes result, or nothing when result is "".
// It returns what the flag held.
func fakeWatcher(t *testing.T, dir, result string) <-chan string {
	t.Helper()
	got := make(chan string, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	go func() {
		for ctx.Err() == nil {
			b, err := os.ReadFile(filepath.Join(dir, "wsl-flag"))
			if err != nil {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			os.Remove(filepath.Join(dir, "wsl-flag"))
			got <- string(b)
			if result != "" {
				time.Sleep(30 * time.Millisecond) // WSL is "restarting"
				os.WriteFile(filepath.Join(dir, "wsl-flag.result"), []byte(result+"\n"), 0o644)
			}
			return
		}
	}()
	return got
}

func TestWSLFlagRestartWritesTheFlagAndPrintsTheResult(t *testing.T) {
	dir := wslFlagHere(t, nil)
	got := fakeWatcher(t, dir, "2026-11-07T11:00:00Z restart ok")
	out, err := runMw(t, "host", "wsl-flag", "restart", "--limit", "10s")
	if err != nil {
		t.Fatalf("mw host wsl-flag restart failed: %v\n%s", err, out)
	}
	if flag := <-got; flag != "restart" {
		t.Errorf("expected the flag to hold exactly %q, got %q", "restart", flag)
	}
	if !strings.Contains(out, "2026-11-07T11:00:00Z restart ok") {
		t.Errorf("expected the result printed, got %q", out)
	}
}

func TestWSLFlagCompactWritesCompact(t *testing.T) {
	dir := wslFlagHere(t, nil)
	got := fakeWatcher(t, dir, "2026-11-07T11:30:00Z compact ok: vhdx 305.0 GB -> 140.0 GB, fstrim exit 0")
	out, err := runMw(t, "host", "wsl-flag", "compact", "--limit", "10s")
	if err != nil {
		t.Fatalf("mw host wsl-flag compact failed: %v\n%s", err, out)
	}
	if flag := <-got; flag != "compact" {
		t.Errorf("expected the flag to hold %q, got %q", "compact", flag)
	}
	if !strings.Contains(out, "305.0 GB -> 140.0 GB") {
		t.Errorf("expected the result printed, got %q", out)
	}
}

// A result left by an earlier request must not answer this one.
func TestWSLFlagIgnoresAnOlderResult(t *testing.T) {
	dir := wslFlagHere(t, nil)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wsl-flag.result"), []byte("2026-11-01T00:00:00Z restart ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runMw(t, "host", "wsl-flag", "restart", "--limit", "300ms")
	if err == nil {
		t.Fatalf("expected no answer to be found, got success:\n%s", out)
	}
	if strings.Contains(out, "2026-11-01") {
		t.Errorf("an older result was taken as the answer: %q", out)
	}
}

func TestWSLFlagAsksForOnlyTheFixedActions(t *testing.T) {
	dir := wslFlagHere(t, nil)
	for _, bad := range []string{"format", "restart; reboot", ""} {
		args := []string{"host", "wsl-flag"}
		if bad != "" {
			args = append(args, bad)
		}
		out, err := runMw(t, args...)
		if err == nil {
			t.Fatalf("expected %q to be refused, got:\n%s", bad, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "wsl-flag")); err == nil {
		t.Errorf("a refused action must write no flag")
	}
}

func TestWSLFlagSaysThatTheTaskNeverTookTheFlag(t *testing.T) {
	dir := wslFlagHere(t, nil)
	out, err := runMw(t, "host", "wsl-flag", "restart", "--limit", "300ms")
	if err == nil {
		t.Fatalf("expected a wait with no watcher to fail, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "still there") || !strings.Contains(err.Error(), "--install-line") {
		t.Errorf("expected the flag to be said still there and the install line named, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "wsl-flag")); string(b) != "restart" {
		t.Errorf("expected the flag left in place, got %q", b)
	}
}

// A host that is slow to take the write (a loaded host, a slow ssh) must not
// spend the wait: --limit bounds the wait for the result, not the write of the
// flag, so a slow write still leaves the flag, and the wait ends "still there".
func TestWSLFlagLimitDoesNotBoundTheWriteOfTheFlag(t *testing.T) {
	dir := wslFlagHere(t, nil)
	real := hostWSLFlagRun
	hostWSLFlagRun = func(ctx context.Context, host, ssh, script string) (string, error) {
		if script == wslFlagWriteScript("restart") {
			select { // the write takes longer than --limit
			case <-time.After(400 * time.Millisecond):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return real(ctx, host, ssh, script)
	}
	t.Cleanup(func() { hostWSLFlagRun = real })
	out, err := runMw(t, "host", "wsl-flag", "restart", "--limit", "300ms")
	if err == nil {
		t.Fatalf("expected a wait with no watcher to fail, got:\n%s", out)
	}
	if strings.Contains(err.Error(), "was not written") || !strings.Contains(err.Error(), "still there") {
		t.Errorf("expected the flag said still there after a slow write, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "wsl-flag")); string(b) != "restart" {
		t.Errorf("expected the flag left in place, got %q", b)
	}
}

func TestWSLFlagSaysThatTheTaskTookTheFlagButHasNotAnswered(t *testing.T) {
	dir := wslFlagHere(t, nil)
	fakeWatcher(t, dir, "")
	_, err := runMw(t, "host", "wsl-flag", "compact", "--limit", "500ms")
	if err == nil || !strings.Contains(err.Error(), "took the flag") {
		t.Errorf("expected the flag said taken, got %v", err)
	}
}

func TestWSLFlagFailsWhenTheWindowsSideSaysItFailed(t *testing.T) {
	dir := wslFlagHere(t, nil)
	fakeWatcher(t, dir, "2026-11-07T11:30:00Z compact failed: diskpart exit 1 (vhdx 1.0 GB -> 1.0 GB); WSL started again")
	out, err := runMw(t, "host", "wsl-flag", "compact", "--limit", "10s")
	if err == nil {
		t.Fatalf("expected a failed result to fail the command:\n%s", out)
	}
	if !strings.Contains(out, "diskpart exit 1") {
		t.Errorf("expected the result printed even so, got %q", out)
	}
}

func TestWSLFlagReachesAnotherHostOverItsSSHPrefix(t *testing.T) {
	wslFlagHere(t, map[string]string{"laptop": "ssh -o BatchMode=yes laptop"})
	real := hostWSLFlagRun
	var calls []string
	hostWSLFlagRun = func(_ context.Context, host, ssh, script string) (string, error) {
		calls = append(calls, host+"|"+ssh+"|"+script)
		if strings.HasPrefix(script, "cat ") {
			return "2026-11-07T11:00:00Z restart ok", nil
		}
		return "", nil
	}
	t.Cleanup(func() { hostWSLFlagRun = real })

	out, err := runMw(t, "host", "wsl-flag", "restart", "--host", "laptop", "--limit", "10s")
	if err != nil {
		t.Fatalf("mw host wsl-flag --host laptop failed: %v\n%s", err, out)
	}
	if len(calls) < 2 || !strings.HasPrefix(calls[0], "laptop|ssh -o BatchMode=yes laptop|") || !strings.Contains(calls[0], "printf %s restart") {
		t.Errorf("expected the flag written on laptop over its ssh prefix, got %q", calls)
	}
	if strings.Contains(calls[0], "'") {
		t.Errorf("the script is wrapped in single quotes over ssh, so it must hold none: %q", calls[0])
	}
}

func TestWSLFlagKeepsAskingWhileTheHostIsDown(t *testing.T) {
	wslFlagHere(t, map[string]string{"laptop": "ssh laptop"})
	real := hostWSLFlagRun
	polls := 0
	hostWSLFlagRun = func(_ context.Context, host, ssh, script string) (string, error) {
		if !strings.HasPrefix(script, "cat ") {
			return "", nil
		}
		polls++
		if polls < 3 {
			return "", context.DeadlineExceeded // ssh fails while WSL restarts
		}
		return "2026-11-07T11:00:00Z restart ok", nil
	}
	t.Cleanup(func() { hostWSLFlagRun = real })
	if out, err := runMw(t, "host", "wsl-flag", "restart", "--host", "laptop", "--limit", "10s"); err != nil {
		t.Fatalf("expected the wait to outlive the restart: %v\n%s", err, out)
	}
}

func TestWSLFlagRefusesAHostItCannotReach(t *testing.T) {
	wslFlagHere(t, map[string]string{"laptop": "ssh laptop"})
	if out, err := runMw(t, "host", "wsl-flag", "restart", "--host", "mars"); err == nil {
		t.Errorf("expected an unknown host to be refused, got:\n%s", out)
	}
}

const wslFlagTestCommit = "5a476507f4c1d2e3a4b5c6d7e8f90123456789ab"

// wslFlagPinned makes this build know the commit it was built from, as a
// release build does and a test binary does not.
func wslFlagPinned(t *testing.T, commit string) {
	t.Helper()
	real := wslFlagCommit
	wslFlagCommit = func() string { return commit }
	t.Cleanup(func() { wslFlagCommit = real })
}

func TestWSLFlagInstallLinePrintsTheFencedLineAndItsWayBack(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	out, err := runMw(t, "host", "wsl-flag", "--install-line")
	if err != nil {
		t.Fatalf("mw host wsl-flag --install-line failed: %v\n%s", err, out)
	}
	for _, want := range []string{"Register-ScheduledTask -TaskName 'millwright-wsl-flag-watch'",
		"-RunLevel Highest", "-Minutes 2", "Unregister-ScheduledTask -TaskName millwright-wsl-flag-watch"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the install output:\n%s", want, out)
		}
	}
	// the install line is one line
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Register-ScheduledTask") && !strings.Contains(l, "-Force") {
			t.Errorf("the Register-ScheduledTask line must be whole on one line: %.80q", l)
		}
	}
}

// A whole-block Copy from Postern copies a fence's exact text, so a fenced
// block holds only the command: a leading PowerShell prompt would be read
// as Get-Process and a redirect to a file named "if". The window and its prompt
// are named in the prose before the fence.
func TestWSLFlagInstallLineFencesHoldOnlyTheCommandAndTheProseNamesThePrompt(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	out, err := runMw(t, "host", "wsl-flag", "--install-line")
	if err != nil {
		t.Fatalf("mw host wsl-flag --install-line failed: %v\n%s", err, out)
	}
	lines := strings.Split(out, "\n")
	var firsts []string
	inFence, prose := false, ""
	for i, l := range lines {
		if l == "```" {
			if !inFence && len(firsts) == 0 {
				prose = strings.Join(lines[:i], "\n")
			}
			if !inFence && i+1 < len(lines) {
				firsts = append(firsts, lines[i+1])
			}
			inFence = !inFence
			continue
		}
		if inFence && strings.HasPrefix(l, "PS ") {
			t.Errorf("a fenced line begins with a prompt: %.60q", l)
		}
	}
	if len(firsts) != 2 {
		t.Fatalf("expected two fenced blocks, got %d:\n%s", len(firsts), out)
	}
	for _, f := range firsts {
		if !strings.HasPrefix(f, "if (-not (") {
			t.Errorf("a fenced block must begin with the command, got %.60q", f)
		}
	}
	for _, want := range []string{"NEW admin Windows PowerShell", "PS C:\\WINDOWS\\system32>"} {
		if !strings.Contains(prose, want) {
			t.Errorf("expected %q in the prose before the first fence:\n%s", want, prose)
		}
	}
}

// The short line downloads the script pinned at the build's commit, refuses it
// unless its SHA-256 is that of the embedded script, and stays under 2000
// bytes so that Postern can carry it to a phone.
func TestWSLFlagInstallLineDownloadsPinnedChecksAHashAndIsShort(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	line, err := WSLFlagInstallLine()
	if err != nil {
		t.Fatal(err)
	}
	if len(line) >= 2000 {
		t.Errorf("the install line must be under 2000 bytes, got %d", len(line))
	}
	if strings.Contains(line, "\n") {
		t.Errorf("the install line must be one line")
	}
	url := "https://raw.githubusercontent.com/Jonathan-A-White/millwright/" + wslFlagTestCommit + "/contrib/windows/wsl-flag-watch.ps1"
	if !strings.Contains(line, "Invoke-WebRequest") || !strings.Contains(line, "'"+url+"'") {
		t.Errorf("expected a download of %s pinned at the commit:\n%s", url, line)
	}
	sum := sha256.Sum256([]byte(millwright.WSLFlagWatch))
	want := strings.ToUpper(hex.EncodeToString(sum[:]))
	check := "(Get-FileHash -Algorithm SHA256 $mwwf_t).Hash -ne '" + want + "'"
	if !strings.Contains(line, check) {
		t.Errorf("expected the SHA-256 of the embedded script checked (%s):\n%s", check, line)
	}
	// the check comes before the script is put in place and the task registered
	chk, mv, reg := strings.Index(line, "Get-FileHash"), strings.Index(line, "Move-Item"), strings.Index(line, "Register-ScheduledTask")
	if chk < 0 || mv < chk || reg < mv {
		t.Errorf("the hash must be checked (%d) before the script is moved in (%d) and the task registered (%d)", chk, mv, reg)
	}
	if strings.Contains(line, "FromBase64String") {
		t.Errorf("the short line must not carry the script in itself")
	}
}

func TestWSLFlagInstallLineRefusesWithNoCommitToPinAt(t *testing.T) {
	wslFlagPinned(t, "")
	out, err := runMw(t, "host", "wsl-flag", "--install-line")
	if err == nil || !strings.Contains(err.Error(), "rebuild") {
		t.Errorf("expected a refusal saying to rebuild, got %v\n%s", err, out)
	}
}

func TestWSLFlagInstallLineTakesNoAction(t *testing.T) {
	if out, err := runMw(t, "host", "wsl-flag", "restart", "--install-line"); err == nil {
		t.Errorf("expected an action with --install-line to be refused, got:\n%s", out)
	}
}

// The script and the command must agree on the file names and the fixed
// actions; a script holding a non-ASCII byte would be misread by Windows
// PowerShell 5.1, which reads a BOM-less file as ANSI.
func TestWSLFlagWatchScriptMatchesTheCommand(t *testing.T) {
	s := millwright.WSLFlagWatch
	for _, want := range []string{`/home/jwhite/.local/state/mw`, `/wsl-flag"`, `/wsl-flag.result"`, "'restart'", "'compact'",
		"fstrim -av", "--shutdown", `D:\wsl\Ubuntu-24.04\ext4.vhdx`,
		"attach vdisk readonly", "compact vdisk", "detach vdisk"} {
		if !strings.Contains(s, want) {
			t.Errorf("the watcher script lacks %q", want)
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			t.Fatalf("the watcher script holds a non-ASCII byte at %d", i)
		}
	}
	// the flag is deleted before the action runs
	del, act := strings.Index(s, "Wsl-Delete $flag"), strings.Index(s, "Log \"${want}: begin\"")
	if del < 0 || act < 0 || del > act {
		t.Errorf("the flag must be deleted before acting (delete at %d, act at %d)", del, act)
	}
}

// The way back unregisters the task and removes a file under ProgramData, which
// a PowerShell that is not elevated cannot do: it must refuse up front too
// (mw-2891blf.28).
func TestWSLFlagWayBackChecksElevationBeforeRemovingAnything(t *testing.T) {
	assertElevationFirst(t, "wsl-flag way back", WSLFlagWayBack())
	if !strings.Contains(WSLFlagWayBack(), windowsElevationCheckRemoval) {
		t.Errorf("the wsl-flag way back must use windowsElevationCheckRemoval")
	}
	if strings.Contains(WSLFlagWayBack(), "\n") {
		t.Errorf("the wsl-flag way back is one line")
	}
}

// The wsl-flag task is registered at RunLevel Highest, which a PowerShell that
// is not elevated cannot do: the line must refuse up front (mw-2891blf.25).
func TestWSLFlagInstallLinesCheckElevationBeforeWritingAnything(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	short, err := WSLFlagInstallLine()
	if err != nil {
		t.Fatal(err)
	}
	assertElevationFirst(t, "wsl-flag short line", short)
}

// Every variable the install paste makes starts $mwwf_, so that none can
// collide with one already set in the window it is pasted into (as the give way
// paste's do, $mwgw_). The automatic ones ($env:..., $true, $false, $null, $_)
// are PowerShell's own and are left alone. The way-back line makes no variable.
func TestWSLFlagInstallLineVariablesCarryThePrefix(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	line, err := WSLFlagInstallLine()
	if err != nil {
		t.Fatal(err)
	}
	automatic := map[string]bool{"env": true, "true": true, "false": true, "null": true, "_": true}
	varRe := regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
	found := 0
	for _, m := range varRe.FindAllStringSubmatch(line, -1) {
		if automatic[m[1]] {
			continue
		}
		found++
		if !strings.HasPrefix(m[1], "mwwf_") {
			t.Errorf("variable $%s of the install line must start $mwwf_", m[1])
		}
	}
	if found == 0 {
		t.Errorf("expected the install line to make variables of its own")
	}
	for _, m := range varRe.FindAllStringSubmatch(WSLFlagWayBack(), -1) {
		if !automatic[m[1]] {
			t.Errorf("the way-back line makes no variable, but has $%s", m[1])
		}
	}
}

// The state the task writes each tick beside windows-drive-free: one 'key value'
// line each (mw-6ww.185.2). Each key is held in the script's text as the start
// of a double-quoted line, "<key> $value".
var wslFlagStateKeys = []string{
	"time", "script_sha256", "boot_time", "restart_pending", "restart_pending_since", "vhdx_drive", "c_free", "vhdx_size",
	"memory_total", "memory_free", "ac_standby_timeout", "wsl_version", "task", "disk",
	"wslconfig_memory", "wslconfig_processors", "wslconfig_swap",
}

func TestWSLFlagWatchScriptWritesTheWindowsState(t *testing.T) {
	s := millwright.WSLFlagWatch
	if !strings.Contains(s, `/windows-state"`) {
		t.Errorf("the watcher script does not write windows-state beside windows-drive-free")
	}
	for _, key := range wslFlagStateKeys {
		if !strings.Contains(s, `"`+key+` `) {
			t.Errorf("the watcher script holds no state line %q", key)
		}
	}
	for _, want := range []string{"Get-ScheduledTask", "*millwright*", "Get-ScheduledTaskInfo", "LogonType", "powercfg", "SUB_SLEEP", "STANDBYIDLE",
		"wsl.exe --version", ".wslconfig", "Win32_OperatingSystem", "Get-FileHash", "RebootRequired", "LastBootUpTime"} {
		if !strings.Contains(s, want) {
			t.Errorf("the watcher script lacks %q", want)
		}
	}
	// the state is written before the flag is looked at
	state, flagRead := strings.Index(s, `$windowsState`), strings.Index(s, "$want = Wsl-Read")
	if state < 0 || flagRead < 0 || state > flagRead {
		t.Errorf("the state must be written before the flag is read (state at %d, flag read at %d)", state, flagRead)
	}
	// the dry run comes before anything is created, written or logged
	dry := strings.Index(s, "if ($StateDryRun)")
	for _, first := range []string{"New-Item -ItemType Directory", "Add-Content", "Set-Content"} {
		if i := strings.Index(s, first); dry < 0 || (i >= 0 && i < dry) {
			t.Errorf("the state dry run (%d) must come before the first %s (%d)", dry, first, i)
		}
	}
}

// runStateDryRun runs the script's state step under the real Windows PowerShell
// 5.1: it prints the lines and writes nothing. Skipped where there is none.
func runStateDryRun(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat(windowsPowerShell); err != nil {
		t.Skip("no Windows PowerShell on this host")
	}
	cmd := exec.Command(windowsPowerShell, "-NoProfile", "-NonInteractive", "-Command",
		`$t=[Console]::In.ReadToEnd(); & ([scriptblock]::Create($t)) -StateDryRun`)
	cmd.Stdin = strings.NewReader(millwright.WSLFlagWatch)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Skipf("Windows PowerShell is not usable here: %v: %s", err, errOut.String())
	}
	if errOut.Len() > 0 {
		t.Errorf("the state dry run wrote to stderr:\n%s", errOut.String())
	}
	return out.String()
}

func TestWSLFlagWatchStateDryRunPrintsTheLinesAndWritesNothing(t *testing.T) {
	out := runStateDryRun(t)
	have := map[string]string{}
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		if l == "" {
			continue
		}
		key, val, ok := strings.Cut(l, " ")
		if !ok || val == "" {
			t.Errorf("a state line is not 'key value': %q", l)
			continue
		}
		have[key] = val
	}
	// task and disk lines are as many as the PC has (a PC may have no millwright task)
	for _, key := range wslFlagStateKeys {
		if key == "task" || key == "disk" {
			continue
		}
		if have[key] == "" {
			t.Errorf("the dry run printed no %q line:\n%s", key, out)
		}
	}
	if _, err := time.Parse("2006-01-02T15:04:05Z", have["time"]); err != nil {
		t.Errorf("time is not UTC ISO-8601: %q", have["time"])
	}
	if got := have["script_sha256"]; got != "none" && len(got) != 64 {
		t.Errorf("script_sha256 is neither 'none' nor a SHA-256: %q", got)
	}
}

func TestWindowsPowerShellParsesTheWSLFlagScriptAndLine(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	line, err := WSLFlagInstallLine()
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"wsl-flag-watch.ps1": millwright.WSLFlagWatch, "install-line": line, "way-back": WSLFlagWayBack()} {
		if got := parseWithWindowsPowerShell(t, text); !strings.HasPrefix(got, "PARSEERRORS=0") {
			t.Errorf("%s: the real Windows PowerShell reports parse errors:\n%s", name, got)
		}
	}
}

// The inventory is a third fixed flag word: the long, read-only form of the
// state. Any other content is still ignored and logged.
func TestWSLFlagWatchScriptTakesTheInventoryWord(t *testing.T) {
	s := millwright.WSLFlagWatch
	for _, want := range []string{"'inventory'", `/windows-inventory.txt"`, "Get-WinEvent", "LogName = 'System'", "AddDays(-7)",
		"Get-PhysicalDisk", "MediaType", "HealthStatus", "-Tail 40", "ignored flag content"} {
		if !strings.Contains(s, want) {
			t.Errorf("the watcher script lacks %q", want)
		}
	}
	// the sleeps, wakes, shutdowns and unexpected shutdowns, by event id
	for _, id := range []string{"42", "1", "1074", "6006", "6008", "41"} {
		if !regexp.MustCompile(`\b` + id + `\b`).MatchString(s) {
			t.Errorf("the watcher script names no event %s", id)
		}
	}
	// the flag is deleted before the inventory is taken, as before any action
	del, inv := strings.Index(s, "Wsl-Delete $flag"), strings.Index(s, "$want -eq 'inventory'")
	if del < 0 || inv < 0 || del > inv {
		t.Errorf("the flag must be deleted (%d) before the inventory is taken (%d)", del, inv)
	}
}

func TestWSLFlagInventoryWritesTheFlagWaitsForTheFileAndPrintsIt(t *testing.T) {
	dir := wslFlagHere(t, nil)
	got := make(chan string, 1)
	go func() {
		for i := 0; i < 2000; i++ {
			b, err := os.ReadFile(filepath.Join(dir, "wsl-flag"))
			if err != nil {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			os.Remove(filepath.Join(dir, "wsl-flag"))
			got <- string(b)
			time.Sleep(30 * time.Millisecond)
			os.WriteFile(filepath.Join(dir, "windows-inventory.txt"), []byte("== state ==\ntime 2026-11-21T10:00:00Z\n== disks ==\nSamsung|SSD|Healthy\n"), 0o644)
			return
		}
	}()
	out, err := runMw(t, "host", "wsl-flag", "inventory", "--limit", "10s")
	if err != nil {
		t.Fatalf("mw host wsl-flag inventory failed: %v\n%s", err, out)
	}
	if flag := <-got; flag != "inventory" {
		t.Errorf("expected the flag to hold exactly %q, got %q", "inventory", flag)
	}
	if !strings.Contains(out, "== disks ==") || !strings.Contains(out, "Samsung|SSD|Healthy") {
		t.Errorf("expected the inventory printed, got %q", out)
	}
}

// An inventory left by an earlier request must not answer this one.
func TestWSLFlagInventoryIgnoresAnOlderFile(t *testing.T) {
	dir := wslFlagHere(t, nil)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "windows-inventory.txt"), []byte("an old inventory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runMw(t, "host", "wsl-flag", "inventory", "--limit", "300ms")
	if err == nil {
		t.Fatalf("expected no answer to be found, got success:\n%s", out)
	}
	if strings.Contains(out, "an old inventory") {
		t.Errorf("an older inventory was taken as the answer: %q", out)
	}
}

func TestWSLFlagInventoryReachesAnotherHostOverItsSSHPrefix(t *testing.T) {
	wslFlagHere(t, map[string]string{"desktop2": "ssh desktop2"})
	real := hostWSLFlagRun
	var calls []string
	hostWSLFlagRun = func(_ context.Context, host, ssh, script string) (string, error) {
		calls = append(calls, host+"|"+ssh+"|"+script)
		if strings.HasPrefix(script, "cat ") && strings.Contains(script, "windows-inventory.txt") {
			return "== state ==\ntime 2026-11-21T10:00:00Z", nil
		}
		return "", nil
	}
	t.Cleanup(func() { hostWSLFlagRun = real })
	out, err := runMw(t, "host", "wsl-flag", "inventory", "--host", "desktop2", "--limit", "10s")
	if err != nil {
		t.Fatalf("mw host wsl-flag inventory --host failed: %v\n%s", err, out)
	}
	if len(calls) < 2 || !strings.Contains(calls[0], "printf %s inventory") || !strings.Contains(calls[0], "rm -f") || !strings.Contains(calls[0], "windows-inventory.txt") {
		t.Errorf("expected the flag written after the last inventory is cleared, got %q", calls)
	}
	if !strings.Contains(out, "== state ==") {
		t.Errorf("expected the inventory printed, got %q", out)
	}
}

func TestWSLFlagStillAsksForOnlyTheFixedWords(t *testing.T) {
	wslFlagHere(t, nil)
	for _, bad := range []string{"inventory; reboot", "Inventory ", "inventories"} {
		if out, err := runMw(t, "host", "wsl-flag", bad); err == nil {
			t.Errorf("expected %q to be refused, got:\n%s", bad, out)
		}
	}
}

// D4 (mw-6ww.185.4): when WSL will not start and its drive is full, the watcher
// compacts, starts, trims, shuts down, compacts and starts, and says so.

func TestWSLFlagWatchScriptFreesAFullDrive(t *testing.T) {
	s := millwright.WSLFlagWatch
	for _, want := range []string{"drive full", "drive-freed-at", "not freeing: ", "Get-DiskImage", "-DriveFreeGB", "-FreedHoursAgo", "-AssumeAttached"} {
		if !strings.Contains(s, want) {
			t.Errorf("the watcher script lacks %q", want)
		}
	}
}

func indexOfLine(lines []string, from int, sub string) int {
	for i := from; i < len(lines); i++ {
		if strings.Contains(lines[i], sub) {
			return i
		}
	}
	return -1
}

func TestWSLFlagWatchDryRunFreesAFullDriveInOrder(t *testing.T) {
	lines := runWatchDryRun(t, "-Assume full -DriveFreeGB 1 -Ticks 1")
	text := strings.Join(lines, "\n")
	steps := []string{"DRYRUN diskpart compact", "sleep infinity", "-u root --exec fstrim -av", "--shutdown", "DRYRUN diskpart compact", "sleep infinity"}
	at := indexOfLine(lines, 0, "not running") // the start that did not take comes first
	if at < 0 {
		t.Fatalf("expected the failed start first:\n%s", text)
	}
	for _, step := range steps {
		i := indexOfLine(lines, at+1, step)
		if i < 0 {
			t.Fatalf("expected %q after line %d, in the order detach?, compact, start, fstrim, shutdown, compact, start:\n%s", step, at, text)
		}
		at = i
	}
	if strings.Contains(text, "DRYRUN diskpart detach") {
		t.Errorf("a vhdx that is not attached is not detached first:\n%s", text)
	}
	if strings.Contains(text, "not freeing") {
		t.Errorf("it freed the drive, so it says nothing of not freeing:\n%s", text)
	}
	acts := actLines(lines)
	last := acts[len(acts)-1]
	// the outcome holds a colon of its own (D:), so the act is cut at the first ': '
	_, act, _ := strings.Cut(last, "Z ")
	what, outcome, _ := strings.Cut(act, ": ")
	if what != "drive full" || !strings.HasPrefix(outcome, "freed (vhdx ") || !strings.Contains(outcome, "GB -> ") || !strings.Contains(outcome, "D: free 1 GB -> ") {
		t.Errorf("expected the act 'drive full: freed (vhdx A GB -> B GB; D: free X GB -> Y GB)', got %q", last)
	}
}

func TestWSLFlagWatchDryRunDetachesAnAttachedVhdxFirst(t *testing.T) {
	lines := runWatchDryRun(t, "-Assume full -DriveFreeGB 1 -AssumeAttached -Ticks 1")
	det := indexOfLine(lines, 0, "DRYRUN diskpart detach")
	comp := indexOfLine(lines, 0, "DRYRUN diskpart compact")
	if det < 0 || comp < 0 || det > comp {
		t.Errorf("expected the detach (%d) before the first compact (%d):\n%s", det, comp, strings.Join(lines, "\n"))
	}
}

func TestWSLFlagWatchDryRunDoesNotFreeWhenItShouldNot(t *testing.T) {
	for name, tc := range map[string]struct{ args, why string }{
		"the distro answers":    {"-Assume running -DriveFreeGB 1", "not freeing: the distro answers"},
		"the drive has room":    {"-Assume full -DriveFreeGB 2", "not freeing: the drive has 2 GB free"},
		"done less than 6h ago": {"-Assume full -DriveFreeGB 1 -FreedHoursAgo 5", "not freeing: it was done less than 6 hours ago"},
	} {
		lines := runWatchDryRun(t, tc.args+" -Ticks 1")
		text := strings.Join(lines, "\n")
		if !strings.Contains(text, tc.why) {
			t.Errorf("%s: expected %q:\n%s", name, tc.why, text)
		}
		for _, bad := range []string{"fstrim", "diskpart", "drive full"} {
			if strings.Contains(text, bad) {
				t.Errorf("%s: it must do none of it, but %q is in:\n%s", name, bad, text)
			}
		}
	}
	// 6 hours or more after, it frees again
	if text := strings.Join(runWatchDryRun(t, "-Assume full -DriveFreeGB 1 -FreedHoursAgo 7 -Ticks 1"), "\n"); !strings.Contains(text, "fstrim -av") {
		t.Errorf("expected it to free again 7 hours after:\n%s", text)
	}
}

func TestWSLFlagWatchDryRunFreesTheDriveOnceInTwoTicks(t *testing.T) {
	text := strings.Join(runWatchDryRun(t, "-Assume full -DriveFreeGB 1 -Ticks 2"), "\n")
	if n := strings.Count(text, "fstrim -av"); n != 1 {
		t.Errorf("expected one freeing in two ticks, got %d:\n%s", n, text)
	}
}

// D5 (mw-6ww.185.5): the Linux disk is made smaller than its Windows drive,
// once, by the fixed flag word 'shrink'.

func TestWSLFlagInstallLineKeepsTheShrinkSizeInTheTasksFolder(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	out, err := runMw(t, "host", "wsl-flag", "--install-line", "--shrink-gb", "220")
	if err != nil {
		t.Fatalf("mw host wsl-flag --install-line --shrink-gb 220 failed: %v\n%s", err, out)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Register-ScheduledTask") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no install line in:\n%s", out)
	}
	if len(line) >= 2000 {
		t.Errorf("the install line must stay under 2000 bytes, got %d", len(line))
	}
	if !strings.Contains(line, `"$mwwf_d\shrink-gb"`) || !strings.Contains(line, "'220'") {
		t.Errorf("expected the size 220 kept in $mwwf_d\\shrink-gb:\n%s", line)
	}
	// the size is kept before the task is registered, and the hash is checked first
	chk, keep, reg := strings.Index(line, "Get-FileHash"), strings.Index(line, "shrink-gb"), strings.Index(line, "Register-ScheduledTask")
	if chk < 0 || keep < chk || reg < keep {
		t.Errorf("expected the hash check (%d), then the size kept (%d), then the task registered (%d)", chk, keep, reg)
	}
}

func TestWSLFlagInstallLineWithNoShrinkSizeKeepsNothing(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	out, err := runMw(t, "host", "wsl-flag", "--install-line")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "shrink-gb") {
		t.Errorf("an install line given no size must not touch the kept one:\n%s", out)
	}
}

func TestWSLFlagShrinkSizeBelongsToTheInstallLineAndIsASensibleNumber(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	wslFlagHere(t, nil)
	if out, err := runMw(t, "host", "wsl-flag", "restart", "--shrink-gb", "220"); err == nil {
		t.Errorf("--shrink-gb belongs to --install-line only, got:\n%s", out)
	}
	for _, bad := range []string{"0", "-5", "9", "abc", "100000"} {
		if out, err := runMw(t, "host", "wsl-flag", "--install-line", "--shrink-gb="+bad); err == nil {
			t.Errorf("--shrink-gb %s must be refused, got:\n%s", bad, out)
		}
	}
}

func TestWSLFlagShrinkWritesTheFlagAndPrintsTheResult(t *testing.T) {
	dir := wslFlagHere(t, nil)
	wslFlagCapIs(t, "0")
	got := fakeWatcher(t, dir, "2026-11-21T12:00:00Z shrink ok: df / 1007.0 GB -> 216.0 GB")
	out, err := runMw(t, "host", "wsl-flag", "shrink", "--limit", "10s")
	if err != nil {
		t.Fatalf("mw host wsl-flag shrink failed: %v\n%s", err, out)
	}
	if flag := <-got; flag != "shrink" {
		t.Errorf("expected the flag to hold %q, got %q", "shrink", flag)
	}
	if !strings.Contains(out, "1007.0 GB -> 216.0 GB") {
		t.Errorf("expected the result printed, got %q", out)
	}
}

func TestWSLFlagShrinkFailsWhenTheWindowsSideSaysItFailed(t *testing.T) {
	dir := wslFlagHere(t, nil)
	wslFlagCapIs(t, "0")
	fakeWatcher(t, dir, "2026-11-21T12:00:00Z shrink failed: no size was installed")
	out, err := runMw(t, "host", "wsl-flag", "shrink", "--limit", "10s")
	if err == nil || !strings.Contains(out, "no size was installed") {
		t.Errorf("expected a failure that says why, got %v\n%s", err, out)
	}
}

func TestWSLFlagShrinkWaitsThreeHoursByDefault(t *testing.T) {
	if wslFlagShrinkLimit != 3*time.Hour {
		t.Errorf("the shrink waits 3h by default, the task's own limit; got %s", wslFlagShrinkLimit)
	}
}

func TestWSLFlagShrinkRefusesWhileTheCapIsNotZero(t *testing.T) {
	for _, cap := range []string{"1", "3", "unset"} {
		dir := wslFlagHere(t, nil)
		wslFlagCapIs(t, cap)
		out, err := runMw(t, "host", "wsl-flag", "shrink", "--limit", "300ms")
		if err == nil {
			t.Fatalf("cap %s: expected a refusal, got:\n%s", cap, out)
		}
		if !strings.Contains(err.Error(), "drain it first: mw config set cap 0") {
			t.Errorf("cap %s: expected the refusal to say how to drain, got %v", cap, err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "wsl-flag")); statErr == nil {
			t.Errorf("cap %s: a refused shrink must write no flag", cap)
		}
	}
}

func TestWSLFlagShrinkRunningSkipsTheCapCheck(t *testing.T) {
	dir := wslFlagHere(t, nil)
	wslFlagCapIs(t, "3")
	got := fakeWatcher(t, dir, "2026-11-21T12:00:00Z shrink ok: df / 1007.0 GB -> 216.0 GB")
	out, err := runMw(t, "host", "wsl-flag", "shrink", "--running", "--limit", "10s")
	if err != nil {
		t.Fatalf("expected --running to go ahead, got %v\n%s", err, out)
	}
	if flag := <-got; flag != "shrink" {
		t.Errorf("expected the flag %q, got %q", "shrink", flag)
	}
}

func TestWSLFlagShrinkRefusesWhenTheCapCannotBeRead(t *testing.T) {
	dir := wslFlagHere(t, nil)
	real := hostWSLFlagRun
	hostWSLFlagRun = func(ctx context.Context, host, ssh, script string) (string, error) {
		if strings.Contains(script, "config get cap") {
			return "", os.ErrDeadlineExceeded
		}
		return real(ctx, host, ssh, script)
	}
	t.Cleanup(func() { hostWSLFlagRun = real })
	if out, err := runMw(t, "host", "wsl-flag", "shrink", "--limit", "300ms"); err == nil {
		t.Errorf("a cap that cannot be read is not known to be 0, got:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "wsl-flag")); statErr == nil {
		t.Errorf("no flag may be written")
	}
}

// wslFlagCapIs makes the read of the host's cap answer cap ('unset' is the
// answer of a host whose config has none: mw config get fails there).
func wslFlagCapIs(t *testing.T, cap string) {
	t.Helper()
	real := hostWSLFlagRun
	hostWSLFlagRun = func(ctx context.Context, host, ssh, script string) (string, error) {
		if strings.Contains(script, "config get cap") {
			return cap, nil
		}
		return real(ctx, host, ssh, script)
	}
	t.Cleanup(func() { hostWSLFlagRun = real })
}

func TestWSLFlagStillIgnoresAnyOtherWordThanTheFixedOnes(t *testing.T) {
	if !strings.Contains(millwright.WSLFlagWatch, "$want -ne 'shrink'") {
		t.Errorf("the watcher script must take shrink among its fixed words")
	}
}

// The script's shrink, run under Windows PowerShell 5.1 against stand-ins.
const shrinkGood = "-FlagWord shrink -ShrinkGB 220 -WslVersion 3.0.3.0 -DriveSizeGB 286 -RootSizeGB 1007 -RootUsedGB 150 -Ticks 1"

func shrinkActs(lines []string) string { return strings.Join(actLines(lines), "\n") }

func TestWSLFlagWatchDryRunShrinksInOrder(t *testing.T) {
	lines := runWatchDryRun(t, shrinkGood)
	text := strings.Join(lines, "\n")
	steps := []string{"df -B1", "--shutdown", "--manage Ubuntu-24.04 --resize 220GB", "sleep infinity", "df -B1"}
	at := -1
	for _, step := range steps {
		i := indexOfLine(lines, at+1, step)
		if i < 0 {
			t.Fatalf("expected %q after line %d, in the order df, shutdown, resize, start, df:\n%s", step, at, text)
		}
		at = i
	}
	acts := actLines(lines)
	if len(acts) != 1 {
		t.Fatalf("expected one act, got:\n%s", text)
	}
	// the outcome holds colons of its own, so the act is cut at the first ': '
	_, act, _ := strings.Cut(acts[0], "Z ")
	what, outcome, _ := strings.Cut(act, ": ")
	if what != "shrink" || !strings.HasPrefix(outcome, "ok") {
		t.Fatalf("expected the act 'shrink: ok ...', got %q", acts[0])
	}
	for _, want := range []string{"1007.0 GB -> 220.0 GB", "completed successfully"} {
		if !strings.Contains(outcome, want) {
			t.Errorf("expected %q in the result (df's size before and after, the tool's last lines), got %q", want, outcome)
		}
	}
	if !strings.Contains(text, "result ") {
		t.Errorf("expected the result line to be written:\n%s", text)
	}
}

func TestWSLFlagWatchDryRunRefusesAndLeavesWSLAsItIs(t *testing.T) {
	for name, tc := range map[string]struct{ args, why string }{
		"no size installed":  {"-FlagWord shrink -Ticks 1", "failed: no size was installed"},
		"WSL older than 2.5": {strings.Replace(shrinkGood, "3.0.3.0", "2.4.13.0", 1), "failed: WSL 2.4.13.0 is older than 2.5"},
		"over 80% of drive":  {strings.Replace(shrinkGood, "-DriveSizeGB 286", "-DriveSizeGB 270", 1), "is not under 80% of the drive's 270 GB"},
		"root over 70% of N": {strings.Replace(shrinkGood, "-RootUsedGB 150", "-RootUsedGB 155", 1), "is not under 70% of 220 GB"},
	} {
		lines := runWatchDryRun(t, tc.args)
		text := strings.Join(lines, "\n")
		if acts := shrinkActs(lines); !strings.Contains(acts, "shrink: ") || !strings.Contains(acts, tc.why) {
			t.Errorf("%s: expected an act holding %q, got:\n%s", name, tc.why, text)
		}
		for _, bad := range []string{"--shutdown", "--manage", "sleep infinity"} {
			if strings.Contains(text, bad) {
				t.Errorf("%s: a refusal must leave WSL as it is, but %q is in:\n%s", name, bad, text)
			}
		}
	}
}

func TestWSLFlagWatchDryRunSaysEachRefusalAtOnce(t *testing.T) {
	lines := runWatchDryRun(t, "-FlagWord shrink -ShrinkGB 220 -WslVersion 2.4.1 -DriveSizeGB 250 -RootSizeGB 1007 -RootUsedGB 200 -Ticks 1")
	acts := shrinkActs(lines)
	for _, want := range []string{"older than 2.5", "not under 80%", "not under 70%"} {
		if !strings.Contains(acts, want) {
			t.Errorf("expected %q among the refusals:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

func TestWSLFlagWatchDryRunStartsWSLAgainWhenTheResizeFails(t *testing.T) {
	lines := runWatchDryRun(t, shrinkGood+" -ResizeFails")
	text := strings.Join(lines, "\n")
	resize := indexOfLine(lines, 0, "--manage")
	start := indexOfLine(lines, resize+1, "sleep infinity")
	if resize < 0 || start < 0 {
		t.Fatalf("expected WSL started again after the failed resize:\n%s", text)
	}
	acts := shrinkActs(lines)
	if !strings.Contains(acts, "shrink: failed: ") || !strings.Contains(acts, "WSL started again") || !strings.Contains(acts, "Wsl/Service/E_FAIL") {
		t.Errorf("expected 'failed: ...' with the tool's last lines and WSL started again, got:\n%s", text)
	}
}

func TestWSLFlagWatchScriptKeepsTheShrinkSizeInItsOwnFolder(t *testing.T) {
	if !strings.Contains(millwright.WSLFlagWatch, "Store-Get 'shrink-gb'") {
		t.Errorf("the script must read the size from shrink-gb beside its log")
	}
}
