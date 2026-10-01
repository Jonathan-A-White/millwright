package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw file is never run against the factory's own vault here: it writes, and
// what it would write is an epic. What is checked is the wiring — that the
// command is there, that a plan that is not a plan stops it before it reaches
// beads at all, and that nothing but a plain yes releases a filed plan.

func TestFileCommandIsPartOfMw(t *testing.T) {
	for _, cmd := range newRootCmd().Commands() {
		if cmd.Name() == "file" {
			return
		}
	}
	t.Fatal("expected mw to have a file command")
}

func TestFileStopsOnAPlanThatIsNotThere(t *testing.T) {
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"file", filepath.Join(t.TempDir(), "nope.json")})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected mw file to stop when the plan is not there")
	}
	if !strings.Contains(err.Error(), "reading the plan") {
		t.Fatalf("expected the reason to say the plan could not be read, got %q", err)
	}
}

func TestFileStopsOnAPlanItCannotRead(t *testing.T) {
	plan := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(plan, []byte(`{"epic":{"ttile":"typo"}}`), 0o600); err != nil {
		t.Fatalf("writing the plan: %v", err)
	}

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"file", plan})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected mw file to stop on a plan with a key the format does not know")
	}
	if !strings.Contains(err.Error(), "this is not a plan") {
		t.Fatalf("expected the reason to say it is not a plan, got %q", err)
	}
}

func TestFileStopsWhenTheMachineDoesNotKnowItsVault(t *testing.T) {
	t.Setenv("MW_VAULT", "")
	t.Setenv("HOME", t.TempDir())

	plan := filepath.Join(t.TempDir(), "plan.json")
	written := `{"epic":{"key":"e","title":"E","defaults":{"rig":"millwright","branch":"main","harness":"claude","model":"opus","effort":"high","host":"vps"}},` +
		`"stories":[{"key":"s","title":"S","acceptance":"it passes","needs":[]}]}`
	if err := os.WriteFile(plan, []byte(written), 0o600); err != nil {
		t.Fatalf("writing the plan: %v", err)
	}

	out, printed := &bytes.Buffer{}, &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(printed)
	root.SetArgs([]string{"file", plan})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected mw file to stop when it does not know where the vault is")
	}
	if !strings.Contains(err.Error(), "MW_VAULT") {
		t.Fatalf("expected the reason to say how to set the vault, got %q", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected nothing to be printed before the vault is known, got %q", out)
	}
}

func TestOnlyAPlainYesReleasesAPlan(t *testing.T) {
	for answer, want := range map[string]bool{
		"y\n": true, "Y\n": true, "yes\n": true, " YES \n": true,
		"n\n": false, "\n": false, "": false, "sure\n": false, "yeah\n": false,
	} {
		out := &bytes.Buffer{}
		got, err := confirm(strings.NewReader(answer), out, "Release them? [y/N] ")
		if err != nil {
			t.Fatalf("asking about %q: %v", answer, err)
		}
		if got != want {
			t.Errorf("expected %q to mean %v, got %v", answer, want, got)
		}
		if !strings.Contains(out.String(), "Release them?") {
			t.Errorf("expected the question to be asked, got %q", out)
		}
	}
}

func TestNobodyIsAskedWhenNobodyIsThere(t *testing.T) {
	root := newRootCmd()
	root.SetIn(strings.NewReader("y\n"))

	// A pipe is not a person: an unattended run leaves the plan held.
	if approval(root, false) != nil {
		t.Error("expected a run with nobody at the terminal to leave the plan held")
	}
	// Nor is /dev/null, which is a character device like a terminal is.
	empty, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	defer empty.Close()
	root.SetIn(empty)
	if approval(root, false) != nil {
		t.Error("expected a run given no input at all to leave the plan held")
	}
	if approval(root, true) == nil {
		t.Fatal("expected --approve to release the plan without asking")
	}
	approved, err := approval(root, true)(context.Background(), application.FiledPlan{})
	if err != nil || !approved {
		t.Fatalf("expected --approve to approve, got %v (%v)", approved, err)
	}
}

// A rig's file in the vault makes mw file refuse a plan before it reaches beads:
// PATH has no bd here, so a plan that got as far as the tracker would fail
// differently.
func TestFileRefusesAPlanThatLacksWhatTheRigsFileRequires(t *testing.T) {
	vaultDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vaultDir, "rigs"), 0o755); err != nil {
		t.Fatal(err)
	}
	rules := "epic_sections = [\"Demo\"]\nepic_last_story_labels = [\"demo\"]\n"
	if err := os.WriteFile(filepath.Join(vaultDir, "rigs", "spell-forge.toml"), []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MW_VAULT", vaultDir)
	t.Setenv("MW_HOST", "vps")
	t.Setenv("PATH", t.TempDir())

	plan := filepath.Join(t.TempDir(), "plan.json")
	written := `{"epic":{"key":"e","title":"E","description":"Cast.","defaults":{"rig":"spell-forge","branch":"main","harness":"claude","model":"opus","effort":"high","host":"vps"}},` +
		`"stories":[{"key":"s","title":"S","acceptance":"it passes","needs":[]}]}`
	if err := os.WriteFile(plan, []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) error {
		root := newRootCmd()
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(append([]string{"file", plan}, args...))
		return root.Execute()
	}

	err := run()
	if err == nil || !strings.Contains(err.Error(), "the Demo section") || !strings.Contains(err.Error(), "the last story labelled demo") {
		t.Fatalf("expected both missing requirements to be named, got %v", err)
	}
	err = run("--waive", "Demo", "--waive", "demo")
	if err == nil || !strings.Contains(err.Error(), "--because") {
		t.Fatalf("expected a waiver without the Governor's words to be refused, got %v", err)
	}
}
