package main

import (
	"bytes"
	"strings"
	"testing"
)

func helpOf(t *testing.T, args ...string) string {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("mw %s: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

// mw path set --help names size and its values, and says --from leaves it
// alone: the Deputy wrote 'SIZE: S.' into a body for want of this (mw-gq6.513).
func TestPathSetHelpNamesSizeItsValuesAndThatFromDoesNotCopyIt(t *testing.T) {
	help := helpOf(t, "path", "set", "--help")
	if !strings.Contains(help, "size=XS|S|M|L|XL") {
		t.Errorf("expected the help to name size=XS|S|M|L|XL, got:\n%s", help)
	}
	if !strings.Contains(help, "--from does not copy size") {
		t.Errorf("expected the help to say --from does not copy size, got:\n%s", help)
	}
}

// mw file --help names size among the keys a story's overrides may carry.
func TestFileHelpNamesSize(t *testing.T) {
	help := helpOf(t, "file", "--help")
	if !strings.Contains(help, `"size", one of XS|S|M|L|XL`) {
		t.Errorf("expected the file help to name size and its values, got:\n%s", help)
	}
}
