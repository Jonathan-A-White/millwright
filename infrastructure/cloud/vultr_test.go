package cloud_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/cloud"
)

// standIn writes a wrapper that appends its arguments to calls and then runs
// body, and returns its path and the calls file. The tests here are not
// parallel: a fork while another test still holds its new script open for
// writing fails with "text file busy".
func standIn(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	path := filepath.Join(dir, "vultr-boost")
	script := "#!/bin/sh\necho \"$*\" >>" + calls + "\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, calls
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestUpAndDownRunTheWrapperForTheBoxFromTheSnapshot(t *testing.T) {
	command, calls := standIn(t, "echo made")
	var out strings.Builder
	v := cloud.Vultr{Command: command, Snapshot: "snap-1", Out: &out}
	if err := v.Up(context.Background(), "cloud2"); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := v.Down(context.Background(), "cloud2"); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got, want := read(t, calls), "up --yes --name cloud2 --snapshot snap-1\ndown --yes --name cloud2\n"; got != want {
		t.Errorf("expected the wrapper run as %q, got %q", want, got)
	}
	if !strings.Contains(out.String(), "made") {
		t.Errorf("expected the wrapper's output copied, got %q", out.String())
	}
}

func TestAFailedUpNamesTheWrappersLastLine(t *testing.T) {
	command, _ := standIn(t, "echo 'step one'; echo 'vultr-boost: terraform apply failed (status 1)' >&2; exit 1")
	err := cloud.Vultr{Command: command}.Up(context.Background(), "cloud1")
	if err == nil || !strings.Contains(err.Error(), "terraform apply failed") {
		t.Errorf("expected the failure to name the wrapper's last line, got %v", err)
	}
}

func TestBilledReadsTheSpendOrUnknown(t *testing.T) {
	command, calls := standIn(t, "echo 12.34")
	usd, known, err := cloud.Vultr{Command: command}.Billed(context.Background(), []string{"cloud1", "cloud2"})
	if err != nil || !known || usd != 12.34 {
		t.Errorf("expected $12.34 known, got %v %v %v", usd, known, err)
	}
	if got := read(t, calls); got != "spend --name cloud1 --name cloud2\n" {
		t.Errorf("expected spend asked for both boxes, got %q", got)
	}

	command, _ = standIn(t, "echo "+cloud.Unknown)
	if usd, known, err := (cloud.Vultr{Command: command}).Billed(context.Background(), nil); err != nil || known || usd != 0 {
		t.Errorf("expected unknown to be no answer and no error, got %v %v %v", usd, known, err)
	}
}
