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

// mw path set --help describes --after, --hold and release_on_path
// (mw-t5shinh.16).
func TestPathSetHelpDescribesAfterHoldAndReleaseOnPath(t *testing.T) {
	help := helpOf(t, "path", "set", "--help")
	for _, want := range []string{"--after", "--hold", "release_on_path = true", "released on path set", "held: <id>: <reason>"} {
		if !strings.Contains(help, want) {
			t.Errorf("expected the help to say %q, got:\n%s", want, help)
		}
	}
}

func TestParseAfterReadsIdsAndNone(t *testing.T) {
	ids, err := parseAfter("mw-a, mw-b")
	if err != nil || len(ids) != 2 {
		t.Errorf("parseAfter(ids) = %v, %v; want two ids", ids, err)
	}
	none, err := parseAfter("none")
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("parseAfter(none) = %#v, %v; want an empty list that is not nil", none, err)
	}
	for _, bad := range []string{"", "  ", "mw-a,none"} {
		if _, err := parseAfter(bad); err == nil {
			t.Errorf("parseAfter(%q) was accepted, want a refusal", bad)
		}
	}
}
