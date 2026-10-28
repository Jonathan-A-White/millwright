package codex

// The unit tests here cover what is ours rather than Codex's: which flags a
// session is started with, what its environment says it is, and the line a
// shell reads. The assembled line is run through a real /bin/sh with a fake
// `codex` script in Codex's place — no session is ever started here, and
// nothing costs fuel.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

func launch(change func(*application.Launch)) application.Launch {
	l := application.Launch{
		StoryID: "mw-9rp4hu.12",
		Path: domain.Path{
			Rig: "millwright", Branch: "main", Harness: domain.HarnessCodex,
			Model: "gpt-5.1-codex", Effort: domain.EffortHigh,
			Formula: "tdd-feature", Host: "luke",
		},
		Seat:       "builder",
		Host:       "luke",
		Dir:        "/home/luke/.mw-worktrees/mw-9rp4hu.12",
		BootFile:   "/home/luke/vault/runs/mw-9rp4hu.12/boot.md",
		ResultFile: "/home/luke/vault/runs/mw-9rp4hu.12/result.json",
		Kickoff:    "You are booted into the builder seat.",
	}
	if change != nil {
		change(&l)
	}
	return l
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestTheSessionIsTheExecLineTheStoryNames(t *testing.T) {
	spec, err := New(WithTimeLimit(45 * time.Minute)).Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	if len(spec.Command) != 3 || spec.Command[0] != Shell || spec.Command[1] != "-c" {
		t.Fatalf("expected %s -c <line>, got %q", Shell, spec.Command)
	}
	want := "timeout 45m codex exec --json -m gpt-5.1-codex -c model_reasoning_effort=high " +
		"--sandbox workspace-write -c sandbox_workspace_write.network_access=true " +
		"-c shell_environment_policy.inherit=all -a never " +
		"-C /home/luke/.mw-worktrees/mw-9rp4hu.12 -o /home/luke/.mw-worktrees/mw-9rp4hu.12/.mw-last.md " +
		"'You are booted into the builder seat.' < /home/luke/vault/runs/mw-9rp4hu.12/boot.md " +
		"> /home/luke/vault/runs/mw-9rp4hu.12/result.json.tmp; " +
		"mv /home/luke/vault/runs/mw-9rp4hu.12/result.json.tmp /home/luke/vault/runs/mw-9rp4hu.12/result.json"
	if line := spec.Command[2]; line != want {
		t.Errorf("the line is\n%s\nwant\n%s", line, want)
	}
	if spec.Name != "mw-9rp4hu_12" {
		t.Errorf("expected the session to be named mw-9rp4hu_12, got %q", spec.Name)
	}
	if spec.Dir != "/home/luke/.mw-worktrees/mw-9rp4hu.12" {
		t.Errorf("expected the session to run in the worktree, got %q", spec.Dir)
	}
}

func TestTheDefaultTimeLimitAndNiceAreInTheLine(t *testing.T) {
	spec, err := New(WithNice(10)).Session(launch(nil))
	if err != nil {
		t.Fatal(err)
	}
	if line := spec.Command[2]; !strings.HasPrefix(line, "nice -n 10 timeout 90m codex exec --json ") {
		t.Errorf("expected nice, then the default time limit, then codex, got %q", line)
	}
}

func TestATimeLimitIsRoundedUpToWholeMinutes(t *testing.T) {
	spec, err := New(WithTimeLimit(90 * time.Second)).Session(launch(nil))
	if err != nil {
		t.Fatal(err)
	}
	if line := spec.Command[2]; !strings.HasPrefix(line, "timeout 2m codex ") {
		t.Errorf("expected 90s to become 2m, got %q", line)
	}
}

func TestSessionRefusesAnIncompleteOrForeignLaunch(t *testing.T) {
	if _, err := New().Session(launch(func(l *application.Launch) { l.Kickoff = "" })); err == nil {
		t.Error("expected a launch with no kickoff to be refused")
	}
	_, err := New().Session(launch(func(l *application.Launch) {
		l.Path.Harness = domain.HarnessClaude
		l.Path.Model = domain.ModelOpus
	}))
	if err == nil || !strings.Contains(err.Error(), "codex") {
		t.Errorf("expected a claude launch to be refused naming codex, got %v", err)
	}
	if New().Name() != domain.HarnessCodex {
		t.Errorf("expected the harness to answer to codex")
	}
}

func TestAStorySessionKnowsItsSeatAndStory(t *testing.T) {
	spec, err := New(WithJobs(3)).Session(launch(nil))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["MW_STORY"] != "mw-9rp4hu.12" || spec.Env["MW_SEAT"] != application.SeatIdentity("builder", "luke") ||
		spec.Env["BEADS_ACTOR"] != spec.Env["MW_SEAT"] {
		t.Errorf("unexpected environment %v", spec.Env)
	}
	if spec.Env["JOBS"] != "3" {
		t.Errorf("expected the worker cap in the environment, got %v", spec.Env)
	}
	if _, ok := spec.Env["CLAUDE_CODE_DISABLE_BACKGROUND_TASKS"]; ok {
		t.Errorf("a Codex session is not given a Claude Code variable: %v", spec.Env)
	}
}

// TestTheAssembledLineRunsAgainstAFakeCodex runs the line through /bin/sh with
// a script named codex that records its argv, reads its stdin and prints a
// canned JSONL stream, and reads back what it saw.
func TestTheAssembledLineRunsAgainstAFakeCodex(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	boot := filepath.Join(dir, "boot.md")
	if err := os.WriteFile(boot, []byte("# charter\nthe boot text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := filepath.Join(dir, "result.json")
	if err := os.WriteFile(result, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	argvFile := filepath.Join(dir, "argv")
	stdinFile := filepath.Join(dir, "stdin")
	program := filepath.Join(dir, "codex")
	writeScript(t, program, `for a in "$@"; do printf '%s\n' "$a"; done > `+argvFile+`
cat > `+stdinFile+`
echo "$BEADS_ACTOR" > `+filepath.Join(dir, "actor")+`
printf '%s\n' '{"type":"thread.started","thread_id":"t-1"}'
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":2,"reasoning_output_tokens":1}}'
`)
	after := filepath.Join(dir, "after")
	writeScript(t, after, `cat `+result+` > `+filepath.Join(dir, "after-saw")+"\n")

	kickoff := "You are booted; it's \"quoted\" $HOME\nand two lines."
	spec, err := New(WithProgram(program)).Session(launch(func(l *application.Launch) {
		l.Dir, l.BootFile, l.ResultFile, l.Kickoff = work, boot, result, kickoff
		l.After = []string{after}
	}))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(spec.Command[0], "-c", spec.Command[2])
	cmd.Dir = work
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "BEADS_ACTOR=builder@luke"}
	if said, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running the assembled line: %v\n%s", err, said)
	}

	argv, _ := os.ReadFile(argvFile)
	got := strings.Split(strings.TrimSuffix(string(argv), "\n"), "\n")
	wantHead := []string{"exec", "--json", "-m", "gpt-5.1-codex", "-c", "model_reasoning_effort=high",
		"--sandbox", "workspace-write", "-c", "sandbox_workspace_write.network_access=true",
		"-c", "shell_environment_policy.inherit=all", "-a", "never", "-C", work, "-o", filepath.Join(work, ".mw-last.md")}
	if strings.Join(got[:len(wantHead)], "\n") != strings.Join(wantHead, "\n") {
		t.Errorf("argv was\n%s\nwant it to begin\n%s", strings.Join(got, "\n"), strings.Join(wantHead, "\n"))
	}
	if rest := strings.Join(got[len(wantHead):], "\n"); rest != kickoff {
		t.Errorf("the kickoff reached codex as %q, want %q", rest, kickoff)
	}
	if stdin, _ := os.ReadFile(stdinFile); string(stdin) != "# charter\nthe boot text\n" {
		t.Errorf("codex read %q on stdin, want the boot text", stdin)
	}
	printed, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(printed), `{"type":"thread.started"`) || strings.Contains(string(printed), "stale") {
		t.Errorf("the result file holds %q, want the canned stream", printed)
	}
	if _, err := os.Stat(result + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("the temp result was left behind (%v)", err)
	}
	if saw, _ := os.ReadFile(filepath.Join(dir, "after-saw")); string(saw) != string(printed) {
		t.Errorf("After ran before the result was in place: saw %q", saw)
	}
	res, err := application.ReadSessionResult(string(printed))
	if err != nil {
		t.Fatalf("the stream does not read as a result: %v", err)
	}
	if res.IsError {
		t.Errorf("expected a clean result, got %+v", res)
	}
}

func TestAnExpiredTimeLimitLeavesExit124ForTheCloseOut(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "codex")
	writeScript(t, program, `exec sleep 30`)
	after := filepath.Join(dir, "after")
	status := filepath.Join(dir, "status")
	writeScript(t, after, `echo ran > `+status)

	spec, err := New(WithProgram(program)).Session(launch(func(l *application.Launch) {
		l.Dir, l.BootFile, l.ResultFile = dir, "/dev/null", filepath.Join(dir, "result.json")
		l.After = []string{after}
	}))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Replace(spec.Command[2], "timeout 90m", "timeout 1", 1)
	// The 90m default is swapped for one second so the test need not wait.
	start := time.Now()
	cmd := exec.Command("/bin/sh", "-c", line)
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin"}
	if said, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running the line: %v\n%s", err, said)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("the time limit did not end the session")
	}
	if _, err := os.Stat(status); err != nil {
		t.Errorf("After did not run after the limit ended the session: %v", err)
	}
}

func TestTheSessionsBdIsMwBdAndTheHeartbeatIsKilledBeforeTheCloseOut(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	writeScript(t, filepath.Join(realDir, "bd"), `echo "real bd $*" >> `+out)
	mw := filepath.Join(dir, "mw")
	writeScript(t, mw, `echo "mw $* real=$MW_BD_REAL" >> `+out)
	program := filepath.Join(dir, "codex")
	writeScript(t, program, `bd close step-1 --reason done; command -v bd >> `+out)
	after := filepath.Join(dir, "after")
	writeScript(t, after, `echo "after finds $(command -v bd)" >> `+out)
	beat := filepath.Join(dir, "beat")
	writeScript(t, beat, `echo beat >> `+out+"\nexec sleep 30")

	spec, err := New(WithProgram(program)).Session(launch(func(l *application.Launch) {
		l.Dir, l.BootFile, l.ResultFile = dir, "/dev/null", filepath.Join(dir, "result.json")
		l.After = []string{after}
		l.BdRetry = []string{mw, "bd"}
		l.Heartbeat = []string{beat}
	}))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", spec.Command[2])
	cmd.Env = []string{"PATH=" + realDir + ":/usr/bin:/bin", "TMPDIR=" + dir}
	start := time.Now()
	if said, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running the line: %v\n%s", err, said)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("the heartbeat was not killed when the session ended")
	}
	printed, _ := os.ReadFile(out)
	text := string(printed)
	for _, want := range []string{
		"mw bd close step-1 --reason done real=" + filepath.Join(realDir, "bd"),
		"after finds " + filepath.Join(realDir, "bd"),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "beat") {
		t.Errorf("the heartbeat never ran:\n%s", text)
	}
}

func TestACloseOutAloneRunsOnlyAfter(t *testing.T) {
	spec, err := New().Session(application.Launch{
		StoryID: "mw-9rp4hu.12", Seat: "builder", Host: "luke", Dir: "/w",
		Path: domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessCodex,
			Model: "gpt-5.1-codex", Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "luke"},
		CloseOutOnly: true, After: []string{"mw", "next"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if line := spec.Command[2]; line != "mw next" {
		t.Errorf("expected only the close-out, got %q", line)
	}
}

func TestTheEnvFileAndServerHostAreReadAheadOfTheSession(t *testing.T) {
	spec, err := New(WithEnvFile("/home/luke/.config/mw/beads.env"), WithBeadsServerHost("10.0.0.2")).Session(launch(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := "if [ -r /home/luke/.config/mw/beads.env ]; then set -a; . /home/luke/.config/mw/beads.env; set +a; fi; " +
		"export BEADS_DOLT_SERVER_HOST=10.0.0.2; timeout 90m codex "
	if line := spec.Command[2]; !strings.HasPrefix(line, want) {
		t.Errorf("expected the env file to be read first, got %q", line)
	}
}

func TestVersionIsWhatCodexPrintsForDashDashVersion(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "codex")
	writeScript(t, program, `[ "$1" = "--version" ] && echo "codex-cli 0.9.9"`)
	got, err := New(WithProgram(program)).Version(context.Background())
	if err != nil || got != "codex-cli 0.9.9" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestVersionOfAMissingCodexIsNotInstalled(t *testing.T) {
	_, err := New(WithProgram(filepath.Join(t.TempDir(), "no-codex"))).Version(context.Background())
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("got %v, want ErrNotInstalled", err)
	}
}

func TestVersionOfACodexThatFailsOrIsSilentIsAnError(t *testing.T) {
	dir := t.TempDir()
	failing := filepath.Join(dir, "codex")
	writeScript(t, failing, `echo boom >&2; exit 3`)
	if _, err := New(WithProgram(failing)).Version(context.Background()); err == nil || errors.Is(err, ErrNotInstalled) {
		t.Errorf("got %v, want a plain failure", err)
	}
	silent := filepath.Join(dir, "silent")
	writeScript(t, silent, `exit 0`)
	if _, err := New(WithProgram(silent)).Version(context.Background()); err == nil {
		t.Error("expected an error for a silent program")
	}
}
