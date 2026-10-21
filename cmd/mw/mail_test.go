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
		if got := strings.Join(subs, " "); got != "inbox read reply send" {
			t.Fatalf("expected mw mail to have inbox, read, reply and send, got %q", got)
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

func TestMailSendAndReplyRefuseWithNoSeatNamingMwSeat(t *testing.T) {
	mwConfig(t, "vault = \""+t.TempDir()+"\"\nhost = \"laptop\"\n")
	t.Setenv("MW_SEAT", "")

	for _, args := range [][]string{
		{"send", "mayor", "-s", "Who am I?", "-m", "Nobody."},
		{"reply", "mw-1", "-m", "Nobody."},
	} {
		err := mailFails(t, args...)
		if !strings.Contains(err.Error(), "MW_SEAT") {
			t.Errorf("expected mw mail %s to name MW_SEAT, got: %v", strings.Join(args, " "), err)
		}
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

func TestMailInboxHasAnAllFlag(t *testing.T) {
	inbox, _, err := newRootCmd().Find([]string{"mail", "inbox"})
	if err != nil {
		t.Fatal(err)
	}
	all := inbox.Flags().Lookup("all")
	if all == nil || all.DefValue != "false" {
		t.Fatalf("expected mw mail inbox to have --all, off unless given, got %+v", all)
	}
}

// The Mayor's mail names `bd mail inbox --unread`: mw mail inbox lists only the
// unread, so --unread is accepted and changes nothing (mw-gq6.678).
func TestMailInboxAcceptsUnreadAndChangesNothing(t *testing.T) {
	inbox, _, err := newRootCmd().Find([]string{"mail", "inbox"})
	if err != nil {
		t.Fatal(err)
	}
	unread := inbox.Flags().Lookup("unread")
	if unread == nil || unread.DefValue != "false" {
		t.Fatalf("expected mw mail inbox to have --unread, off unless given, got %+v", unread)
	}
}
