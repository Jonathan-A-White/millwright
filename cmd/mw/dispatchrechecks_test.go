package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.869: every dispatch cmd/mw builds re-runs a bug's check on main before it
// claims the bug, not only the timer's. The dispatch that ends a landing (mw next)
// was built without one, and a nightly-suite bug whose spec already passed on main
// was claimed and given a Builder session (mw-5r3p30.922). A file that builds an
// application.Dispatch must give it the host's rechecks through withHostRechecks.
func TestEveryDispatchCmdMwBuildsRechecksABugOnMainFirst(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	built := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		wrapped := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "//") && strings.Contains(line, "withHostRechecks(application.Dispatch{") {
				wrapped++
			}
		}
		for n, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") || !strings.Contains(line, "application.Dispatch{") {
				continue
			}
			built++
			if !strings.Contains(line, "withHostRechecks(application.Dispatch{") || wrapped == 0 {
				t.Errorf("%s:%d builds a dispatch with no rechecks: wrap it in withHostRechecks(application.Dispatch{...}, suite), as the timer's dispatch does", file, n+1)
			}
		}
	}
	if built == 0 {
		t.Fatal("expected cmd/mw to build at least one application.Dispatch, found none: the scan reads the wrong thing")
	}
}

// withHostRechecks gives a dispatch the suite that re-runs a spec, the grist
// examples' rechecker and the host's gate lock.
func TestWithHostRechecksGivesADispatchWhatItReRunsABugWith(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	suite := application.Suite{Rigs: map[string]string{"lampas": "/rigs/lampas"}}
	got := withHostRechecks(application.Dispatch{Host: "laptop"}, suite)
	if got.Rechecks == nil || got.ExampleRechecks == nil || got.GateLock == nil {
		t.Fatalf("expected rechecks, example rechecks and a gate lock, got %+v %+v %+v", got.Rechecks, got.ExampleRechecks, got.GateLock)
	}
	if got.Host != "laptop" {
		t.Errorf("expected the rest of the dispatch kept, got host %q", got.Host)
	}
}
