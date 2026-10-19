package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// mw member init --help documents every flag, and the steps it cannot do.
func TestMemberInitHelpDocumentsEveryFlagAndTheHandSteps(t *testing.T) {
	root := newRootCmd()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"member", "init", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw member init --help: %v", err)
	}
	help := out.String()

	init, _, err := newRootCmd().Find([]string{"member", "init"})
	if err != nil {
		t.Fatal(err)
	}
	init.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		if !strings.Contains(help, "--"+f.Name) || f.Usage == "" || !strings.Contains(help, f.Usage) {
			t.Errorf("--help does not document --%s", f.Name)
		}
	})
	for _, hand := range []string{"adduser", "claude login", "enable-linger", "git remote", "BY HAND", "mw member remove", "deletes nothing"} {
		if !strings.Contains(help, hand) {
			t.Errorf("--help does not mention %q", hand)
		}
	}
}

func TestMemberInitRequiresItsFlags(t *testing.T) {
	root := newRootCmd()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"member", "init", "--name", "luke"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "required flag(s)") {
		t.Fatalf("mw member init with only --name = %v, want a refusal naming the required flags", err)
	}
}
