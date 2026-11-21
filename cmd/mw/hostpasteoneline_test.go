package main

import (
	"bytes"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// hostPasteBlocks is each paste in what an `mw host` command printed: the lines
// between ``` fences when it prints any (the prose around them is not pasted),
// otherwise the whole output, which is then the paste itself.
func hostPasteBlocks(out string) []string {
	if !strings.Contains(out, "```") {
		return []string{strings.TrimSuffix(out, "\n")}
	}
	var blocks []string
	parts := strings.Split(out, "```")
	for i := 1; i < len(parts); i += 2 {
		blocks = append(blocks, strings.Trim(parts[i], "\n"))
	}
	return blocks
}

// mw-gq6.1437: a 25-line paste typed into the Governor's PowerShell came out
// garbled ('Triggggggggggggggggger') and left a $c from an earlier line in use.
// Every paste mw host prints is one physical line, with no base64.
func TestEveryHostPasteIsOneLineAndHoldsNoBase64(t *testing.T) {
	hostTrimStates(t, "laptop", map[string]string{"laptop": "lacks-dropin"})
	giveWayPinned(t)
	for _, args := range [][]string{
		{"host", "trim-setup"},
		{"host", "wsl-flag", "--install-line"},
		{"host", "giveway", "--install-line"},
		{"host", "giveway", "--uninstall-line"},
		{"host", "giveway", "--mode-line", "freeze"},
		{"host", "giveway", "--mode-line", "shutdown"},
		{"host", "hidden"},
	} {
		name := "mw " + strings.Join(args, " ")
		out, err := runMw(t, args...)
		if err != nil {
			t.Errorf("%s failed: %v\n%.300s", name, err, out)
			continue
		}
		blocks := hostPasteBlocks(out)
		if len(blocks) == 0 || blocks[0] == "" {
			t.Errorf("%s printed no paste:\n%.300s", name, out)
		}
		for _, b := range blocks {
			if strings.ContainsAny(b, "\r\n") {
				t.Errorf("%s prints a paste of %d lines, it must be one physical line:\n%.400s", name, strings.Count(b, "\n")+1, b)
			}
		}
		lower := strings.ToLower(out)
		for _, banned := range []string{"base64", "-encodedcommand"} {
			if strings.Contains(lower, banned) {
				t.Errorf("%s prints %q", name, banned)
			}
		}
	}
}

// Every variable a give-way paste assigns or reads is $mwgw_<name>, so a $c, $d
// or $t left in the window by an earlier line is never picked up.
var powerShellVariableRe = regexp.MustCompile(`\$(\w+)`)

func TestGivewayInstallLineVariablesCarryThePrefix(t *testing.T) {
	giveWayPinned(t)
	line, err := GiveWayInstallLine("grace")
	if err != nil {
		t.Fatal(err)
	}
	assertVariablesCarryPrefix(t, "install-line", line, "mwgw_")

	// the line parses as PowerShell where pwsh is on the host
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh is not on this host: the parse of the install line is not run")
	}
	cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-Command",
		`$t=[Console]::In.ReadToEnd(); $e=$null; $tk=$null; [void][System.Management.Automation.Language.Parser]::ParseInput($t,[ref]$tk,[ref]$e); "PARSEERRORS=$($e.Count)"; $e | ForEach-Object { $_.ToString() }`)
	cmd.Stdin = strings.NewReader(line)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Skipf("pwsh is not usable here: %v: %s", err, out.String())
	}
	if !strings.HasPrefix(out.String(), "PARSEERRORS=0") {
		t.Errorf("pwsh reports parse errors in the install line:\n%s", out.String())
	}
}

// assertVariablesCarryPrefix fails for each variable of line that is neither
// automatic ($_, $true, $env:...) nor named with prefix.
func assertVariablesCarryPrefix(t *testing.T, what, line, prefix string) {
	t.Helper()
	automatic := map[string]bool{"_": true, "true": true, "false": true, "null": true, "env": true, "PSItem": true}
	seen := 0
	for _, m := range powerShellVariableRe.FindAllStringSubmatch(line, -1) {
		name := m[1]
		if automatic[name] {
			continue
		}
		seen++
		if !strings.HasPrefix(name, prefix) {
			t.Errorf("%s: the variable $%s must be named $%s...", what, name, prefix)
		}
	}
	if seen == 0 {
		t.Errorf("%s: no variable found, the check reads nothing", what)
	}
}

// The hidden paste's own variables are named apart too.
func TestHostHiddenPasteVariablesCarryThePrefix(t *testing.T) {
	out := hostHiddenPaste(t)
	// the scriptblock parameters ($s, $a, $x, $arg, $w) live in their own scope
	// and are named by their param(...) list; only the window's variables count
	for _, m := range regexp.MustCompile(`\$(q|done|hide|back|todo|t|new)\b`).FindAllString(out, -1) {
		t.Errorf("the hidden paste leaves %s, a name an earlier line may have set, in the window", m)
	}
}

// mw-q0cblt6: every paste an `mw host` command prints is a paste the Mayor may
// send to the Governor, so it passes the paste rules of mw postern send as
// printed (no base64, a Windows paste on one line, one fenced block, a new
// Windows PowerShell named). Each paste is sent as the Mayor would send it:
// the words that open a new Windows PowerShell, then the paste in one fence.
func TestHostPastesPassThePasteRules(t *testing.T) {
	hostTrimStates(t, "laptop", map[string]string{"laptop": "lacks-dropin"})
	giveWayPinned(t)
	for _, args := range [][]string{
		{"host", "trim-setup"},
		{"host", "wsl-flag", "--install-line"},
		{"host", "giveway", "--install-line"},
		{"host", "giveway", "--uninstall-line"},
		{"host", "giveway", "--mode-line", "freeze"},
		{"host", "giveway", "--mode-line", "shutdown"},
		{"host", "hidden"},
	} {
		name := "mw " + strings.Join(args, " ")
		out, err := runMw(t, args...)
		if err != nil {
			t.Errorf("%s failed: %v\n%.300s", name, err, out)
			continue
		}
		for _, block := range hostPasteBlocks(out) {
			message := "Open a new Windows PowerShell and paste this:\n```\n" + block + "\n```"
			if _, err := (application.PasteCheck{Surface: application.PasteSurfaceSend, Text: message}).Run(); err != nil {
				t.Errorf("%s prints a paste the paste rules refuse:\n%v\n%.300s", name, err, block)
			}
		}
	}
	line := doctor.WSLTrimPowerShellLine()
	message := "Open a new Windows PowerShell and paste this:\n```\n" + line + "\n```"
	if _, err := (application.PasteCheck{Surface: application.PasteSurfaceSend, Text: message}).Run(); err != nil {
		t.Errorf("the WSL trim PowerShell line is refused by the paste rules:\n%v", err)
	}
}
