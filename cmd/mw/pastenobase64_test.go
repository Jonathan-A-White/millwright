package main

import (
	"strings"
	"testing"
)

// mw-gq6.1431: Bitdefender quarantined the Governor's PowerShell history for the
// `echo <base64> | base64 -d` pattern of a paste. No paste mw prints for a
// Windows PowerShell carries base64 or -EncodedCommand: its files are written as
// plain text, or downloaded.
func TestNoWindowsPasteCarriesBase64(t *testing.T) {
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
		out, err := runMw(t, args...)
		if err != nil {
			t.Errorf("mw %s failed: %v\n%.300s", strings.Join(args, " "), err, out)
			continue
		}
		lower := strings.ToLower(out)
		for _, banned := range []string{"base64", "-encodedcommand"} {
			if strings.Contains(lower, banned) {
				t.Errorf("mw %s prints %q, which an antivirus mistakes for a threat:\n%.300s", strings.Join(args, " "), banned, out)
			}
		}
	}
	// --inline carried the script as base64: the option is gone.
	for _, args := range [][]string{
		{"host", "wsl-flag", "--install-line", "--inline"},
		{"host", "giveway", "--install-line", "--inline"},
	} {
		if out, err := runMw(t, args...); err == nil {
			t.Errorf("mw %s should be refused, it carried base64:\n%.200s", strings.Join(args, " "), out)
		}
	}
}
