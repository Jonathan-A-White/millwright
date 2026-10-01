package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
)

// mw dispatch is never run for real here: it would claim stories in the
// factory's own beads database and start sessions that spend fuel. What is
// checked is the wiring — that the command is there, and that it stops plainly
// when this machine has not been told what it needs to know.

// mwConfig puts a config file under a fresh home directory and makes it the
// home directory of this test.
func mwConfig(t *testing.T, contents string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("MW_VAULT", "")
	t.Setenv("MW_HOST", "")
	t.Setenv("MW_CAP", "")
}

// dispatchFails runs mw dispatch and returns the reason it would not run.
func dispatchFails(t *testing.T, args ...string) error {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"dispatch"}, args...))

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected mw dispatch to stop, got: %s", out)
	}
	return err
}

func TestDispatchCommandIsPartOfMw(t *testing.T) {
	for _, cmd := range newRootCmd().Commands() {
		if cmd.Name() == "dispatch" {
			return
		}
	}
	t.Fatal("expected mw to have a dispatch command")
}

func TestDispatchStopsWhenTheMachineDoesNotKnowItsVault(t *testing.T) {
	mwConfig(t, "host = \"vps\"\n")

	if err := dispatchFails(t); !strings.Contains(err.Error(), "MW_VAULT") {
		t.Fatalf("expected the reason to say how to set the vault, got %q", err)
	}
}

func TestDispatchStopsWhenNoRigIsCheckedOutHere(t *testing.T) {
	mwConfig(t, "vault = \"/nowhere/vault\"\nhost = \"vps\"\n")

	err := dispatchFails(t, "--dry-run")
	for _, want := range []string{"rig", "vps", "rigs"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected the reason to mention %q, got %q", want, err)
		}
	}
}

func TestDispatchStopsOnACapThatWouldStartNothing(t *testing.T) {
	mwConfig(t, "vault = \"/nowhere/vault\"\nhost = \"vps\"\ncap = 0\n\n[rigs]\nmillwright = \"/nowhere/millwright\"\n")

	if err := dispatchFails(t, "--dry-run"); !strings.Contains(err.Error(), "cap") {
		t.Fatalf("expected the reason to be the cap, got %q", err)
	}
}

// mw-gq6.140: a second mw dispatch, run while another holds this host's dispatch
// lock, says so, claims nothing and is not a failure. The lock is a real flock
// in the dispatch state directory; the vault is nowhere, so any attempt at the
// tracker would fail loudly.
func TestDispatchLeavesQuietlyWhileAnotherDispatchHoldsTheHostLock(t *testing.T) {
	mwConfig(t, "vault = \"/nowhere/vault\"\nhost = \"vps\"\n\n[rigs]\nmillwright = \"/nowhere/millwright\"\n")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("finding the home directory: %v", err)
	}
	release, taken, err := hostlock.NewTry(filepath.Join(home, DispatchStateDir), hostlock.DispatchFile).TryTake(context.Background())
	if err != nil || !taken {
		t.Fatalf("expected to take the dispatch lock first, got taken %v, error %v", taken, err)
	}
	// The flock lives as long as the *os.File behind release: drop release and a
	// garbage collection closes the file and frees the lock under the test's feet
	// (mw-gq6.197). Holding it to the end of the test keeps the lock; the
	// collections here make sure a lock that was not held would have been lost.
	defer release()
	runtime.GC()
	runtime.GC()

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"dispatch"})
	if err := root.Execute(); err != nil {
		t.Fatalf("expected a dispatch that finds another running to exit 0, got %v: %s", err, out)
	}
	if !strings.Contains(out.String(), "another mw dispatch is running here; nothing done") {
		t.Fatalf("expected it to say another dispatch is running, got %q", out)
	}
}
