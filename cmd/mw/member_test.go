package main

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/spf13/pflag"
)

// mw member init --help documents every flag, and the steps it cannot do.
func TestMemberInitHelpDocumentsEveryFlagAndTheHandSteps(t *testing.T) {
	root := newRootCmd()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"member", "init", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw member init --help: %v", err)
	}
	help := out.String()

	init, _, err := newRootCmd().Find([]string{"member", "init"})
	if err != nil {
		t.Fatal(err)
	}
	init.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		if !strings.Contains(help, "--"+f.Name) || f.Usage == "" || !strings.Contains(help, f.Usage) {
			t.Errorf("--help does not document --%s", f.Name)
		}
	})
	for _, hand := range []string{"adduser", "claude login", "enable-linger", "git remote", "BY HAND", "mw member remove", "deletes nothing"} {
		if !strings.Contains(help, hand) {
			t.Errorf("--help does not mention %q", hand)
		}
	}
}

func TestMemberInitRequiresItsFlags(t *testing.T) {
	root := newRootCmd()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"member", "init", "--name", "luke"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "required flag(s)") {
		t.Fatalf("mw member init with only --name = %v, want a refusal naming the required flags", err)
	}
}

// The starter kit's Mayor procedures carry the first-sitting Onboarding, and
// Boot sends the Mayor there before anything else when the vault has no git
// remote or no rig.
func TestStarterKitMayorProceduresCarryOnboarding(t *testing.T) {
	kit := millwright.Template()
	procedures, err := fs.ReadFile(kit, "seats/mayor/procedures.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(procedures)

	_, onboarding, found := strings.Cut(text, "\n## Onboarding (the first sitting)\n")
	if !found {
		t.Fatalf("procedures.md has no '## Onboarding (the first sitting)' section")
	}
	if next := strings.Index(onboarding, "\n## "); next >= 0 {
		onboarding = onboarding[:next]
	}
	for _, want := range []string{
		"gh auth login", "device", "gh repo list", "one question at a time", "recommend",
		"gh repo create", "--private", "git remote add origin", "git push",
		"~/.config/mw/", "0600", "never print", "Postern", "card", "handoff",
	} {
		if !strings.Contains(onboarding, want) {
			t.Errorf("the Onboarding section does not mention %q", want)
		}
	}

	_, boot, _ := strings.Cut(text, "\n## Boot\n")
	boot, _, _ = strings.Cut(boot, "\n## ")
	if !strings.Contains(boot, "no git remote or no rig") || !strings.Contains(boot, "Onboarding") {
		t.Errorf("Boot does not say to run Onboarding first when the vault has no git remote or no rig:\n%s", boot)
	}
	if len(procedures) > 8*1024 {
		t.Errorf("procedures.md is %d bytes; it says to keep it under 8 KB", len(procedures))
	}

	// Both harnesses boot from the same kit: the codex AGENTS.md is the
	// starter kit's CLAUDE.md text, and that names the Mayor's procedures.
	boots, err := fs.ReadFile(kit, "CLAUDE.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(boots), "seats/mayor/procedures.md") {
		t.Errorf("the kit's boot text (CLAUDE.md, and AGENTS.md on codex) does not name seats/mayor/procedures.md")
	}
}

// What mw member init says is still owed names Onboarding in place of the bare
// git remote hand item.
func TestMemberInitStillOwedNamesOnboarding(t *testing.T) {
	for _, harness := range []domain.Harness{domain.HarnessClaude, domain.HarnessCodex} {
		out := application.MemberReport{
			Name: "luke", Holder: "jon", Port: 3310, Dir: "/home/luke/luke-vault",
			Harness: harness, LingerKnown: true, LingerSet: true,
		}.String()
		if !strings.Contains(out, "Onboarding") {
			t.Errorf("%s: still-owed text does not name Onboarding:\n%s", harness, out)
		}
		if strings.Contains(out, "Make a private remote") || strings.Contains(out, "remote add origin <url>") {
			t.Errorf("%s: still-owed text still hands the bare git remote item:\n%s", harness, out)
		}
	}
}
