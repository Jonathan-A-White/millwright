package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTalkSayHelpRunsAndListsItsFlags(t *testing.T) {
	buf := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"talk", "say", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw talk say --help: %v", err)
	}
	for _, want := range []string{"--talk", "--turn", "--holding", "--end", "--link"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("expected the help to name %s, got:\n%s", want, buf.String())
		}
	}
}
