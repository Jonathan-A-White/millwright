package main

import (
	"bytes"
	"strings"
	"testing"
)

// mw check is wired into the tree and takes exactly one story.
func TestCheckTakesOneStory(t *testing.T) {
	standInBeads(t)

	for _, args := range [][]string{{"check"}, {"check", "mw-1", "mw-2"}} {
		root := newRootCmd()
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Fatalf("expected mw %s to be refused", strings.Join(args, " "))
		}
	}
}

// A check that cannot even find its story fails, which is what leaves mw with a
// non-zero status, and it prints no verdict for a branch it never read.
func TestCheckOfAStoryTheTrackerDoesNotHoldFails(t *testing.T) {
	standInBeads(t)

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"check", "mw-nothing.1"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected the check of a story that is not there to fail, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "mw-nothing.1") {
		t.Fatalf("expected the error to name the story, got %v", err)
	}
	if strings.Contains(out.String(), "passed") {
		t.Fatalf("expected no verdict, got:\n%s", out)
	}
}
