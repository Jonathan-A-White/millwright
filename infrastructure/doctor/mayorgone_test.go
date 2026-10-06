package doctor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
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
	return mayorGoneVaultText(t, "acting in window "+actingWindowName+"\n")
}

func mayorGoneVaultText(t *testing.T, actingText string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, application.ActingFileName("mayor")), []byte(actingText), 0o644); err != nil {
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

// fakeMayorGoneTmuxWithPanePIDs is fakeMayorGoneTmux extended to answer
// `list-panes -a` (asked for #{window_id} #{pane_pid}, to find which process
// a window's pane runs) differently from `list-panes -t <window>` (asked for
// #{pane_dead} #{pane_current_command}, paneAlive's question).
func fakeMayorGoneTmuxWithPanePIDs(t *testing.T, windowsLine, panePIDsLine, panesLine string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for tmux is a shell script")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, "tmux-stand-in")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
list-windows) printf '%s\n' ;;
list-panes)
	case "$2" in
	-a) printf '%s\n' ;;
	*) printf '%s\n' ;;
	esac
	;;
esac
exit 0
`, windowsLine, panePIDsLine, panesLine)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the tmux stand-in: %v", err)
	}
	return program
}

// fakeMayorGonePS writes a stand-in for ps answering `-eo pid=,ppid=,args=`
// with the given lines, one process a line: "<pid> <ppid> <args...>".
func fakeMayorGonePS(t *testing.T, lines ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for ps is a shell script")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, "ps-stand-in")
	script := "#!/bin/sh\n"
	for _, line := range lines {
		script += fmt.Sprintf("printf '%s\\n'\n", line)
	}
	script += "exit 0\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the ps stand-in: %v", err)
	}
	return program
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

// TestTheMayorGoneProbeIsNotFaultyWhenTheSessionIDMatchesAWindowOfADifferentName
// is mw-i80dx.10's acceptance criterion: .mayor-acting names window "mayor-X"
// and a session id, a Mayor resumed with `claude --resume` into a window
// tmux opens under its own name ("claude") rather than the one .mayor-acting
// gives — a real Mayor, not a faulty one, so the probe must not send the cure
// to start a second one.
func TestTheMayorGoneProbeIsNotFaultyWhenTheSessionIDMatchesAWindowOfADifferentName(t *testing.T) {
	vault := mayorGoneVaultText(t, "acting in window mayor-2026-09-25-61, "+
		"session 6b3686b4-dbe4-4f1b-8969-07e8c1084811\n")
	tmux := fakeMayorGoneTmuxWithPanePIDs(t,
		"@0|claude", // list-windows: only an unrelated-named window is open
		"@0 4242",   // list-panes -a: its pane's pid
		"0 claude",  // list-panes -t @0: alive, running claude
	)
	ps := fakeMayorGonePS(t, "4242 1000 claude --resume 6b3686b4-dbe4-4f1b-8969-07e8c1084811")
	check := &doctor.MayorGone{Vault: vault, Tmux: tmux, PS: ps}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok: the resumed session's pid carries the acting file's session id, got %s (%s)", verdict, reason)
	}
}

// mw-43v9x.3: a host that is not home holds no Mayor, so the one that is
// missing there is nothing to cure.
func TestTheMayorGoneProbeIsOKOnAHostThatIsNotHome(t *testing.T) {
	vault := mayorGoneVault(t, "mayor-2026-09-23-40")
	tmux := fakeMayorGoneTmux(t, "@2|some-other-window", "0 claude")
	home := &apptest.FakeHomeFile{Text: "desktop 2026-09-29T00:10:00Z mw@desktop"}
	check := &doctor.MayorGone{Vault: vault, Tmux: tmux, Home: home, Host: "laptop"}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok on a host that is not home, got %s (%s)", verdict, reason)
	}
}

func TestTheMayorGoneProbeStillJudgesTheHome(t *testing.T) {
	vault := mayorGoneVault(t, "mayor-2026-09-23-40")
	tmux := fakeMayorGoneTmux(t, "@2|some-other-window", "0 claude")
	for name, home := range map[string]*apptest.FakeHomeFile{
		"the home":     {Text: "laptop 2026-09-29T00:10:00Z mw@laptop"},
		"no home file": {Missing: true},
	} {
		t.Run(name, func(t *testing.T) {
			check := &doctor.MayorGone{Vault: vault, Tmux: tmux, Home: home, Host: "laptop"}
			if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty {
				t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
			}
		})
	}
}

// fakeMayorGoneTmuxFailing writes a stand-in for tmux that, for every
// subcommand, prints stderr to STDERR and exits 1, as the real tmux does when
// there is no server to ask (mw-gq6.281).
func fakeMayorGoneTmuxFailing(t *testing.T, stderr string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for tmux is a shell script")
	}
	program := filepath.Join(t.TempDir(), "tmux-stand-in")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s' >&2\nexit 1\n", stderr)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the tmux stand-in: %v", err)
	}
	return program
}

// mw-gq6.281: tmux says it has no server on STDERR, which exec's Output() kept
// out of the error, so a Mayor killed with the last window read cannot-tell
// and was never respawned.
func TestTheMayorGoneProbeIsFaultyWhenTmuxSaysThereIsNoServer(t *testing.T) {
	for name, stderr := range map[string]string{
		"no server running":   "no server running on /tmp/tmux-1000/default",
		"error connecting to": "error connecting to /tmp/tmux-1000/default (No such file or directory)",
	} {
		t.Run(name, func(t *testing.T) {
			vault := mayorGoneVault(t, "mayor-2026-09-23-40")
			check := &doctor.MayorGone{Vault: vault, Tmux: fakeMayorGoneTmuxFailing(t, stderr)}

			verdict, reason := check.Probe(context.Background())
			if verdict != application.DoctorFaulty {
				t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
			}
			if !strings.Contains(reason, "no open tmux window matches") {
				t.Errorf("expected a reason naming no matching window, got %q", reason)
			}
		})
	}
}

func TestTheMayorGoneProbeCannotTellOtherTmuxFailuresAndSaysWhatTmuxSaid(t *testing.T) {
	vault := mayorGoneVault(t, "mayor-2026-09-23-40")
	check := &doctor.MayorGone{Vault: vault, Tmux: fakeMayorGoneTmuxFailing(t, "protocol version mismatch (client 8, server 7)")}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "protocol version mismatch (client 8, server 7)") {
		t.Errorf("expected the reason to carry tmux's stderr, got %q", reason)
	}
}
