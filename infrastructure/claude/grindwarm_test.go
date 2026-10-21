package claude_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// aWarmCall is aGrindCall as the pool starts it: everything but the prompt.
func aWarmCall(t *testing.T, dir string) application.GrindCall {
	call := aGrindCall(t, dir)
	call.Prompt = ""
	call.MaxTurns = 6
	return call
}

// A warm session is started with the cold session's flags, every one, the
// same system prompt and schema among them: only how it takes its message and
// prints its result differ, the message coming on its standard input as one
// stream-json line instead of as the last argument.
func TestWarmArgsAreTheColdOnesTakingTheirMessageOnStandardInput(t *testing.T) {
	warmCall := aWarmCall(t, "/tmp/mw-grist-1")
	coldCall := warmCall
	coldCall.Prompt = "Grind this grist."
	warm, err := claude.WarmArgs(warmCall)
	if err != nil {
		t.Fatal(err)
	}
	cold, err := claude.GrindArgs(coldCall)
	if err != nil {
		t.Fatal(err)
	}
	if cold[len(cold)-1] != coldCall.Prompt {
		t.Fatalf("expected the cold prompt last, got %q", cold)
	}
	want := []string{}
	for i := 0; i < len(cold)-1; i++ {
		if cold[i] == "--output-format" {
			want = append(want, "--output-format", "stream-json", "--input-format", "stream-json", "--verbose")
			i++
			continue
		}
		want = append(want, cold[i])
	}
	if strings.Join(warm, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("expected\n%q\ngot\n%q", want, warm)
	}
	schema, _ := claude.GrindSchema(warmCall.Schema)
	for _, pair := range [][2]string{
		{"--tools", claude.GrindTools}, {"--system-prompt-file", claude.SystemPromptPath(warmCall)}, {"--json-schema", schema},
		{"--model", "sonnet"}, {"--effort", "low"}, {"--max-turns", "6"}, {"--permission-prompts", "none"},
	} {
		at := slices.Index(warm, pair[0])
		if at < 0 || at+1 >= len(warm) || warm[at+1] != pair[1] {
			t.Fatalf("expected %s %q in %q", pair[0], pair[1], warm)
		}
	}
	for _, flag := range []string{"--print", "--restricted", "--no-session-persistence", "--strict-mcp-config", "--disable-slash-commands", "--safe-mode"} {
		if !slices.Contains(warm, flag) {
			t.Fatalf("expected %s in %q", flag, warm)
		}
	}
	if slices.Contains(warm, "") {
		t.Fatalf("expected no empty prompt argument, got %q", warm)
	}
}

// streamResult is liveResult on one line, as stream-json prints it.
func streamResult(t *testing.T) string {
	t.Helper()
	var line bytes.Buffer
	if err := json.Compact(&line, []byte(liveResult)); err != nil {
		t.Fatal(err)
	}
	return line.String()
}

// warmStandIn is a stand-in for claude that waits for one line on its
// standard input and writes it to the file message beside itself, waits for
// its input to close, then runs body.
func warmStandIn(t *testing.T, body string) (program, seen string) {
	t.Helper()
	return grindStandIn(t, "IFS= read -r line || exit 0\n"+
		"printf '%s' \"$line\" > \"$(dirname \"$0\")/message\"\n"+
		"cat > /dev/null\n"+body)
}

// A warm session starts in its private directory with the warm flags, prints
// nothing and is not ended while it waits, and answers the one message it is
// given on its standard input with the result its stream ends with.
func TestAWarmSessionWaitsForItsMessageAndAnswersIt(t *testing.T) {
	lines := `{"type":"system","subtype":"init","session_id":"abc"}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"` + strings.Repeat("x", 200000) + `"}]}}` + "\n" +
		streamResult(t) + "\n"
	program, seen := warmStandIn(t, "cat <<'EOF'\n"+lines+"EOF\n")
	dir := t.TempDir()
	call := aWarmCall(t, dir)

	session, err := claude.NewGrinder(claude.WithGrindProgram(program)).Warm(call)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if session.Exited() {
		t.Fatal("expected the warm session to wait for its message")
	}
	if got, err := os.ReadFile(claude.SystemPromptPath(call)); err != nil || string(got) != call.System {
		t.Fatalf("expected the warm session's system prompt in its directory, got %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(seen, "message")); err == nil {
		t.Fatal("expected no message before Answer")
	}
	prompt := "Grind this grist.\n" + filepath.Join(dir, "photo-1.jpg") + "\n"
	result, err := session.Answer(context.Background(), prompt, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Finished() || result.Turns != 4 || result.Fuel.Total() != 16620 || len(result.Answer) == 0 {
		t.Fatalf("expected the stream's result read, got %+v", result)
	}
	if !session.Exited() {
		t.Fatal("expected the session ended once it answered")
	}

	raw, _ := os.ReadFile(filepath.Join(seen, "message"))
	var got struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || got.Type != "user" || got.Message.Role != "user" || got.Message.Content != prompt {
		t.Fatalf("expected one user message carrying the prompt, got %s (%v)", raw, err)
	}
	cwd, _ := os.ReadFile(filepath.Join(seen, "cwd"))
	if got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(cwd))); got != mustEval(t, dir) {
		t.Fatalf("expected the session in %s, it ran in %s", dir, cwd)
	}
	argv, _ := os.ReadFile(filepath.Join(seen, "args"))
	args := strings.Split(strings.TrimSuffix(string(argv), "\x00"), "\x00")
	want, _ := claude.WarmArgs(call)
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("expected the warm arguments\n%q\ngot\n%q", want, args)
	}
}

// A retired session is ended, with everything it started, and was given
// nothing.
func TestARetiredWarmSessionEndsUnused(t *testing.T) {
	program, seen := warmStandIn(t, "sleep 30 &\nwait\n")
	session, err := claude.NewGrinder(claude.WithGrindProgram(program)).Warm(aWarmCall(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	session.Retire()
	if !session.Exited() {
		t.Fatal("expected a retired session ended")
	}
	if waited := time.Since(started); waited > 5*time.Second {
		t.Fatalf("expected the session ended at once, waited %s", waited)
	}
	if _, err := os.Stat(filepath.Join(seen, "message")); err == nil {
		t.Fatal("expected a retired session to have been given nothing")
	}
}

// A session that dies while it waits says so, and answering it is an error
// naming what it said, not a timeout.
func TestAWarmSessionThatDiedCannotAnswer(t *testing.T) {
	program, _ := grindStandIn(t, "echo 'Error: not logged in' >&2\nexit 1\n")
	session, err := claude.NewGrinder(claude.WithGrindProgram(program)).Warm(aWarmCall(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !session.Exited() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !session.Exited() {
		t.Fatal("expected a session that died to say it exited")
	}
	_, err = session.Answer(context.Background(), "Grind this grist.", 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "not logged in") || errors.Is(err, application.ErrGrindTimedOut) {
		t.Fatalf("expected the session's own words, not a timeout, got %v", err)
	}
}

// A warm session that runs past its time once given its message is killed and
// says it ran out of time.
func TestAWarmSessionIsKilledAtItsTimeout(t *testing.T) {
	program, _ := warmStandIn(t, "sleep 30 &\nwait\n")
	session, err := claude.NewGrinder(claude.WithGrindProgram(program)).Warm(aWarmCall(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = session.Answer(context.Background(), "Grind this grist.", 200*time.Millisecond)
	if !errors.Is(err, application.ErrGrindTimedOut) {
		t.Fatalf("expected ErrGrindTimedOut, got %v", err)
	}
	if waited := time.Since(started); waited > 10*time.Second {
		t.Fatalf("expected the session stopped at its timeout, waited %s", waited)
	}
}
