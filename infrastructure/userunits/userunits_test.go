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
