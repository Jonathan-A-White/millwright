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

// A foreground whole check waits nine minutes for the gate lock, the merge slot
// and a calm host, under the 590 seconds of a Builder's tool call, and keeps its
// place in the queue for ten minutes, so that the same command run again goes on
// waiting where it was; --wait (forty minutes by default, mw-gq6.938) and
// --background are the long wait, which ends in the hand-back line (mw-gq6.939).
func TestCheckWaitsNineMinutesInTheForegroundAndKeepsItsPlaceAndFortyMinutesWithWait(t *testing.T) {
	cmd := newCheckCmd()
	flag := cmd.Flags().Lookup("wait")
	if flag == nil {
		t.Fatal("expected mw check to take --wait")
	}
	if flag.DefValue != "40m0s" {
		t.Errorf("expected --wait to default to 40m for --background, got %s", flag.DefValue)
	}

	for _, c := range []struct {
		name      string
		part      int
		given     string
		wantLimit time.Duration
		wantOnce  bool
		wantKeep  time.Duration
	}{
		{"a whole check in the foreground", 0, "", application.CheckPartSlotWait, false, application.CheckKeepPlace},
		{"a whole check given a limit", 0, "15m", 15 * time.Minute, true, 0},
		{"a part keeps the nine minutes of a foreground call", 1, "", application.CheckPartSlotWait, false, application.CheckKeepPlace},
		{"a part given a limit", 1, "20m", 20 * time.Minute, false, application.CheckKeepPlace},
	} {
		cmd := newCheckCmd()
		if c.given != "" {
			if err := cmd.Flags().Set("wait", c.given); err != nil {
				t.Fatal(err)
			}
		}
		limit, once, keep := checkWaitLimit(cmd, c.part, application.CheckCallLimitsFor(""))
		if limit != c.wantLimit || once != c.wantOnce || keep != c.wantKeep {
			t.Errorf("%s: expected a limit of %s, hand back at once %v and a place kept %s, got %s, %v and %s", c.name, c.wantLimit, c.wantOnce, c.wantKeep, limit, once, keep)
		}
	}
	if application.CheckKeepPlace != 10*time.Minute {
		t.Errorf("expected a place to be kept for 10 minutes, got %s", application.CheckKeepPlace)
	}

	// A check started in the background has no tool call to fit: it waits the long
	// forty minutes unless given another.
	for given, want := range map[string]time.Duration{"": CheckWait, "15m": 15 * time.Minute} {
		cmd := newCheckCmd()
		if given != "" {
			if err := cmd.Flags().Set("wait", given); err != nil {
				t.Fatal(err)
			}
		}
		if got := checkBackgroundWait(cmd); got != want {
			t.Errorf("expected a background check given %q to wait %s, got %s", given, want, got)
		}
	}
}

// A foreground check reads BASH_MAX_TIMEOUT_MS from its own environment: unset or
// not a number it keeps to today's times, and a longer call is its budget less 50
// seconds for the call and for every wait (mw-t5shinh.14).
func TestCheckReadsItsCallFromBashMaxTimeout(t *testing.T) {
	t.Setenv("BASH_MAX_TIMEOUT_MS", "")
	if got := checkCallLimits(); got.Budget != 540*time.Second || got.Wait != 9*time.Minute || got.Keep != 10*time.Minute {
		t.Errorf("unset: expected today's 540s, 9m and 10m, got %+v", got)
	}
	t.Setenv("BASH_MAX_TIMEOUT_MS", "soon")
	if got := checkCallLimits(); got.Budget != 540*time.Second {
		t.Errorf("not a number: expected 540s, got %+v", got)
	}
	t.Setenv("BASH_MAX_TIMEOUT_MS", "3300000")
	got := checkCallLimits()
	if got.Budget != 3250*time.Second {
		t.Errorf("a 55 minute call: expected a budget of 3250s, got %+v", got)
	}
	cmd := newCheckCmd()
	for _, part := range []int{0, 1} {
		limit, once, keep := checkWaitLimit(cmd, part, got)
		if limit != 3250*time.Second || once || keep != application.CheckKeepPlace {
			t.Errorf("part %d: expected a wait of 3250s and a place kept 10m, got %s, %v and %s", part, limit, once, keep)
		}
	}
}
