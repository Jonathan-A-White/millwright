package userunits_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/userunits"
)

// standIn is a systemctl that logs its arguments and fails for the unit named
// bad.service; it never reaches the real user manager.
func standIn(t *testing.T) (userunits.Systemctl, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\ncase \"$*\" in *bad.service*) echo 'Job failed' >&2; exit 1;; esac\n"
	bin := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return userunits.Systemctl{Bin: bin}, log
}

func TestStartStartsTheUserUnitAndReportsAFailureWithWhatSystemctlSaid(t *testing.T) {
	sc, log := standIn(t)
	if err := sc.Start(context.Background(), "mw-dispatch.service"); err != nil {
		t.Fatal(err)
	}
	err := sc.Start(context.Background(), "bad.service")
	if err == nil || !strings.Contains(err.Error(), "Job failed") {
		t.Fatalf("expected the failure with what systemctl said, got %v", err)
	}
	got, _ := os.ReadFile(log)
	if string(got) != "--user start mw-dispatch.service\n--user start bad.service\n" {
		t.Fatalf("systemctl was called as:\n%s", got)
	}
}

func TestInstalledIsWhetherSystemctlKnowsTheUnit(t *testing.T) {
	sc, _ := standIn(t)
	if !sc.Installed(context.Background(), "mw-dispatch.service") || sc.Installed(context.Background(), "bad.service") {
		t.Fatal("expected a known unit installed and a failing one not")
	}
}

func TestTryRestartRestartsARunningUnitAndLeavesAStoppedOneBe(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	// A systemctl that knows every unit and says stopped.service is not running.
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\ncase \"$*\" in *is-active*stopped.service*) exit 3;; esac\n"
	bin := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sc := userunits.Systemctl{Bin: bin}

	if restarted, err := sc.TryRestart(context.Background(), "mw-view-follow.service"); err != nil || !restarted {
		t.Fatalf("expected a running unit restarted, got %v, %v", restarted, err)
	}
	if restarted, err := sc.TryRestart(context.Background(), "stopped.service"); err != nil || restarted {
		t.Fatalf("expected a stopped unit left be without error, got %v, %v", restarted, err)
	}
	got, _ := os.ReadFile(log)
	want := "--user cat mw-view-follow.service\n--user is-active --quiet mw-view-follow.service\n--user try-restart mw-view-follow.service\n" +
		"--user cat stopped.service\n--user is-active --quiet stopped.service\n"
	if string(got) != want {
		t.Fatalf("systemctl was called as:\n%s\nwant:\n%s", got, want)
	}
}

func TestTryRestartReportsAFailedRestartWithWhatSystemctlSaid(t *testing.T) {
	sc, _ := standIn(t)
	// bad.service is not installed for the stand-in (cat fails), so it is left be.
	if restarted, err := sc.TryRestart(context.Background(), "bad.service"); err != nil || restarted {
		t.Fatalf("expected an unknown unit left be, got %v, %v", restarted, err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in *try-restart*) echo 'Job failed' >&2; exit 1;; esac\n"
	bin := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := userunits.Systemctl{Bin: bin}.TryRestart(context.Background(), "mw-view-follow.service")
	if err == nil || !strings.Contains(err.Error(), "Job failed") {
		t.Fatalf("expected the failure with what systemctl said, got %v", err)
	}
}

// reloadStandIn is a systemctl that lists mw-dispatch.service and
// mw-health.timer as loaded and answers `show` for them from needs (unit ->
// yes/no), logging every call. A `daemon-reload` fails when reloadFails.
func reloadStandIn(t *testing.T, needs map[string]string, reloadFails bool) (userunits.Systemctl, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	var show strings.Builder
	for _, unit := range []string{"mw-dispatch.service", "mw-health.timer"} {
		show.WriteString("echo Id=" + unit + "; echo NeedDaemonReload=" + needs[unit] + "; echo;")
	}
	fail := "exit 0"
	if reloadFails {
		fail = "echo 'Access denied' >&2; exit 1"
	}
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\n" +
		"case \"$*\" in\n" +
		"*list-units*) echo 'mw-dispatch.service loaded active running Dispatch'; echo 'mw-health.timer loaded active waiting Health';;\n" +
		"*show*) " + show.String() + " true;;\n" +
		"*daemon-reload*) " + fail + ";;\n" +
		"esac\n"
	bin := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return userunits.Systemctl{Bin: bin}, log
}

func TestStaleUnitsNamesEveryLoadedMwUnitThatNeedsADaemonReload(t *testing.T) {
	sc, _ := reloadStandIn(t, map[string]string{"mw-dispatch.service": "yes", "mw-health.timer": "no"}, false)

	stale, err := sc.StaleUnits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0] != "mw-dispatch.service" {
		t.Fatalf("expected only mw-dispatch.service stale, got %q", stale)
	}
}

func TestReloadStaleRunsDaemonReloadWhenAUnitFileChangedAndNotOtherwise(t *testing.T) {
	sc, log := reloadStandIn(t, map[string]string{"mw-dispatch.service": "no", "mw-health.timer": "no"}, false)
	stale, err := sc.ReloadStale(context.Background())
	if err != nil || len(stale) != 0 {
		t.Fatalf("expected nothing stale and no error, got %q, %v", stale, err)
	}
	if got, _ := os.ReadFile(log); strings.Contains(string(got), "daemon-reload") {
		t.Fatalf("expected no daemon-reload when no unit changed, calls were:\n%s", got)
	}

	sc, log = reloadStandIn(t, map[string]string{"mw-dispatch.service": "yes", "mw-health.timer": "yes"}, false)
	stale, err = sc.ReloadStale(context.Background())
	if err != nil || len(stale) != 2 {
		t.Fatalf("expected both units named stale, got %q, %v", stale, err)
	}
	if got, _ := os.ReadFile(log); !strings.Contains(string(got), "--user daemon-reload\n") {
		t.Fatalf("expected systemctl --user daemon-reload to have run, calls were:\n%s", got)
	}
}

func TestReloadStaleSaysWhatSystemctlSaidWhenTheReloadFails(t *testing.T) {
	sc, _ := reloadStandIn(t, map[string]string{"mw-dispatch.service": "yes"}, true)

	stale, err := sc.ReloadStale(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("expected the failure with what systemctl said, got %v", err)
	}
	if len(stale) != 1 {
		t.Errorf("expected the unit still named, got %q", stale)
	}
}

// systemStandIn is a systemctl whose system manager has loaded
// mw-seat-tmux.service and mw-doctor.timer (needs: unit -> yes/no); a call with
// --user fails, as it does on a host with no user manager, and every call is
// logged.
func systemStandIn(t *testing.T, needs map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	var show strings.Builder
	for _, unit := range []string{"mw-seat-tmux.service", "mw-doctor.timer"} {
		show.WriteString("echo Id=" + unit + "; echo NeedDaemonReload=" + needs[unit] + "; echo;")
	}
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\n" +
		"case \"$1\" in --user) echo 'Failed to connect to bus' >&2; exit 1;; esac\n" +
		"case \"$*\" in\n" +
		"*list-units*) echo 'mw-seat-tmux.service loaded active running Seat'; echo 'mw-doctor.timer loaded active waiting Doctor';;\n" +
		"*show*) " + show.String() + " true;;\n" +
		"esac\n"
	bin := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func TestReloadStaleSystemRunsADaemonReloadWithoutUserWhenARootHostsSystemUnitChanged(t *testing.T) {
	bin, log := systemStandIn(t, map[string]string{"mw-seat-tmux.service": "yes", "mw-doctor.timer": "no"})
	sc := userunits.Systemctl{Bin: bin, Root: func() bool { return true }}

	stale, err := sc.ReloadStaleSystem(context.Background())
	if err != nil || len(stale) != 1 || stale[0] != "mw-seat-tmux.service" {
		t.Fatalf("expected mw-seat-tmux.service named stale, got %q, %v", stale, err)
	}
	got, _ := os.ReadFile(log)
	if !strings.Contains(string(got), "daemon-reload\n") || strings.Contains(string(got), "--user") {
		t.Fatalf("expected a daemon-reload with no --user, calls were:\n%s", got)
	}
}

func TestReloadStaleSystemRunsNothingWhenNoSystemUnitChanged(t *testing.T) {
	bin, log := systemStandIn(t, map[string]string{"mw-seat-tmux.service": "no", "mw-doctor.timer": "no"})
	sc := userunits.Systemctl{Bin: bin, Root: func() bool { return true }}

	stale, err := sc.ReloadStaleSystem(context.Background())
	if err != nil || len(stale) != 0 {
		t.Fatalf("expected nothing stale, got %q, %v", stale, err)
	}
	if got, _ := os.ReadFile(log); strings.Contains(string(got), "daemon-reload") {
		t.Fatalf("expected no daemon-reload, calls were:\n%s", got)
	}
}

func TestReloadStaleSystemAsksNothingWhenNotRoot(t *testing.T) {
	bin, log := systemStandIn(t, map[string]string{"mw-seat-tmux.service": "yes"})
	sc := userunits.Systemctl{Bin: bin, Root: func() bool { return false }}

	stale, err := sc.ReloadStaleSystem(context.Background())
	if err != nil || len(stale) != 0 {
		t.Fatalf("expected nothing for a non-root host, got %q, %v", stale, err)
	}
	if got, _ := os.ReadFile(log); len(got) != 0 {
		t.Fatalf("expected systemctl not to be run, calls were:\n%s", got)
	}
}

func TestStaleSystemUnitsListsTheSystemUnitsWithoutUserForTheDoctor(t *testing.T) {
	bin, log := systemStandIn(t, map[string]string{"mw-seat-tmux.service": "yes", "mw-doctor.timer": "yes"})
	stale, err := userunits.Systemctl{Bin: bin, SystemScope: true}.StaleUnits(context.Background())
	if err != nil || len(stale) != 2 {
		t.Fatalf("expected both system units stale, got %q, %v", stale, err)
	}
	if got, _ := os.ReadFile(log); strings.Contains(string(got), "--user") {
		t.Fatalf("expected no --user, calls were:\n%s", got)
	}
}
