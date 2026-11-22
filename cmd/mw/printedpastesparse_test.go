package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// parseErrorsOfPaste hands text to Windows PowerShell's own parser (powershell.exe,
// 5.1) and returns what it refuses, one error per line, "" when the text is whole.
// The text goes in a file with a byte-order mark, so the parser reads it as
// UTF-8 whatever the console's code page; nothing is run and nothing is
// written beside the temporary file. An error returned is a parse that did not
// happen, never a parse fault.
func parseErrorsOfPaste(ps, text string) (string, error) {
	dir, err := os.MkdirTemp("", "mwpaste")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "paste.ps1")
	if err := os.WriteFile(file, append([]byte{0xEF, 0xBB, 0xBF}, text...), 0o600); err != nil {
		return "", err
	}
	const parse = `$e=$null;$t=$null;[void][System.Management.Automation.Language.Parser]::ParseFile($env:MW_PS1,[ref]$t,[ref]$e);` +
		`'PARSED';foreach($x in $e){ '{0}:{1}: {2}' -f $x.Extent.StartLineNumber,$x.Extent.StartColumnNumber,$x.Message }`
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", parse)
	// WSLENV's /p turns the Linux path into the Windows one on its way in.
	cmd.Env = append(os.Environ(), "MW_PS1="+file, "WSLENV=MW_PS1/p:"+os.Getenv("WSLENV"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	got := strings.TrimSpace(strings.ReplaceAll(string(out), "\r", ""))
	if got != "PARSED" && !strings.HasPrefix(got, "PARSED\n") {
		return "", &parseNotRun{got}
	}
	return strings.TrimSpace(strings.TrimPrefix(got, "PARSED")), nil
}

type parseNotRun struct{ output string }

func (p *parseNotRun) Error() string { return "the parser did not answer: " + p.output }

// TestPrintedPastesParse runs every paste mw prints for a Windows PowerShell
// through that PowerShell's own parser. The pastes were checked by matching
// strings, so a parse fault in one (mw-6ww.185.18: "$want: begin" is a drive
// reference to 5.1) went unseen until the Governor pasted it. Skipped where
// there is no powershell.exe, as on a cloud host: it runs on the Laptop.
func TestPrintedPastesParse(t *testing.T) {
	ps := powerShellExe()
	if ps == "" {
		t.Skip("powershell.exe is not on this host (not on the PATH, not under /mnt/c): the parse of the printed pastes is not run here")
	}
	// The parser must refuse what it should, or an empty answer proves nothing.
	if got, err := parseErrorsOfPaste(ps, `Write-Host "$want: begin"`); err != nil {
		t.Skipf("powershell.exe is not usable here: %v", err)
	} else if got == "" {
		t.Fatal(`the parser accepted "$want: begin", which Windows PowerShell 5.1 refuses: this test reads nothing`)
	}

	hostTrimStates(t, "laptop", map[string]string{"laptop": "lacks-dropin"})
	giveWayPinned(t)
	pastes := map[string]string{"the WSL trim line (doctor.WSLTrimPowerShellLine)": doctor.WSLTrimPowerShellLine()}
	for _, args := range hostPasteCommands {
		name := "mw " + strings.Join(args, " ")
		out, err := runMw(t, args...)
		if err != nil {
			t.Errorf("%s failed: %v\n%.300s", name, err, out)
			continue
		}
		for i, block := range hostPasteBlocks(out) {
			pastes[name+" (paste "+string(rune('1'+i))+")"] = block
		}
	}
	for name, text := range pastes {
		got, err := parseErrorsOfPaste(ps, text)
		if err != nil {
			t.Errorf("%s: the parse did not run: %v", name, err)
			continue
		}
		if got != "" {
			t.Errorf("Windows PowerShell refuses %s (line:column: message):\n%s\n%.300s", name, got, text)
		}
	}
}
