package rig_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// A test-runner ticket names the story it is taken for, whichever path takes it
// (mw-gq6.1491): mw status counts a session waiting for a slot by the story's id
// in the ticket's holder line, and the whole tests, the changed specs and the
// after-landing command took theirs with the rig alone.
func TestEveryTestRunTicketNamesItsStory(t *testing.T) {
	slots := t.TempDir()
	runs := rig.NewTestRuns(slots, 1)
	// What a command reads from the one slot's file is what mw status would read
	// of its ticket while it runs.
	saysWho := func(into string) string {
		return "head -n 1 " + filepath.Join(slots, rig.TestRunsPrefix+"0.lock") + " > " + into
	}
	forStory := application.WithTestRunFor(context.Background(), "mw-x")
	read := func(path string) string {
		t.Helper()
		said, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("the command did not say who held the slot: %v", err)
		}
		return strings.TrimSpace(string(said))
	}

	t.Run("the whole tests", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "who")
		checks := rig.NewChecks(rig.WithTestRuns(runs), rig.WithCommands(map[string]string{"lampas": saysWho(out)}))
		if _, err := checks.Run(forStory, "lampas", t.TempDir()); err != nil {
			t.Fatal(err)
		}
		if got, want := read(out), "tests of lampas for mw-x"; got != want {
			t.Errorf("expected the ticket %q, got %q", want, got)
		}
	})

	t.Run("the changed specs", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "who")
		dir := specsRepo(t, "tests/e2e/a.spec.ts")
		checks := rig.NewChecks(rig.WithTestRuns(runs), rig.WithSpecCommands(map[string]string{"lampas": saysWho(out) + " # {specs}"}))
		if _, err := checks.RunSpecs(forStory, "lampas", dir, "main"); err != nil {
			t.Fatal(err)
		}
		if got, want := read(out), "changed specs of lampas for mw-x"; got != want {
			t.Errorf("expected the ticket %q, got %q", want, got)
		}
	})

	t.Run("the after-landing command", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "who")
		after := rig.NewAfterLanding(rig.WithAfterTestRuns(runs), rig.WithAfterCommands(map[string]string{"lampas": saysWho(out)}))
		if _, err := after.Run(forStory, "lampas", t.TempDir()); err != nil {
			t.Fatal(err)
		}
		if got, want := read(out), "after-landing of lampas for mw-x"; got != want {
			t.Errorf("expected the ticket %q, got %q", want, got)
		}
	})
}
