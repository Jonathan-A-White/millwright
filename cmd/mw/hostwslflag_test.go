package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
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
	for _, want := range []string{"```\nPS C:\\", "Register-ScheduledTask -TaskName 'millwright-wsl-flag-watch'",
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
	check := "(Get-FileHash -Algorithm SHA256 $t).Hash -ne '" + want + "'"
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
	// the task is registered exactly as the inline form registers it
	reg1 := line[strings.Index(line, "Register-ScheduledTask"):]
	inline := WSLFlagInstallLineInline()
	if reg2 := inline[strings.Index(inline, "Register-ScheduledTask"):]; reg1 != reg2 {
		t.Errorf("the task must be registered as the inline form does:\n%s\n%s", reg1, reg2)
	}
}

func TestWSLFlagInstallLineRefusesWithNoCommitToPinAt(t *testing.T) {
	wslFlagPinned(t, "")
	out, err := runMw(t, "host", "wsl-flag", "--install-line")
	if err == nil || !strings.Contains(err.Error(), "--inline") {
		t.Errorf("expected a refusal naming --inline, got %v\n%s", err, out)
	}
	if out, err = runMw(t, "host", "wsl-flag", "--install-line", "--inline"); err != nil {
		t.Errorf("--inline needs no commit: %v\n%s", err, out)
	}
}

func TestWSLFlagInstallLineInlineCarriesTheScript(t *testing.T) {
	out, err := runMw(t, "host", "wsl-flag", "--install-line", "--inline")
	if err != nil {
		t.Fatalf("mw host wsl-flag --install-line --inline failed: %v\n%s", err, out)
	}
	m := regexp.MustCompile(`FromBase64String\('([^']+)'\)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("expected the script carried in the line:\n%s", out)
	}
	script, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil || string(script) != millwright.WSLFlagWatch {
		t.Errorf("the line must carry contrib/windows/wsl-flag-watch.ps1 as it is (err %v)", err)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Register-ScheduledTask") && !strings.Contains(l, "-Force") {
			t.Errorf("the Register-ScheduledTask line must be whole on one line: %.80q", l)
		}
	}
}

func TestWSLFlagInlineGoesWithInstallLine(t *testing.T) {
	dir := wslFlagHere(t, nil)
	if out, err := runMw(t, "host", "wsl-flag", "restart", "--inline"); err == nil {
		t.Errorf("expected --inline alone to be refused, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "wsl-flag")); err == nil {
		t.Errorf("a refused request must write no flag")
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
	for _, want := range []string{`\home\jwhite\.local\state\mw`, `\wsl-flag"`, `\wsl-flag.result"`, "'restart'", "'compact'",
		"fstrim -av", "wsl.exe --shutdown", "Start WSL (millwright)", `D:\wsl\Ubuntu-24.04\ext4.vhdx`,
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
	del, act := strings.Index(s, "Remove-Item -LiteralPath $flag"), strings.Index(s, "Log \"$want: begin\"")
	if del < 0 || act < 0 || del > act {
		t.Errorf("the flag must be deleted before acting (delete at %d, act at %d)", del, act)
	}
}

// The way back unregisters the task and removes a file under ProgramData, which
// a PowerShell that is not elevated cannot do: it must refuse up front too
// (mw-2891blf.28).
func TestWSLFlagWayBackChecksElevationBeforeRemovingAnything(t *testing.T) {
	assertElevationFirst(t, "wsl-flag way back", WSLFlagWayBack())
	if !strings.Contains(WSLFlagWayBack(), windowsElevationCheck) {
		t.Errorf("the wsl-flag way back must use windowsElevationCheck")
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
	assertElevationFirst(t, "wsl-flag inline line", WSLFlagInstallLineInline())
}
