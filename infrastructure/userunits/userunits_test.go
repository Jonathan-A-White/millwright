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
