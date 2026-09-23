package doctor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// fakeMayorGoneTmux writes a stand-in for tmux answering list-windows and
// list-panes, the only two subcommands the real check ever runs, with the
// given bodies. windowsBody is what tmux 3.4 itself prints for
// #{window_id}|#{window_name} under env -i: a printable | is never rewritten
// for want of a UTF-8 client, unlike the tab the check used to ask for.
func fakeMayorGoneTmux(t *testing.T, windowsLine, panesLine string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for tmux is a shell script")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, "tmux-stand-in")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
list-windows) printf '%s\n' ;;
list-panes) printf '%s\n' ;;
esac
exit 0
`, windowsLine, panesLine)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the tmux stand-in: %v", err)
	}
	return program
}

func mayorGoneVault(t *testing.T, actingWindowName string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, application.ActingFileName("mayor")), []byte("acting in window "+actingWindowName+"\n"), 0o644); err != nil {
		t.Fatalf("writing .mayor-acting: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatalf("making bin: %v", err)
	}
	mayorUp := filepath.Join(dir, "bin", "mayor-up")
	if err := os.WriteFile(mayorUp, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing bin/mayor-up: %v", err)
	}
	return dir
}

// TestTheMayorGoneProbeIsOKWithTheTmux34NonUTF8ListWindowsFormat drives the
// probe with a stub that prints list-windows exactly as tmux 3.4 does under
// env -i for a non-UTF-8 client (mw-8wujg.4): no tab anywhere, since a | is
// printable and is never rewritten to _ the way a tab is.
func TestTheMayorGoneProbeIsOKWithTheTmux34NonUTF8ListWindowsFormat(t *testing.T) {
	vault := mayorGoneVault(t, "mayor-2026-09-23-40")
	tmux := fakeMayorGoneTmux(t, "@2|mayor-2026-09-23-40", "0 claude")
	check := &doctor.MayorGone{Vault: vault, Tmux: tmux}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestTheMayorGoneProbeIsFaultyWhenNoWindowNameMatchesTheActingFile(t *testing.T) {
	vault := mayorGoneVault(t, "mayor-2026-09-23-40")
	tmux := fakeMayorGoneTmux(t, "@2|some-other-window", "0 claude")
	check := &doctor.MayorGone{Vault: vault, Tmux: tmux}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if reason == "" {
		t.Errorf("expected a reason naming the acting file")
	}
}
