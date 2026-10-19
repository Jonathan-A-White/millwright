package main

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestStuckIsACommandThatSaysWhatCountsAsAHeldRoot(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"stuck"})
	if err != nil || cmd.Name() != "stuck" {
		t.Fatalf("mw stuck is not a command: %v", err)
	}
	for _, want := range []string{"icebox", "grilling", "hitl", "demo", application.HeldCommentPrefix, "6 hours", "always exits 0"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("mw stuck --help does not say %q:\n%s", want, cmd.Long)
		}
	}
}

func TestIceHelpSaysItMovesTheBeadBehindItsDependents(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"ice"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"never blocks a live one", "closed bead", "cycle"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("mw ice --help does not say %q:\n%s", want, cmd.Long)
		}
	}
}

func TestSleepPassHelpSaysItReadsMwStuck(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"sleep", "pass"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmd.Long, "mw stuck") {
		t.Errorf("mw sleep pass --help does not say it reads mw stuck:\n%s", cmd.Long)
	}
}

func TestStuckHasAnInversionsFlagThatIsPlainWhenOff(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"stuck"})
	if err != nil {
		t.Fatal(err)
	}
	flag := cmd.Flags().Lookup("inversions")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("mw stuck has no --inversions flag defaulting to off: %v", flag)
	}
	if !strings.Contains(cmd.Long, "--inversions") {
		t.Errorf("mw stuck --help does not say what --inversions does:\n%s", cmd.Long)
	}
}
