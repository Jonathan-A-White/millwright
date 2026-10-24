package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw grist smoke asks for the app, and with no test key says where one is
// wanted and that it is no key mw makes: only a key the backend holds a licence
// for is of any use.
func TestGristSmokeNeedsTheAppAndATestKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_VAULT", t.TempDir())
	t.Setenv("MW_HOST", "laptop")
	t.Setenv("MW_GRIST_TEST_KEY_FILE", "")
	t.Setenv("MW_GRIST_KEY_FILE", "")
	t.Setenv("MW_POSTERN_KEY_FILE", "")
	if _, err := runMw(t, "grist", "smoke"); err == nil {
		t.Error("expected mw grist smoke to want the app")
	}
	out, err := runMw(t, "grist", "smoke", "trade-tracker")
	if err == nil || !strings.Contains(err.Error(), "no grist test key at "+home+"/.config/mw/grist-test.key") {
		t.Fatalf("expected the missing test key named, got %v\n%s", err, out)
	}
}

// mw-gq6.339: --json is the smoke alone, what a landing asks of the mw it built:
// it needs no vault, and with no test key says so on stderr, exiting non-zero,
// which the landing reads as a smoke that could not be made.
func TestGristSmokeJSONNeedsOnlyTheTestKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_VAULT", "")
	t.Setenv("MW_GRIST_TEST_KEY_FILE", "")
	t.Setenv("MW_GRIST_KEY_FILE", "")
	t.Setenv("MW_POSTERN_KEY_FILE", "")
	out, err := runMw(t, "grist", "smoke", "trade-tracker", "--json")
	if err == nil || !strings.Contains(err.Error(), "no grist test key at "+home+"/.config/mw/grist-test.key") {
		t.Fatalf("expected the missing test key named, got %v\n%s", err, out)
	}
}

// smokeRefRig is a git rig with a small tutor grind at main and an oversized one
// on the branch mw/story, named in the config's [grist-apps] as lampas; there is
// no test key and no vault, so anything beyond the size check would fail.
func smokeRefRig(t *testing.T) {
	t.Helper()
	home, rig := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_VAULT", "")
	t.Setenv("MW_GRIST_TEST_KEY_FILE", "")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", rig, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(rig, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rig, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("grinds/tutor.json", `{"grind": 1, "app": "lampas", "kind": "tutor", "versions": ["1"], "model": "sonnet", "effort": "low", "instructions": "grinds/tutor.md", "answerSchema": "schemas/tutor.json"}`)
	write("grinds/tutor.md", "SAY MAIN")
	write("schemas/tutor.json", `{"type": "object"}`)
	git("add", ".")
	git("commit", "-q", "-m", "main")
	git("checkout", "-q", "-b", "mw/story")
	write("grinds/tutor.md", strings.Repeat("x", application.GristInstructionsWarnBytes+1))
	git("commit", "-q", "-am", "big")
	git("checkout", "-q", "main")
	config := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte("[grist-apps]\nlampas = \""+rig+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mw-gq6.732: --ref is the size check alone at that ref: it warns of the
// branch's oversized instructions, with no test key and no vault, and so records
// and holds nothing; the same check at main finds nothing to say.
func TestGristSmokeRefWarnsOfTheBranchsInstructionsAndRecordsNothing(t *testing.T) {
	smokeRefRig(t)
	out, err := runGrist(t, "smoke", "lampas", "--ref", "mw/story")
	if err != nil {
		t.Fatalf("expected the size check to pass with a warning, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "warning: lampas/tutor: its instructions are 100001 bytes, over 100000") || !strings.Contains(out, "nothing sent or recorded") {
		t.Errorf("expected the size warning at the ref, got:\n%s", out)
	}
	out, err = runGrist(t, "smoke", "lampas", "--ref", "main")
	if err != nil || strings.Contains(out, "warning:") {
		t.Errorf("expected no warning at main, got %v\n%s", err, out)
	}
	if _, err := runGrist(t, "smoke", "lampas", "--ref", "mw/story", "--lift"); err == nil {
		t.Error("expected --ref with --lift to be refused")
	}
	if out, err := runGrist(t, "smoke", "lampas", "--ref", "no/such"); err == nil || !strings.Contains(out+err.Error(), "no/such") {
		t.Errorf("expected a ref that names nothing to be refused, got %v", err)
	}
}

// Without --ref the smoke is as it was: it wants the test key before it reads a grind.
func TestGristSmokeWithoutARefStillWantsTheTestKey(t *testing.T) {
	smokeRefRig(t)
	t.Setenv("MW_VAULT", t.TempDir())
	t.Setenv("MW_HOST", "laptop")
	if _, err := runGrist(t, "smoke", "lampas"); err == nil || !strings.Contains(err.Error(), "no grist test key") {
		t.Errorf("expected the missing test key, got %v", err)
	}
}

func TestGristSmokeHelpNamesRefAndItsCommittedHEAD(t *testing.T) {
	help, err := runGrist(t, "smoke", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--ref", "committed", "HEAD"} {
		if !strings.Contains(help, want) {
			t.Errorf("expected the help to name %q, got:\n%s", want, help)
		}
	}
}

// mw grist smoke --help points a branch's examples at eval --ref, since smoke
// reads the rig's main.
func TestGristSmokeHelpPointsAtEvalRefForExamples(t *testing.T) {
	out, err := runMw(t, "grist", "smoke", "--help")
	if err != nil {
		t.Fatal(err)
	}
	want := "smoke reads main; to prove an example on a branch, use mw grist eval <app> <kind> --example <name> --ref <worktree>"
	if !strings.Contains(out, want) {
		t.Errorf("expected the help to say %q, got %q", want, out)
	}
	t.Log(want)
}
