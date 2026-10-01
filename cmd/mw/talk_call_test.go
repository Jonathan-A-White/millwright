package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTalkCallHelpRunsAndListsItsFlags(t *testing.T) {
	buf := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"talk", "call", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw talk call --help: %v", err)
	}
	for _, flag := range []string{"--link", "--chain"} {
		if !strings.Contains(buf.String(), flag) {
			t.Errorf("expected the help to name %s, got:\n%s", flag, buf.String())
		}
	}
}
