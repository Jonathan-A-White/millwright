package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	millwright "github.com/Jonathan-A-White/millwright"
)

// runWatchDryRun runs the watcher script's tick dry run under the real Windows
// PowerShell 5.1 with a stand-in for wsl.exe: it prints what it would do and
// writes nothing. Skipped where there is no Windows PowerShell.
func runWatchDryRun(t *testing.T, args ...string) []string {
	t.Helper()
	if _, err := os.Stat(windowsPowerShell); err != nil {
		t.Skip("no Windows PowerShell on this host")
	}
	cmd := exec.Command(windowsPowerShell, "-NoProfile", "-NonInteractive", "-Command",
		`$t=[Console]::In.ReadToEnd(); & ([scriptblock]::Create($t)) -DryRun `+strings.Join(args, " "))
	cmd.Stdin = strings.NewReader(millwright.WSLFlagWatch)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Skipf("Windows PowerShell is not usable here: %v: %s", err, errOut.String())
	}
	if errOut.Len() > 0 {
		t.Errorf("the dry run wrote to stderr:\n%s", errOut.String())
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(out.String(), "\r", ""), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// actLines are the lines of a dry run that are acts: 'act <UTC time> <what>: <outcome>'.
func actLines(lines []string) []string {
	var acts []string
	for _, l := range lines {
		if strings.HasPrefix(l, "act ") {
			acts = append(acts, l)
		}
	}
	return acts
}

var actLineRe = regexp.MustCompile(`^act \d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ (.+): (.+)$`)

func TestWSLFlagWatchDryRunLeavesAWSLThatAnswersAlone(t *testing.T) {
	lines := runWatchDryRun(t, "-Assume running -Ticks 2")
	if acts := actLines(lines); len(acts) != 0 {
		t.Errorf("a running WSL that answers must get no act, got:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "--exec true") {
		t.Errorf("the dry run must show the answer check 'wsl.exe -d <distro> --exec true':\n%s", strings.Join(lines, "\n"))
	}
}

func TestWSLFlagWatchDryRunStartsAWSLThatIsNotRunning(t *testing.T) {
	lines := runWatchDryRun(t, "-Assume stopped -Ticks 1")
	acts := actLines(lines)
	if len(acts) != 1 {
		t.Fatalf("expected one act, got:\n%s", strings.Join(lines, "\n"))
	}
	m := actLineRe.FindStringSubmatch(acts[0])
	if m == nil || m[1] != "not running" || m[2] != "started" {
		t.Errorf("expected the act 'not running: started' with a UTC time, got %q", acts[0])
	}
	if strings.Contains(strings.Join(lines, "\n"), "--shutdown") {
		t.Errorf("a WSL that is not running is started, never shut down first:\n%s", strings.Join(lines, "\n"))
	}
}

func TestWSLFlagWatchDryRunRestartsAWSLThatIsHungOnTwoTicksAndNotOnOne(t *testing.T) {
	one := runWatchDryRun(t, "-Assume hung -Ticks 1")
	if acts := actLines(one); len(acts) != 0 {
		t.Errorf("one failed answer is not an act, got:\n%s", strings.Join(one, "\n"))
	}
	two := runWatchDryRun(t, "-Assume hung -Ticks 2")
	acts := actLines(two)
	if len(acts) != 1 {
		t.Fatalf("expected one act after two failed ticks, got:\n%s", strings.Join(two, "\n"))
	}
	if m := actLineRe.FindStringSubmatch(acts[0]); m == nil || m[1] != "hung" || m[2] != "restarted" {
		t.Errorf("expected the act 'hung: restarted', got %q", acts[0])
	}
	text := strings.Join(two, "\n")
	// the restart is a shutdown and then a start, in the second tick, never both a start and a restart
	sd, tick2 := strings.Index(text, "--shutdown"), strings.Index(text, "tick 2")
	if sd < 0 || tick2 < 0 || sd < tick2 {
		t.Errorf("the shutdown must come in the second tick (shutdown at %d, tick 2 at %d):\n%s", sd, tick2, text)
	}
	if strings.Contains(text, "not running: started") {
		t.Errorf("never both in one tick:\n%s", text)
	}
}

func TestWSLFlagWatchDryRunTakesADownFlagKeepsWSLDownAndStartsItAgain(t *testing.T) {
	lines := runWatchDryRun(t, "-Assume down -DownFor 600 -Ticks 3")
	text := strings.Join(lines, "\n")
	idx := func(sub string) int {
		for i, l := range lines {
			if strings.Contains(l, sub) {
				return i
			}
		}
		return -1
	}
	taken, still, again := idx("down: taken"), idx("down: still down"), idx("down: started again")
	if taken < 0 || still < 0 || again < 0 || !(taken < still && still < again) {
		t.Fatalf("expected 'taken', 'still down' and 'started again' in that order (%d, %d, %d):\n%s", taken, still, again, text)
	}
	if !strings.Contains(lines[taken], "down until 20") {
		t.Errorf("the first moment says when WSL is down until: %q", lines[taken])
	}
	// while it is down nothing is started and WSL is never asked
	for _, l := range lines[taken+1 : again] {
		if strings.Contains(l, "--exec") || strings.Contains(l, "sleep infinity") {
			t.Errorf("nothing may be started or asked while WSL is down: %q", l)
		}
	}
	if acts := actLines(lines); len(acts) != 1 || !strings.Contains(acts[0], "down: started again") {
		t.Errorf("expected one act 'down: started again', got %v", acts)
	}
}

func TestWSLFlagWatchDryRunIgnoresADownOfAnyOtherLength(t *testing.T) {
	for _, secs := range []string{"299", "86401", "0"} {
		lines := runWatchDryRun(t, "-Assume down -DownFor "+secs+" -Ticks 1")
		text := strings.Join(lines, "\n")
		if !strings.Contains(text, "ignored flag content") || strings.Contains(text, "down: taken") || strings.Contains(text, "--shutdown") {
			t.Errorf("a down of %s seconds must be ignored and WSL left up:\n%s", secs, text)
		}
	}
	for _, secs := range []string{"300", "86400"} {
		text := strings.Join(runWatchDryRun(t, "-Assume down -DownFor "+secs+" -Ticks 1"), "\n")
		if !strings.Contains(text, "down: taken") {
			t.Errorf("a down of %s seconds is a fixed action:\n%s", secs, text)
		}
	}
}

// The task keeps WSL up without anyone signed in, so every file of WSL's it
// reads or writes goes through wsl.exe (a sealed distro has no per-user
// \\wsl.localhost to a task with no session), and it needs no logon task.
func TestWSLFlagWatchScriptReachesWSLOnlyThroughWSLExe(t *testing.T) {
	s := millwright.WSLFlagWatch
	if strings.Contains(s, "wsl.localhost") {
		t.Errorf("the watcher script must not read or write through \\\\wsl.localhost")
	}
	if strings.Contains(s, "Start-ScheduledTask") || strings.Contains(s, "Start WSL (millwright)") {
		t.Errorf("restart and compact no longer need the task 'Start WSL (millwright)'")
	}
	for _, want := range []string{"/windows-acts", "--exec true", "--exec", "not running", "started", "hung", "restarted",
		"--shutdown", "sleep infinity", "WaitForExit"} {
		if !strings.Contains(s, want) {
			t.Errorf("the watcher script lacks %q", want)
		}
	}
	// the 40-second answer check
	if !regexp.MustCompile(`(?s)function Wsl-Answers.{0,400}40`).MatchString(s) {
		t.Errorf("the answer check must be bounded at 40 seconds")
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			t.Fatalf("the watcher script holds a non-ASCII byte at %d", i)
		}
	}
}

// Before it looks at the flag, each tick keeps WSL up: the keep-up step is
// called before the flag is read.
func TestWSLFlagWatchScriptKeepsWSLUpBeforeItReadsTheFlag(t *testing.T) {
	s := millwright.WSLFlagWatch
	keep, flagRead := strings.Index(s, "$kept = Keep-WslUp"), strings.Index(s, "$want = Wsl-Read")
	if keep < 0 || flagRead < 0 || keep > flagRead {
		t.Errorf("the keep-up step (%d) must come before the flag is read (%d)", keep, flagRead)
	}
	// a tick that started WSL does not also restart it, and the reverse
	if !strings.Contains(s, "$acted") {
		t.Errorf("a tick must make one act at most")
	}
	if !strings.Contains(s, "down-until") {
		t.Errorf("the down action must be kept across ticks in a file of its own")
	}
}

func TestWSLFlagInstallLineRegistersATaskForNobodySignedIn(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	line, err := WSLFlagInstallLine()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, "\n") || len(line) >= 2000 {
		t.Errorf("the install line is one line under 2000 bytes, got %d bytes", len(line))
	}
	for _, want := range []string{"-LogonType S4U", "-RunLevel Highest", "New-ScheduledTaskTrigger -AtStartup", "-RepetitionInterval (New-TimeSpan -Minutes 2)"} {
		if !strings.Contains(line, want) {
			t.Errorf("the install line lacks %q:\n%s", want, line)
		}
	}
	for _, bad := range []string{"-LogonType Interactive", "-AtLogOn", "-Password", "Get-Credential", "FromBase64String", "-EncodedCommand"} {
		if strings.Contains(line, bad) {
			t.Errorf("the install line must not hold %q (no password, no logon, no encoded script):\n%s", bad, line)
		}
	}
}

func TestWSLFlagInstallLineRemovesTheKeeperAndWatchdogAfterAGoodTick(t *testing.T) {
	wslFlagPinned(t, wslFlagTestCommit)
	line, err := WSLFlagInstallLine()
	if err != nil {
		t.Fatal(err)
	}
	start, good := strings.Index(line, "Start-ScheduledTask"), strings.Index(line, "last-good-tick")
	rm := strings.Index(line, "Unregister-ScheduledTask -TaskName millwright-wsl-keeper")
	if start < 0 || good < 0 || rm < 0 || !(start < rm) || !(good < rm) {
		t.Fatalf("the keeper and watchdog go only after the task has run (start %d, good tick %d, removal %d):\n%s", start, good, rm, line)
	}
	for _, name := range []string{"millwright-wsl-keeper", "millwright-wsl-watchdog"} {
		if !strings.Contains(line[rm:], name) {
			t.Errorf("the install line does not remove %s", name)
		}
	}
	if strings.Contains(line, "Start WSL (millwright)") {
		t.Errorf("the logon task is not this line's to touch")
	}
	// the script writes that file after a good tick
	if !strings.Contains(millwright.WSLFlagWatch, "last-good-tick") {
		t.Errorf("the watcher script must write last-good-tick after a good tick")
	}
}

func TestWSLFlagWayBackRemovesOnlyWhatTheInstallLineInstalls(t *testing.T) {
	back := WSLFlagWayBack()
	for _, want := range []string{"Unregister-ScheduledTask -TaskName millwright-wsl-flag-watch", `wsl-flag-watch.ps1"`} {
		if !strings.Contains(back, want) {
			t.Errorf("the way back lacks %q: %s", want, back)
		}
	}
	for _, bad := range []string{"keeper", "watchdog", "Start WSL", "-Recurse", "wsl.exe", "wslconfig", "ProgramData\\millwright\""} {
		if strings.Contains(back, bad) {
			t.Errorf("the way back must remove only the task and its script, but holds %q: %s", bad, back)
		}
	}
	if n := strings.Count(back, "Unregister-ScheduledTask"); n != 1 {
		t.Errorf("the way back unregisters exactly the one task, got %d", n)
	}
}

func TestWSLFlagDownWritesTheFlagWithTheSeconds(t *testing.T) {
	dir := wslFlagHere(t, nil)
	out, err := runMw(t, "host", "wsl-flag", "down", "--for", "10m")
	if err != nil {
		t.Fatalf("down --for 10m failed: %v\n%s", err, out)
	}
	b, rerr := os.ReadFile(filepath.Join(dir, "wsl-flag"))
	if rerr != nil || string(b) != "down 600" {
		t.Errorf("expected the flag 'down 600', got %q (%v)", b, rerr)
	}
	for _, want := range []string{"down", "10m", "WSL"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in what mw says:\n%s", want, out)
		}
	}
}

func TestWSLFlagDownTakesFiveMinutesToTwentyFourHours(t *testing.T) {
	dir := wslFlagHere(t, nil)
	for _, c := range []struct{ d, want string }{{"5m", "down 300"}, {"24h", "down 86400"}, {"1h30m", "down 5400"}} {
		os.Remove(filepath.Join(dir, "wsl-flag"))
		if out, err := runMw(t, "host", "wsl-flag", "down", "--for", c.d); err != nil {
			t.Fatalf("down --for %s refused: %v\n%s", c.d, err, out)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "wsl-flag")); string(b) != c.want {
			t.Errorf("--for %s: expected the flag %q, got %q", c.d, c.want, b)
		}
	}
}

func TestWSLFlagDownRefusesAnyOtherLengthBeforeTheFlagIsWritten(t *testing.T) {
	dir := wslFlagHere(t, nil)
	for _, d := range []string{"4m59s", "1s", "0s", "24h1s", "48h", "-10m", "5m0.5s"} {
		if out, err := runMw(t, "host", "wsl-flag", "down", "--for", d); err == nil {
			t.Errorf("--for %s must be refused, got:\n%s", d, out)
		}
		if _, err := os.Stat(filepath.Join(dir, "wsl-flag")); err == nil {
			t.Fatalf("--for %s: the flag was written", d)
		}
	}
	if _, err := runMw(t, "host", "wsl-flag", "down"); err == nil {
		t.Errorf("down with no --for must be refused")
	}
	if _, err := runMw(t, "host", "wsl-flag", "restart", "--for", "10m"); err == nil {
		t.Errorf("--for belongs to down only")
	}
	if _, err := os.Stat(filepath.Join(dir, "wsl-flag")); err == nil {
		t.Fatalf("the flag was written")
	}
}

func TestWSLFlagDownDryRunPrintsThreeMomentsAndWritesNothing(t *testing.T) {
	dir := wslFlagHere(t, nil)
	out, err := runMw(t, "host", "wsl-flag", "down", "--for", "10m", "--dry-run")
	if err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, out)
	}
	taken, still, again := strings.Index(out, "taken"), strings.Index(out, "still down"), strings.Index(out, "started again")
	if taken < 0 || still < 0 || again < 0 || !(taken < still && still < again) {
		t.Errorf("expected 'taken', 'still down' and 'started again' in order:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "wsl-flag")); err == nil {
		t.Errorf("a dry run writes no flag")
	}
	if out, err := runMw(t, "host", "wsl-flag", "down", "--for", "1m", "--dry-run"); err == nil {
		t.Errorf("a dry run of a length that is refused is refused too:\n%s", out)
	}
}
