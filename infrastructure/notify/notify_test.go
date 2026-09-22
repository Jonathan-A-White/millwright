package notify

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

// TestNotifyWithNoNotifierDoesNothing is the common case: neither a VPS nor
// most CI sandboxes have notify-send on the PATH, and that is not a fault.
func TestNotifyWithNoNotifierDoesNothing(t *testing.T) {
	if _, err := exec.LookPath(Program); err == nil {
		t.Skipf("%s is on this machine's PATH; this test only covers a host with none", Program)
	}
	if err := New().Notify(context.Background(), "mw: sync halted on vps since 2026-09-22T01:35:00Z: conflict"); err != nil {
		t.Fatalf("expected a host with no notifier to do nothing, got %v", err)
	}
}

// TestNotifyRunsTheNotifierWhenThereIsOne stands in a notifier of its own on
// the PATH, so the test never depends on what is really installed here.
func TestNotifyRunsTheNotifierWhenThereIsOne(t *testing.T) {
	dir := t.TempDir()
	marker := dir + "/called"
	script := "#!/bin/sh\necho \"$@\" > " + marker + "\n"
	path := dir + "/" + Program
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in notifier: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := New().Notify(context.Background(), "sync halted on vps"); err != nil {
		t.Fatalf("expected the stand-in notifier to run, got %v", err)
	}
	held, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("expected the stand-in notifier to have run, reading %s: %v", marker, err)
	}
	if got := string(held); got != Title+" sync halted on vps\n" {
		t.Fatalf("expected the notifier called with %q and the line, got %q", Title, got)
	}
}
