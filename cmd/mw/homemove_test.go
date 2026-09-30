package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// moveHost is a laptop whose vault says the desktop is home, with stand-ins first
// on PATH for every program a move runs: each writes its name to calls, and ssh
// answers as sshExit says (0: the desktop is up; 255: it does not answer).
func moveHost(t *testing.T, sshExit int) (calls string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins are shell scripts")
	}
	mwConfig(t, "postern_data = \""+t.TempDir()+"\"\n[hands_hosts]\ndesktop = \"ssh desktop\"\n")
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "home"), []byte("desktop 2026-09-29T12:00:00Z mayor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MW_VAULT", vault)
	t.Setenv("MW_HOST", "laptop")

	dir := t.TempDir()
	calls = filepath.Join(dir, "calls")
	for name, body := range map[string]string{
		"ssh":       "echo 'ssh: connect to host desktop port 22: Connection timed out' >&2; echo 0; exit " + itoa(sshExit),
		"bd":        "exit 0",
		"git":       "exit 0",
		"systemctl": "exit 0",
	} {
		script := "#!/bin/sh\necho " + name + " \"$@\" >> '" + calls + "'\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return "255"
}

// runHomeMove runs the real command.
func runHomeMove(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"home", "move"}, args...))
	err := root.Execute()
	return out.String(), err
}

func ranWhat(t *testing.T, calls string) string {
	t.Helper()
	text, err := os.ReadFile(calls)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

func TestHomeMoveDryRunPrintsEverySixStepsAndRunsNothing(t *testing.T) {
	calls := moveHost(t, 255)

	out, err := runHomeMove(t, "laptop", "--dry-run")

	if err != nil {
		t.Fatalf("mw home move --dry-run: %v\n%s", err, out)
	}
	for _, want := range []string{"Dry run: nothing below is run", "Step 1 of 6", "Step 6 of 6", "ssh desktop", "http://laptop.mw:8787/healthz", "Way back:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
	if got := ranWhat(t, calls); got != "" {
		t.Errorf("a dry run ran programs:\n%s", got)
	}
}

func TestHomeMoveRefusesAnOldHomeThatIsUpBeforeTouchingAnything(t *testing.T) {
	calls := moveHost(t, 0)

	out, err := runHomeMove(t, "laptop", "--old-home-dead")

	if err == nil || !strings.Contains(err.Error(), "old home is up: use --planned") {
		t.Fatalf("expected the planned-path line, got %v\n%s", err, out)
	}
	if got := ranWhat(t, calls); !strings.HasPrefix(got, "ssh -o BatchMode=yes -o ConnectTimeout=10 desktop true") || strings.Count(got, "\n") != 1 {
		t.Errorf("expected only the one ssh, got:\n%s", got)
	}
}

func TestHomeMoveRefusesAnOldHomeThatDoesNotAnswerUntilToldItIsDead(t *testing.T) {
	calls := moveHost(t, 255)

	_, err := runHomeMove(t, "laptop")

	if err == nil || !strings.Contains(err.Error(), "--old-home-dead") {
		t.Fatalf("expected a refusal naming --old-home-dead, got %v", err)
	}
	if got := ranWhat(t, calls); strings.Count(got, "\n") != 1 {
		t.Errorf("expected only the one ssh, got:\n%s", got)
	}
}

func TestHomeMoveRunsOnTheHostThatBecomesHome(t *testing.T) {
	calls := moveHost(t, 255)

	_, err := runHomeMove(t, "desktop", "--old-home-dead")

	if err == nil || !strings.Contains(err.Error(), "run it on desktop") {
		t.Fatalf("expected a refusal to run it on the desktop, got %v", err)
	}
	if got := ranWhat(t, calls); got != "" {
		t.Errorf("ran programs for a move to another host:\n%s", got)
	}
}

func TestHomeMoveNeedsTheHostNamed(t *testing.T) {
	moveHost(t, 255)
	if _, err := runHomeMove(t); err == nil {
		t.Error("expected mw home move with no host to be refused")
	}
}

func TestHomeMoveIsListedUnderHome(t *testing.T) {
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetArgs([]string{"home", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "move") {
		t.Errorf("mw home --help does not list move:\n%s", out)
	}
}

func TestHomeMovePlannedDryRunPrintsSevenStepsAndRunsNothing(t *testing.T) {
	calls := moveHost(t, 0)

	out, err := runHomeMove(t, "laptop", "--planned", "--dry-run")

	if err != nil {
		t.Fatalf("mw home move --planned --dry-run: %v\n%s", err, out)
	}
	for _, want := range []string{"Step 1 of 7", "Step 7 of 7", "the old home stands down", "Hand off now: the home moves to laptop", "planned move"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
	if got := ranWhat(t, calls); got != "" {
		t.Errorf("a dry run ran programs:\n%s", got)
	}
}

func TestHomeMovePlannedAndOldHomeDeadRefuseTogether(t *testing.T) {
	calls := moveHost(t, 0)

	_, err := runHomeMove(t, "laptop", "--planned", "--old-home-dead")

	if err == nil || !strings.Contains(err.Error(), "--planned") || !strings.Contains(err.Error(), "--old-home-dead") {
		t.Fatalf("expected a refusal naming both flags, got %v", err)
	}
	if got := ranWhat(t, calls); got != "" {
		t.Errorf("ran programs for a contradiction:\n%s", got)
	}
}

func TestHomeMovePlannedRunsTheOldHomesStepsBeforeAnyOfThisHosts(t *testing.T) {
	calls := moveHost(t, 0)

	out, err := runHomeMove(t, "laptop", "--planned")

	// The stand-in ssh answers every command with 0, the old home's count, and the
	// stand-in bd counts nothing, so the move stops at step 3, beads: after the old
	// home stood down, before anything reached the vault.
	if err == nil || !strings.Contains(err.Error(), "step 3") {
		t.Fatalf("expected the move to stop at step 3, got %v\n%s", err, out)
	}
	got := ranWhat(t, calls)
	// The stand-in ssh prints nothing, so neither unit is listed on the old home
	// and there is nothing to stop. What it was asked, in order:
	at := 0
	for _, want := range []string{"desktop true", "mw mail send mayor", ".mayor-acting", "mw sync", "postern-backend.service", "mw postern mirror", "dolt-beads.service"} {
		found := strings.Index(got[at:], want)
		if found < 0 {
			t.Fatalf("expected %q asked of the old home, in order, in:\n%s", want, got)
		}
		at += found + len(want)
	}
	// This host's own bd is a line of its own; the old home's `bd count` is inside an ssh line.
	lastSSH := strings.LastIndex(got, "ssh ")
	if bd := strings.Index("\n"+got, "\nbd "); bd >= 0 && bd < lastSSH {
		t.Errorf("bd ran before the old home stood down:\n%s", got)
	}
}
