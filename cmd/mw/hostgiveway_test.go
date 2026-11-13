package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	millwright "github.com/Jonathan-A-White/millwright"
)

// The script and mw giveway (the first story's command) must agree: the three
// events, the two modes, and the on, off and mark commands with the notes the
// log reads. A script holding a non-ASCII byte would be misread by Windows
// PowerShell 5.1, which reads a BOM-less file as ANSI.
func TestGiveWayScriptKeepsItsEventsModesAndCommands(t *testing.T) {
	s := millwright.GiveWayScript
	for _, want := range []string{
		"'unlock'", "'lock'", "'logoff'", "'tick'", "'freeze'", "'shutdown'",
		"'giveway', 'on'", "'giveway', 'off'", "'giveway', 'mark'",
		"windows-unlock", "windows-lock", "windows-logoff", "windows-tick",
		"windows_free_mb=", "windows_ms=",
		"[switch]$DryRun", "--exec", "--terminate", "Start WSL (millwright)",
		"drop_caches", "Ubuntu-24.04", "jwhite",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the give-way script lacks %q", want)
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			t.Fatalf("the give-way script holds a non-ASCII byte at %d", i)
		}
	}
	// the freeze comes first at unlock, before anything that can take time
	on := strings.Index(s, "'giveway', 'on', '--by', 'windows-unlock'")
	state := on + strings.Index(s[on:], "Set-State 'unlocked'")
	drop, sleep, mark := strings.Index(s, "drop_caches"), strings.Index(s, "Start-Sleep 10"), strings.Index(s, "'giveway', 'mark'")
	if on < 0 || state < on || drop < state || sleep < drop || mark < sleep {
		t.Errorf("at unlock the order must be on (%d), state (%d), drop caches (%d), sleep (%d), mark (%d)", on, state, drop, sleep, mark)
	}
	// the three tick modes: freeze renews, shutdown does not
	if !strings.Contains(s, "'giveway', 'on'") || strings.Count(s, "windows-tick") < 1 {
		t.Errorf("tick must renew with mw giveway on --by windows-tick")
	}
}

// The script's flags and note names are the ones mw giveway reads.
func TestGiveWayScriptUsesFlagsMwGivewayHas(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"giveway"})
	if err != nil || cmd.Name() != "giveway" {
		t.Fatalf("mw giveway not found: %v", err)
	}
	for _, flag := range []string{"by", "note"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("mw giveway has no --%s, which the give-way script passes", flag)
		}
	}
	for _, flag := range []string{"--by", "--note"} {
		if !strings.Contains(millwright.GiveWayScript, flag) {
			t.Errorf("the give-way script does not pass %s", flag)
		}
	}
}

func giveWayPinned(t *testing.T) {
	t.Helper()
	wslFlagPinned(t, wslFlagTestCommit)
}

func TestGiveWayInstallLineStartsWithACommentWithItsWayBack(t *testing.T) {
	giveWayPinned(t)
	for _, inline := range []bool{false, true} {
		line, err := GiveWayInstallLine("grace", inline)
		if err != nil {
			t.Fatal(err)
		}
		first := strings.SplitN(line, "\n", 2)[0]
		if !strings.HasPrefix(first, "# ") || !strings.Contains(first, "mw host giveway --uninstall-line") {
			t.Errorf("(inline %v) the paste must begin with a comment saying what it does and its way back, got %q", inline, first)
		}
	}
}

func TestGiveWayInstallLineRegistersFourTasksWithTheTriggersForAnyUser(t *testing.T) {
	giveWayPinned(t)
	line, err := GiveWayInstallLine("grace", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"millwright give way (unlock)", "millwright give way (lock)", "millwright give way (logoff)", "millwright give way (tick)"} {
		if !strings.Contains(line, "'"+name+"'") {
			t.Errorf("the paste must register the task %q:\n%s", name, line)
		}
	}
	if n := strings.Count(line, "Register-ScheduledTask"); n != 4 {
		t.Errorf("expected four Register-ScheduledTask, got %d", n)
	}
	// session state changes: 1 console connect, 2 console disconnect, 7 lock, 8 unlock, of any user's session
	if !strings.Contains(line, "MSFT_TaskSessionStateChangeTrigger") {
		t.Errorf("the lock, unlock and console triggers are session-state-change triggers")
	}
	// 3 remote connect and 4 remote disconnect (remote desktop) are the same as console connect and disconnect
	for _, want := range []string{"StateChange=8", "StateChange=7", "StateChange=1", "StateChange=2", "StateChange=3", "StateChange=4", "-AtLogOn"} {
		if !strings.Contains(line, want) {
			t.Errorf("the paste lacks the trigger %q", want)
		}
	}
	if strings.Contains(line, "UserId=") || strings.Contains(line, "-AtLogOn -User") {
		t.Errorf("the triggers must fire for ANY user's session: no UserId on a trigger")
	}
	// every in-script event name is the script's own
	for _, ev := range []string{"-Event unlock", "-Event lock", "-Event logoff", "-Event tick"} {
		if !strings.Contains(line, ev) {
			t.Errorf("the paste lacks the action %q", ev)
		}
	}
	// no window: the task's action is a console-less host, not a flag that still flashes
	if !strings.Contains(line, "conhost.exe") || !strings.Contains(line, "--headless") {
		t.Errorf("the task action must start powershell under conhost.exe --headless")
	}
	// the tick repeats every 5 minutes
	if !strings.Contains(line, "-RepetitionInterval (New-TimeSpan -Minutes 5)") {
		t.Errorf("the tick task must repeat every 5 minutes")
	}
	// the principal is the account that runs the paste, and the paste refuses any other
	if !strings.Contains(line, `-UserId "$env:USERDOMAIN\$env:USERNAME"`) || !strings.Contains(line, "-LogonType Interactive") {
		t.Errorf("the tasks must run under the account that runs the paste")
	}
	if !strings.Contains(line, "$env:USERNAME -ne 'grace'") {
		t.Errorf("the paste must refuse to run under any account but the one that owns WSL")
	}
	// the folder, the mode file and the print of what was registered
	for _, want := range []string{`$env:ProgramData\millwright\giveway`, `"$d\mode"`, "-Value freeze", "Get-ScheduledTaskInfo", "NextRunTime"} {
		if !strings.Contains(line, want) {
			t.Errorf("the paste lacks %q", want)
		}
	}
}

// A sign-out lifts the give way at once: Task Scheduler has no logoff trigger, so
// an event-log trigger on the User Profile Service's "Finished processing user
// logoff notification" (event 4, which fires after the session has ended) runs
// the logoff task. It carries no user, so a sign-out of any account fires it.
func TestGiveWayInstallLineHasALogoffTaskOnTheUserProfileServiceEvent(t *testing.T) {
	giveWayPinned(t)
	line, err := GiveWayInstallLine("grace", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"MSFT_TaskEventTrigger",
		"Microsoft-Windows-User Profile Service/Operational",
		// the log is "Profile Service" and the provider "Profiles Service": read off the Laptop's real events
		`Provider[@Name="Microsoft-Windows-User Profiles Service"]`,
		"EventID=4",
		"Subscription=",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the logoff trigger lacks %q:\n%s", want, line)
		}
	}
	// the logoff task has the event trigger and no other
	m := regexp.MustCompile(`Register-ScheduledTask -TaskName 'millwright give way \(logoff\)'[^\n]*`).FindString(line)
	if m == "" || !strings.Contains(m, "-Event logoff") || !strings.Contains(m, "$lo") || strings.Contains(m, "$t") || strings.Contains(m, "-AtLogOn") {
		t.Errorf("the logoff task must run -Event logoff on the event trigger alone, got %q", m)
	}
	if !strings.Contains(line, "$lo=") {
		t.Errorf("the event trigger must be built once, as $lo, and handed to the logoff task")
	}
}

// Remote desktop connect and disconnect give way and let go like the console's.
func TestGiveWayRemoteDesktopTriggersAreOnTheUnlockAndLockTasks(t *testing.T) {
	giveWayPinned(t)
	line, err := GiveWayInstallLine("grace", true)
	if err != nil {
		t.Fatal(err)
	}
	unlock := regexp.MustCompile(`Register-ScheduledTask -TaskName 'millwright give way \(unlock\)'[^\n]*`).FindString(line)
	lock := regexp.MustCompile(`Register-ScheduledTask -TaskName 'millwright give way \(lock\)'[^\n]*`).FindString(line)
	// each session-state trigger is built once, as $tN for StateChange=N, and handed to the tasks
	for _, n := range []string{"3", "4"} {
		if !strings.Contains(line, "$t"+n+"=") || !strings.Contains(line, "StateChange="+n+";") {
			t.Errorf("the paste must build the session-state trigger %s", n)
		}
	}
	if !strings.Contains(unlock, "$t3") || !strings.Contains(unlock, "$t1") || !strings.Contains(unlock, "$t8") {
		t.Errorf("the unlock task must carry session states 8 (unlock), 1 (console connect) and 3 (remote connect): %q", unlock)
	}
	if !strings.Contains(lock, "$t7") || !strings.Contains(lock, "$t2") || !strings.Contains(lock, "$t4") {
		t.Errorf("the lock task must carry session states 7 (lock), 2 (console disconnect) and 4 (remote disconnect): %q", lock)
	}
}

// With WSL down nothing runs the factory, so a give way has nothing to freeze, and
// wsl.exe --exec would START the distro: the script looks for the WSL virtual
// machine's process (some tens of ms; wsl.exe --list --running takes about 400)
// before it calls wsl.exe, and does not call it when there is none.
func TestGiveWayScriptDoesNotStartWslThatIsDown(t *testing.T) {
	s := millwright.GiveWayScript
	for _, want := range []string{"function Wsl-Running", "GetProcessesByName", "vmmemWSL", "vmmem", "-AssumeWsl", "'up', 'down'"} {
		if !strings.Contains(s, want) {
			t.Errorf("the give-way script lacks %q", want)
		}
	}
	if strings.Contains(s, "--list --running") || strings.Contains(s, "'--list'") {
		t.Errorf("wsl.exe --list --running costs about 400 ms: the check must be a process lookup")
	}
	// at unlock the check comes before the freeze (it costs nothing) and before anything that calls wsl.exe
	i := strings.Index(s, "if ($Event -eq 'unlock')")
	if i < 0 {
		t.Fatalf("no unlock branch")
	}
	unlock := s[i:]
	check, on := strings.Index(unlock, "Wsl-Running"), strings.Index(unlock, "windows-unlock")
	if check < 0 || on < check {
		t.Errorf("at unlock Wsl-Running (%d) must come before the freeze (%d)", check, on)
	}
	// every place the script calls wsl.exe for mw is behind the check, except shutdown mode's start
	for _, want := range []string{"WSL is not running"} {
		if !strings.Contains(s, want) {
			t.Errorf("the log must say when it skipped for a WSL that is down: %q", want)
		}
	}
}

// A person at the screen under any account, console or remote, is what the tick and
// the logoff look for.
func TestGiveWayScriptLooksForASessionOfAnyUser(t *testing.T) {
	s := millwright.GiveWayScript
	for _, want := range []string{"function Screen-Occupied", "quser.exe", "Active", "LogonUI", "SessionId", "-AssumeScreen", "'occupied', 'empty'"} {
		if !strings.Contains(s, want) {
			t.Errorf("the give-way script lacks %q", want)
		}
	}
	if strings.Contains(s, `\bconsole\b`) {
		t.Errorf("a remote session is at the screen too: the check must not insist on the console")
	}
}

func TestGiveWayInstallLineChecksTheDownloadBeforeItRegistersAnything(t *testing.T) {
	giveWayPinned(t)
	line, err := GiveWayInstallLine("grace", false)
	if err != nil {
		t.Fatal(err)
	}
	url := "https://raw.githubusercontent.com/Jonathan-A-White/millwright/" + wslFlagTestCommit + "/contrib/windows/giveway.ps1"
	if !strings.Contains(line, "'"+url+"'") {
		t.Errorf("expected a download of %s pinned at the commit:\n%s", url, line)
	}
	sum := sha256.Sum256([]byte(millwright.GiveWayScript))
	if !strings.Contains(line, "-ne '"+strings.ToUpper(hex.EncodeToString(sum[:]))+"'") {
		t.Errorf("expected the SHA-256 of the embedded script checked")
	}
	chk, mv, reg := strings.Index(line, "Get-FileHash"), strings.Index(line, "Move-Item"), strings.Index(line, "Register-ScheduledTask")
	if chk < 0 || mv < chk || reg < mv {
		t.Errorf("hash (%d) before move (%d) before register (%d)", chk, mv, reg)
	}
	// the build check comes before the first thing it changes
	build, mk := strings.Index(line, "OSVersion.Version.Build"), strings.Index(line, "New-Item")
	if build < 0 || mk < build {
		t.Errorf("the Windows build and the account are checked (%d) before the folder is made (%d)", build, mk)
	}
	if strings.Contains(line, "FromBase64String") {
		t.Errorf("the short paste must not carry the script in itself")
	}
}

func TestGiveWayInstallLineWithNoCommitToPinAtSaysInline(t *testing.T) {
	wslFlagPinned(t, "")
	out, err := runMw(t, "host", "giveway", "--install-line")
	if err == nil || !strings.Contains(err.Error(), "--inline") {
		t.Errorf("expected a refusal naming --inline, got %v\n%s", err, out)
	}
	if out, err = runMw(t, "host", "giveway", "--install-line", "--inline"); err != nil {
		t.Errorf("--inline needs no commit: %v\n%s", err, out)
	}
}

func TestGiveWayInstallLineInlineCarriesTheScript(t *testing.T) {
	out, err := runMw(t, "host", "giveway", "--install-line", "--inline")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := regexp.MustCompile(`FromBase64String\('([^']+)'\)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("expected the script carried in the paste:\n%s", out)
	}
	script, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil || string(script) != millwright.GiveWayScript {
		t.Errorf("the paste must carry contrib/windows/giveway.ps1 as it is (err %v)", err)
	}
}

func TestGiveWayInstallLineTakesAnotherAccount(t *testing.T) {
	line, err := GiveWayInstallLine("someone", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "$env:USERNAME -ne 'someone'") {
		t.Errorf("--account must change the account the paste insists on")
	}
	if _, err := GiveWayInstallLine("a'b", true); err == nil {
		t.Errorf("an account holding a quote must be refused")
	}
}

func TestGiveWayUninstallLineRemovesTheFourTasksAndTheFolder(t *testing.T) {
	out, err := runMw(t, "host", "giveway", "--uninstall-line")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# ") {
		t.Errorf("must begin with a comment, got %.60q", out)
	}
	for _, want := range []string{"millwright give way (unlock)", "millwright give way (lock)", "millwright give way (logoff)", "millwright give way (tick)",
		"Unregister-ScheduledTask", "Remove-Item", `millwright\giveway`, "giveway off --by windows-uninstall"} {
		if !strings.Contains(out, want) {
			t.Errorf("the way back lacks %q:\n%s", want, out)
		}
	}
	// the wsl-flag script's folder is left as it is
	if strings.Contains(out, `Remove-Item "$env:ProgramData\millwright"`) {
		t.Errorf("the way back must not remove the folder of the other scripts")
	}
}

func TestGiveWayModeLine(t *testing.T) {
	for _, mode := range []string{"freeze", "shutdown"} {
		out, err := runMw(t, "host", "giveway", "--mode-line", mode)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if !strings.HasPrefix(out, "# ") || !strings.Contains(out, "mw host giveway --mode-line ") {
			t.Errorf("%s: must begin with a comment that names its way back, got %.80q", mode, out)
		}
		if !strings.Contains(out, `Set-Content -Path "$env:ProgramData\millwright\giveway\mode" -Value `+mode) {
			t.Errorf("%s: must write the mode file:\n%s", mode, out)
		}
	}
	if out, err := runMw(t, "host", "giveway", "--mode-line", "sleep"); err == nil {
		t.Errorf("a mode that is not freeze or shutdown must be refused, got:\n%s", out)
	}
}

func TestGiveWayHostCommandWantsExactlyOneThing(t *testing.T) {
	if out, err := runMw(t, "host", "giveway"); err == nil {
		t.Errorf("no flag must be refused, got:\n%s", out)
	}
	if out, err := runMw(t, "host", "giveway", "--install-line", "--uninstall-line"); err == nil {
		t.Errorf("two flags must be refused, got:\n%s", out)
	}
	if out, err := runMw(t, "host", "giveway", "--inline", "--uninstall-line"); err == nil {
		t.Errorf("--inline without --install-line must be refused, got:\n%s", out)
	}
}

// windowsPowerShell is the real Windows PowerShell 5.1 from a WSL with interop;
// the parse tests are skipped where there is none.
const windowsPowerShell = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"

// parseWithWindowsPowerShell hands text to the language parser on stdin and
// returns the parse errors it prints (none when the text is whole). It parses
// only: nothing is run and nothing is written.
func parseWithWindowsPowerShell(t *testing.T, text string) string {
	t.Helper()
	if _, err := os.Stat(windowsPowerShell); err != nil {
		t.Skip("no Windows PowerShell on this host")
	}
	cmd := exec.Command(windowsPowerShell, "-NoProfile", "-NonInteractive", "-Command",
		`$t=[Console]::In.ReadToEnd(); $e=$null; $tk=$null; [void][System.Management.Automation.Language.Parser]::ParseInput($t,[ref]$tk,[ref]$e); "PARSEERRORS=$($e.Count)"; $e | ForEach-Object { $_.ToString() }`)
	cmd.Stdin = strings.NewReader(text)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Skipf("Windows PowerShell is not usable here: %v: %s", err, out.String())
	}
	return out.String()
}

func TestWindowsPowerShellParsesTheScriptAndThePastes(t *testing.T) {
	giveWayPinned(t)
	short, err := GiveWayInstallLine("grace", false)
	if err != nil {
		t.Fatal(err)
	}
	inline, err := GiveWayInstallLine("grace", true)
	if err != nil {
		t.Fatal(err)
	}
	mode, _ := GiveWayModeLine("shutdown")
	for name, text := range map[string]string{
		"giveway.ps1": millwright.GiveWayScript, "install-line": short, "install-line --inline": inline,
		"uninstall-line": GiveWayUninstallLine(), "mode-line": mode,
	} {
		if got := parseWithWindowsPowerShell(t, text); !strings.HasPrefix(got, "PARSEERRORS=0") {
			t.Errorf("%s: the real Windows PowerShell reports parse errors:\n%s", name, got)
		}
	}
}

// A non-elevated PowerShell cannot register the lock and unlock tasks (session
// state triggers: Access is denied) while logoff and tick register, so a paste
// run there half-installs. The paste must ask whether the session is elevated
// before it writes anything (mw-2891blf.25).
func TestGiveWayInstallLineChecksElevationBeforeWritingAnything(t *testing.T) {
	giveWayPinned(t)
	for _, inline := range []bool{false, true} {
		line, err := GiveWayInstallLine("grace", inline)
		if err != nil {
			t.Fatal(err)
		}
		assertElevationFirst(t, fmt.Sprintf("giveway install (inline %v)", inline), line)
	}
}

// assertElevationFirst fails unless the paste checks for an elevated session,
// throws 'run as administrator: nothing installed', and does so before its
// first New-Item, Register-ScheduledTask or file write.
func assertElevationFirst(t *testing.T, what, line string) {
	t.Helper()
	check := strings.Index(line, "IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)")
	if check < 0 {
		t.Errorf("%s: the paste must check for an elevated session", what)
		return
	}
	if !strings.Contains(line, "run as administrator: nothing installed") {
		t.Errorf("%s: the paste must throw '... run as administrator: nothing installed'", what)
	}
	for _, first := range []string{"New-Item", "Register-ScheduledTask", "Set-Content", "WriteAllBytes", "Invoke-WebRequest", "Move-Item"} {
		if i := strings.Index(line, first); i >= 0 && i < check {
			t.Errorf("%s: the elevation check (%d) must come before the first %s (%d)", what, check, first, i)
		}
	}
	if i := strings.Index(line, "throw"); i < 0 || i < check {
		t.Errorf("%s: the first throw (%d) must be the elevation check's (%d)", what, i, check)
	}
}
