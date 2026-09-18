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
	if approval(root, true) == nil {
		t.Fatal("expected --approve to release the plan without asking")
	}
	approved, err := approval(root, true)(context.Background(), application.FiledPlan{})
	if err != nil || !approved {
		t.Fatalf("expected --approve to approve, got %v (%v)", approved, err)
	}
}
