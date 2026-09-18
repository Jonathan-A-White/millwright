package claude

// The unit tests here cover the parts that are ours rather than Claude Code's:
// which flags a session is started with, what its environment says it is, and
// the quoting of the line a shell reads. The quoting is checked by running the
// line through a real /bin/sh with a harmless command in Claude Code's place —
// no session is ever started here, and nothing costs fuel.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// launch is a complete launch, with whatever a test wants to change applied.
func launch(change func(*application.Launch)) application.Launch {
	l := application.Launch{
		StoryID: "mw-gq6.6",
		Path: domain.Path{
			Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
			Model: domain.ModelOpus, Effort: domain.EffortHigh,
			Formula: "tdd-feature", Host: "vps",
		},
		Seat:       "builder",
		Host:       "vps",
		Dir:        "/root/.mw-worktrees/mw-gq6.6",
		BootFile:   "/root/millwright-vault/runs/mw-gq6.6/boot.md",
		ResultFile: "/root/millwright-vault/runs/mw-gq6.6/result.json",
		Kickoff:    "You are booted into the builder seat.",
	}
	if change != nil {
		change(&l)
	}
	return l
}

func TestSessionIsAShellLineThatKeepsTheResult(t *testing.T) {
	spec, err := New().Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}

	if len(spec.Command) != 3 || spec.Command[0] != Shell || spec.Command[1] != "-c" {
		t.Fatalf("expected %s -c <line>, got %q", Shell, spec.Command)
	}
	line := spec.Command[2]
	for _, want := range []string{
		"claude --print",
		"--output-format json",
		"--model opus",
		"--effort high",
		"--permission-mode auto",
		"--permission-prompts none",
		"--append-system-prompt-file /root/millwright-vault/runs/mw-gq6.6/boot.md",
		"--name mw-gq6.6",
		"'You are booted into the builder seat.'",
		"> /root/millwright-vault/runs/mw-gq6.6/result.json",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("expected the line to carry %q, got %q", want, line)
		}
	}
	if spec.Name != "mw-gq6_6" {
		t.Errorf("expected the session to be named mw-gq6_6, got %q", spec.Name)
	}
	if spec.Dir != "/root/.mw-worktrees/mw-gq6.6" {
		t.Errorf("expected the session to run in the worktree, got %q", spec.Dir)
	}
}

func TestSessionIsTheSeatOnThisHost(t *testing.T) {
	spec, err := New().Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	for name, want := range map[string]string{
		"BEADS_ACTOR": "builder@vps",
		"MW_SEAT":     "builder@vps",
		"MW_STORY":    "mw-gq6.6",
	} {
		if got := spec.Env[name]; got != want {
			t.Errorf("expected %s to be %q, got %q", name, want, got)
		}
	}
}

func TestSessionTakesThePermissionModeItIsGiven(t *testing.T) {
	spec, err := New(WithPermissionMode(PermissionDontAsk)).Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	if !strings.Contains(spec.Command[2], "--permission-mode dontAsk") {
		t.Errorf("expected the given permission mode, got %q", spec.Command[2])
	}

	if _, err := New(WithPermissionMode("whatever")).Session(launch(nil)); err == nil {
		t.Error("expected a permission mode Claude Code does not take to be refused")
	}
}

func TestSessionRefusesAnIncompleteOrForeignLaunch(t *testing.T) {
	cases := map[string]func(*application.Launch){
		"no story":       func(l *application.Launch) { l.StoryID = "" },
		"no seat":        func(l *application.Launch) { l.Seat = "" },
		"no boot file":   func(l *application.Launch) { l.BootFile = "" },
		"no result file": func(l *application.Launch) { l.ResultFile = "" },
		"no kickoff":     func(l *application.Launch) { l.Kickoff = "" },
		"no model":       func(l *application.Launch) { l.Path.Model = "" },
		"no effort":      func(l *application.Launch) { l.Path.Effort = "" },
		"another harness": func(l *application.Launch) {
			l.Path.Harness = domain.Harness("herdr")
		},
	}
	for name, change := range cases {
		if _, err := New().Session(launch(change)); err == nil {
			t.Errorf("expected a launch with %s to be refused", name)
		}
	}
}

// TestTheShellReadsBackTheArgumentsItWasGiven runs an assembled line through a
// real shell, with a command that only prints its arguments, and checks that
// every argument arrived whole. Story titles and paths carry quotes, colons,
// dollar signs and newlines, and a line a shell re-splits would hand Claude
// Code something other than what the factory meant.
func TestTheShellReadsBackTheArgumentsItWasGiven(t *testing.T) {
	dir := filepath.Join(t.TempDir(), `a dir with a 'quote' and $HOME`)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	result := filepath.Join(dir, "result.json")

	kickoff := "Your story is mw-gq6.6: it's \"quoted\", it holds $dollars, `backticks`,\na newline and a ; semicolon"
	spec, err := New(WithProgram("printf")).Session(launch(func(l *application.Launch) {
		l.BootFile = filepath.Join(dir, "boot.md")
		l.ResultFile = result
		l.Kickoff = kickoff
	}))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}

	// printf repeats its format for every remaining argument, so each argument
	// of the line comes back on a line of its own.
	line := strings.Replace(spec.Command[2], "printf --print", `printf '[%s]\n' --print`, 1)
	if err := exec.Command(spec.Command[0], "-c", line).Run(); err != nil {
		t.Fatalf("running the assembled line: %v", err)
	}

	printed, err := os.ReadFile(result)
	if err != nil {
		t.Fatalf("the line did not write its output where it was told: %v", err)
	}
	for _, want := range []string{
		"[--model]\n[opus]\n",
		"[--append-system-prompt-file]\n[" + filepath.Join(dir, "boot.md") + "]\n",
		"[" + kickoff + "]\n",
	} {
		if !strings.Contains(string(printed), want) {
			t.Errorf("expected the shell to pass %q, got:\n%s", want, printed)
		}
	}
}

func TestShellQuoteLeavesPlainWordsAloneAndWrapsTheRest(t *testing.T) {
	plain := []string{"claude", "--model", "opus", "/root/millwright-vault/runs/mw-gq6.6/boot.md", "a,b=c+d%e@f:g"}
	for _, word := range plain {
		if got := shellQuote(word); got != word {
			t.Errorf("expected %q to be left alone, got %q", word, got)
		}
	}

	wrapped := map[string]string{
		"":                  "''",
		"two words":         "'two words'",
		"it's":              `'it'\''s'`,
		"$HOME":             "'$HOME'",
		"a\nb":              "'a\nb'",
		"semi;colon":        "'semi;colon'",
		`back\slash`:        `'back\slash'`,
		`"quoted"`:          `'"quoted"'`,
		"~/millwright":      "'~/millwright'",
		"*":                 "'*'",
		"`backticks`":       "'`backticks`'",
		"ends with a quote": "'ends with a quote'",
	}
	for word, want := range wrapped {
		if got := shellQuote(word); got != want {
			t.Errorf("quoting %q: expected %q, got %q", word, want, got)
		}
	}
}

func TestShellLineJoinsTheWordsItQuoted(t *testing.T) {
	got := shellLine([]string{"claude", "--name", "mw-gq6.6", "it's a story"})
	want := `claude --name mw-gq6.6 'it'\''s a story'`
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestTheCloseOutIsChainedOnHoweverTheSessionEnds(t *testing.T) {
	spec, err := New().Session(launch(func(l *application.Launch) {
		l.After = []string{"/root/millwright/bin/mw", "next", "mw-gq6.6"}
	}))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}

	line := spec.Command[2]
	want := "> /root/millwright-vault/runs/mw-gq6.6/result.json; /root/millwright/bin/mw next mw-gq6.6"
	if !strings.Contains(line, want) {
		t.Errorf("expected the close-out to be chained after the redirection, got %q", line)
	}
	// `&&` would skip the close-out for exactly the sessions that need one: the
	// ones that failed, ran out of fuel or died.
	if strings.Contains(line, "&&") {
		t.Errorf("expected the close-out to run whatever the harness exited with, got %q", line)
	}
}

func TestASessionWithNothingAfterItEndsAtTheRedirection(t *testing.T) {
	spec, err := New().Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	if strings.Contains(spec.Command[2], ";") {
		t.Errorf("expected nothing chained after a session with no After, got %q", spec.Command[2])
	}
}
