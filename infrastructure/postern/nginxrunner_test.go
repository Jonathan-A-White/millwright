package postern_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// standIn writes a stand-in program that exits with status, printing output
// to stdout and stderr, both captured by NginxRunner as one combined stream.
func standIn(t *testing.T, name, output string, status int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	path := filepath.Join(t.TempDir(), name)
	script := "#!/bin/sh\ncat <<'EOF'\n" + output + "\nEOF\nexit " + strconv.Itoa(status) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return path
}

func TestNginxRunnerTestReportsSuccess(t *testing.T) {
	program := standIn(t, "nginx", "configuration file test is successful", 0)
	r := &postern.NginxRunner{NginxProgram: program}

	out, ok, err := r.Test(context.Background())
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if !ok {
		t.Fatalf("Test reported failure: %s", out)
	}
	if !strings.Contains(out, "successful") {
		t.Fatalf("Test output = %q, want it to mention success", out)
	}
}

func TestNginxRunnerTestReportsFailureWithoutAnError(t *testing.T) {
	program := standIn(t, "nginx", "nginx: [emerg] bad thing", 1)
	r := &postern.NginxRunner{NginxProgram: program}

	out, ok, err := r.Test(context.Background())
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if ok {
		t.Fatalf("Test reported success for a failing command")
	}
	if !strings.Contains(out, "emerg") {
		t.Fatalf("Test output = %q, want it to carry the failure", out)
	}
}

func TestNginxRunnerReloadSucceeds(t *testing.T) {
	program := standIn(t, "systemctl", "", 0)
	r := &postern.NginxRunner{SystemctlProgram: program}

	if _, err := r.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
}

func TestNginxRunnerReloadReportsAnErrorOnFailure(t *testing.T) {
	program := standIn(t, "systemctl", "Failed to reload nginx.service", 1)
	r := &postern.NginxRunner{SystemctlProgram: program}

	_, err := r.Reload(context.Background())
	if err == nil {
		t.Fatal("Reload succeeded for a failing systemctl")
	}
	if !strings.Contains(err.Error(), "reload nginx failed") {
		t.Fatalf("Reload error = %q, want it to say the reload failed", err)
	}
}

func TestNginxRunnerReportsAnErrorForAProgramThatCannotRunAtAll(t *testing.T) {
	r := &postern.NginxRunner{NginxProgram: filepath.Join(t.TempDir(), "no-such-program")}
	if _, _, err := r.Test(context.Background()); err == nil {
		t.Fatal("Test succeeded for a program that does not exist")
	}
}
