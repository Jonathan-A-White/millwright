package main

import (
	"bytes"
	"strings"
	"testing"
)

// mw mail is not run against the factory's own beads database here: what is
// checked is the wiring, and that a command that would write refuses first,
// before it has reached for any vault.

func TestMailCommandsArePartOfMw(t *testing.T) {
	for _, cmd := range newRootCmd().Commands() {
		if cmd.Name() != "mail" {
			continue
		}
		var subs []string
		for _, sub := range cmd.Commands() {
			subs = append(subs, sub.Name())
		}
		if got := strings.Join(subs, " "); got != "inbox read send" {
			t.Fatalf("expected mw mail to have inbox, read and send, got %q", got)
		}
		return
	}
	t.Fatal("expected mw to have a mail command")
}

// mailFails runs mw mail and returns the reason it would not.
func mailFails(t *testing.T, args ...string) error {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"mail"}, args...))

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected mw mail %s to stop, got: %s", strings.Join(args, " "), out)
	}
	return err
}

func TestMailSendRefusesWithNoSeatNamingMwSeat(t *testing.T) {
	mwConfig(t, "vault = \""+t.TempDir()+"\"\nhost = \"laptop\"\n")
	t.Setenv("MW_SEAT", "")

	err := mailFails(t, "send", "mayor", "-s", "Who am I?", "-m", "Nobody.")
	if !strings.Contains(err.Error(), "MW_SEAT") {
		t.Errorf("expected the refusal to name MW_SEAT, got: %v", err)
	}
}

func TestMailInboxAndReadRefuseWithNoSeatAndNoAs(t *testing.T) {
	mwConfig(t, "vault = \""+t.TempDir()+"\"\nhost = \"laptop\"\n")
	t.Setenv("MW_SEAT", "")

	for _, args := range [][]string{{"inbox"}, {"read", "mw-1"}} {
		err := mailFails(t, args...)
		if !strings.Contains(err.Error(), "MW_SEAT") {
			t.Errorf("expected mw mail %s to name MW_SEAT, got: %v", strings.Join(args, " "), err)
		}
	}
}
