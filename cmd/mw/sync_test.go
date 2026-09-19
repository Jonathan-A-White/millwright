package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw sync is never run against a real vault here: it would reach the factory's
// remote. What is checked is the wiring — that the command is there, that it
// stops plainly when this machine has not been told where the vault is, and
// that a sync beads stopped leaves mw with beads' own exit status.

func TestSyncCommandStopsWhenTheMachineDoesNotKnowItsVault(t *testing.T) {
	t.Setenv("MW_VAULT", "")
	t.Setenv("MW_HOST", "")
	t.Setenv("HOME", t.TempDir())

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"sync"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected mw sync to stop when it does not know where the vault is")
	}
	if !strings.Contains(err.Error(), "MW_VAULT") {
		t.Fatalf("expected the reason to say how to set the vault, got %q", err)
	}
	if exitCode(err) != 1 {
		t.Fatalf("expected mw to leave with 1, got %d", exitCode(err))
	}
}

func TestSyncCommandIsPartOfMw(t *testing.T) {
	for _, cmd := range newRootCmd().Commands() {
		if cmd.Name() == "sync" {
			return
		}
	}
	t.Fatal("expected mw to have a sync command")
}

func TestExitCodeIsBeadsOwnWhenBeadsStoppedTheSync(t *testing.T) {
	for code, want := range map[int]int{0: 1, 1: 1, 2: 2, 3: 3, 4: 4} {
		halted := &application.SyncHalt{Code: code}
		if got := exitCode(halted); got != want {
			t.Fatalf("expected a halt on %d to leave with %d, got %d", code, want, got)
		}
	}
	blocked := &application.VaultBlocked{Host: "vps", Files: []string{"seats/mayor/ledger.md"}}
	if got := exitCode(blocked); got != application.VaultBlockedExit {
		t.Fatalf("expected a vault nobody committed to leave with %d, got %d", application.VaultBlockedExit, got)
	}
	if got := exitCode(errors.New("something else went wrong")); got != 1 {
		t.Fatalf("expected an ordinary failure to leave with 1, got %d", got)
	}
	if got := exitCode(nil); got != 0 {
		t.Fatalf("expected nothing wrong to leave with 0, got %d", got)
	}
}
