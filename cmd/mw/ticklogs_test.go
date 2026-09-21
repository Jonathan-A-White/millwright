package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/ticklog"
)

// The two logs are kept under the home directory, each in a directory of its
// own: here a temp one, never the real home.
func TestHostTickLogsAreKeptUnderTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	logs := hostTickLogs()
	if err := logs.Dispatch.Append(context.Background(), "2026-09-21T12:00:00Z ok: nothing ready"); err != nil {
		t.Fatal(err)
	}
	if err := logs.Millhand.Append(context.Background(), "2026-09-21T12:00:00Z quiet"); err != nil {
		t.Fatal(err)
	}

	for dir, want := range map[string]string{
		DispatchStateDir:     "2026-09-21T12:00:00Z ok: nothing ready\n",
		MillhandTickStateDir: "2026-09-21T12:00:00Z quiet\n",
	} {
		held, err := os.ReadFile(filepath.Join(home, dir, ticklog.File))
		if err != nil {
			t.Fatalf("expected a log in %s: %v", dir, err)
		}
		if string(held) != want {
			t.Errorf("%s holds %q, expected %q", dir, held, want)
		}
	}
	if DispatchStateDir == MillhandTickStateDir {
		t.Fatal("expected the two logs to be kept apart")
	}
}
