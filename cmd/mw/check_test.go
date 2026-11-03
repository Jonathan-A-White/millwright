package main

import (
	"time"

	"bytes"
	"github.com/Jonathan-A-White/millwright/application"
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

// mw check --wait is the limit of a whole check's queue for the gate lock: forty
// minutes unless the Builder passes another, so a check queued behind a long
// gate is not handed back after nine minutes (mw-gq6.938).
func TestCheckWaitsFortyMinutesForAWholeCheckUnlessGivenAnotherLimit(t *testing.T) {
	cmd := newCheckCmd()
	flag := cmd.Flags().Lookup("wait")
	if flag == nil {
		t.Fatal("expected mw check to take --wait")
	}
	if flag.DefValue != "40m0s" {
		t.Errorf("expected --wait to default to 40m, got %s", flag.DefValue)
	}

	for _, c := range []struct {
		name      string
		part      int
		given     string
		wantLimit time.Duration
		wantOnce  bool
	}{
		{"a whole check", 0, "", 40 * time.Minute, true},
		{"a whole check given a limit", 0, "15m", 15 * time.Minute, true},
		{"a part keeps the nine minutes of a foreground call", 1, "", application.CheckPartSlotWait, false},
		{"a part given a limit", 1, "20m", 20 * time.Minute, false},
	} {
		cmd := newCheckCmd()
		if c.given != "" {
			if err := cmd.Flags().Set("wait", c.given); err != nil {
				t.Fatal(err)
			}
		}
		limit, once := checkWaitLimit(cmd, c.part)
		if limit != c.wantLimit || once != c.wantOnce {
			t.Errorf("%s: expected a limit of %s and hand back at once %v, got %s and %v", c.name, c.wantLimit, c.wantOnce, limit, once)
		}
	}
}
