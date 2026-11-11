package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
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

// mw-gq6.1081: the suite a recheck runs with reads the host's load, as mw suite's
// does, or mw-gq6.1070's wait for a calm host never fires in production.
func TestHostRecheckSuiteReadsTheHostLoadAndWaitsAsMwSuiteDoes(t *testing.T) {
	suite := hostRecheckSuite(context.Background(), rig.New(), vault.New(t.TempDir()), nil, nil, nil, 0, 0, "cloud1")
	if _, ok := suite.Load.(hostload.Proc); !ok {
		t.Errorf("expected Load to be hostload.Proc{} as mw suite sets it, got %#v", suite.Load)
	}
	if suite.Host != "cloud1" {
		t.Errorf("expected Host to be this host, got %q", suite.Host)
	}
	if suite.LoadBound != application.SuiteCalmBound {
		t.Errorf("expected LoadBound to be mw suite's calm bound %s, got %s", application.SuiteCalmBound, suite.LoadBound)
	}
}

// The dispatch's grist example recheck follows the same rule for a host far over
// its cores (mw-gq6.1083), so it is given the recheck suite's load and host.
func TestWithHostRechecksGivesTheExampleRecheckTheSuitesLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	suite := hostRecheckSuite(context.Background(), rig.New(), vault.New(t.TempDir()), nil, nil, nil, 0, 0, "cloud1")
	got := withHostRechecks(application.Dispatch{Host: "cloud1"}, suite)
	examples, ok := got.ExampleRechecks.(hostExampleRecheck)
	if !ok {
		t.Fatalf("expected a hostExampleRecheck, got %#v", got.ExampleRechecks)
	}
	if examples.calm.Load == nil || examples.calm.Host != "cloud1" || examples.calm.LoadBound != application.SuiteCalmBound {
		t.Errorf("expected the example recheck to wait for calm by the suite's rule, got %+v", examples.calm)
	}
}
