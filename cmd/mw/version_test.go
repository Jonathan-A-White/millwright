package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommandPrintsAVersion(t *testing.T) {
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("mw version failed: %v", err)
	}

	printed := strings.TrimSpace(out.String())
	if printed == "" {
		t.Fatal("mw version printed nothing")
	}
	if !strings.Contains(printed, version) {
		t.Fatalf("expected %q to contain the version %q", printed, version)
	}
}

func TestRootCommandIsNamedMw(t *testing.T) {
	if got := newRootCmd().Name(); got != "mw" {
		t.Fatalf("expected the binary to be named mw, got %q", got)
	}
}
