package claude_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// sweepSchema is Cairn's Sweep Result 1.1 schema as its rig writes it: a
// $schema and $id at the top, $defs and $refs below.
func sweepSchema(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/sweep-result-1.1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func aGrindCall(t *testing.T, dir string) application.GrindCall {
	return application.GrindCall{
		Dir: dir, Model: "sonnet", Effort: "low",
		System: "The mill's rules.\n\nList what you see.", Schema: sweepSchema(t),
		Prompt:  "Grind this grist.\n" + filepath.Join(dir, "photo-1.jpg") + "\n",
		Timeout: 5 * time.Second,
	}
}

// The command line is the one the coordinator's live run proved, argument
// for argument, with the prompt last and no shell anywhere.
func TestGrindArgsAreExactlyTheVerifiedFlags(t *testing.T) {
	call := aGrindCall(t, "/tmp/mw-grist-1")
	args, err := claude.GrindArgs(call)
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := claude.GrindSchema(call.Schema)
	want := []string{
		"--print", "--output-format", "json", "--model", "sonnet", "--effort", "low",
		"--restricted", "--tools", "Read", "--strict-mcp-config", "--permission-prompts", "none",
		"--no-session-persistence", "--disable-slash-commands", "--safe-mode",
		"--system-prompt-file", claude.SystemPromptPath(call), "--json-schema", schema, call.Prompt,
	}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("expected\n%q\ngot\n%q", want, args)
	}
}

// claude refuses a schema naming its draft by $schema, so the top-level
// $schema and $id are left out; $defs and $ref stay, and it goes compact.
func TestGrindSchemaDropsSchemaAndIDAndKeepsDefs(t *testing.T) {
	got, err := claude.GrindSchema(sweepSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, `"$schema"`) || strings.Contains(got, `"$id"`) {
		t.Fatalf("expected no $schema or $id, got %s", got)
	}
	if !strings.Contains(got, `"$defs":{`) || !strings.Contains(got, `"$ref":"#/$defs/item"`) {
		t.Fatalf("expected $defs and $ref kept, got %s", got)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(got)); err != nil || compact.String() != got {
		t.Fatalf("expected compact JSON, got %s", got)
	}
	var fields map[string]any
	_ = json.Unmarshal([]byte(got), &fields)
	if fields["type"] != "object" || fields["title"] != "Sweep Result 1.1" {
		t.Fatalf("expected the rest of the schema kept, got %v", fields)
	}
	if _, err := claude.GrindSchema("not json"); err == nil {
		t.Fatal("expected a schema that is not JSON refused")
	}
}

// grindStandIn writes a shell script standing in for claude and returns its
// path. It writes its arguments, NUL-separated, its working directory and
// what its standard input is to files beside itself, then runs body.
func grindStandIn(t *testing.T, body string) (program, seen string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the stand-in reads /proc")
	}
	seen = t.TempDir()
	program = filepath.Join(seen, "claude")
	script := "#!/bin/sh\n" +
		"printf '%s\\0' \"$@\" > " + filepath.Join(seen, "args") + "\n" +
		"pwd > " + filepath.Join(seen, "cwd") + "\n" +
		"readlink /proc/self/fd/0 > " + filepath.Join(seen, "stdin") + "\n" +
		body
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return program, seen
}

// The result a grind's session printed in the live run, in its real shape.
const liveResult = `{"type":"result","subtype":"success","is_error":false,"duration_ms":21000,"duration_api_ms":19000,
"num_turns":4,"result":"","stop_reason":"end_turn","session_id":"abc","total_cost_usd":0.027,
"usage":{"input_tokens":8,"output_tokens":512,"cache_read_input_tokens":14000,"cache_creation_input_tokens":2100},
"modelUsage":{"claude-sonnet-5-5":{"inputTokens":8,"outputTokens":512,"cacheReadInputTokens":14000,"cacheCreationInputTokens":2100}},
"permission_denials":[{"tool_name":"Read","tool_use_id":"t1","tool_input":{"file_path":"/tmp/photo-1.jgp"}}],
"structured_output":{"schemaVersion":"1.1","responseType":"sweep-result","placeName":"Top drawer","items":[{"name":"scissors"}]}}`

// A grind runs in its private directory, reads nothing from its standard
// input, and reports the session's result: its answer, its Fuel, and the
// Read it was refused.
func TestGrindRunsInItsDirectoryAndReadsTheResult(t *testing.T) {
	program, seen := grindStandIn(t, "cat <<'EOF'\n"+liveResult+"\nEOF\n")
	dir := t.TempDir()
	call := aGrindCall(t, dir)

	result, err := claude.NewGrinder(claude.WithGrindProgram(program)).Grind(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Answer) != `{"schemaVersion":"1.1","responseType":"sweep-result","placeName":"Top drawer","items":[{"name":"scissors"}]}` {
		t.Fatalf("expected the structured answer, got %s", result.Answer)
	}
	if !result.Finished() || result.Turns != 4 || result.CostUSD != 0.027 || result.Denials != 1 || result.Fuel.Total() != 16620 {
		t.Fatalf("expected the session's own account of itself, got %+v", result)
	}

	cwd, _ := os.ReadFile(filepath.Join(seen, "cwd"))
	if got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(cwd))); got != mustEval(t, dir) {
		t.Fatalf("expected the session in %s, it ran in %s", dir, cwd)
	}
	stdin, _ := os.ReadFile(filepath.Join(seen, "stdin"))
	if strings.TrimSpace(string(stdin)) != os.DevNull {
		t.Fatalf("expected the session's standard input to be %s, got %q", os.DevNull, stdin)
	}
	raw, _ := os.ReadFile(filepath.Join(seen, "args"))
	args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	want, _ := claude.GrindArgs(call)
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("expected the arguments untouched by any shell\n%q\ngot\n%q", want, args)
	}
}

// A session that ends in an error still says why, in its result: the grind
// reports that result, marked an error, rather than failing to read it.
func TestGrindReadsAnErrorResult(t *testing.T) {
	program, _ := grindStandIn(t, `echo '{"type":"result","subtype":"error_max_structured_output_retries","is_error":true,"total_cost_usd":0.01}'`+"\nexit 1\n")
	result, err := claude.NewGrinder(claude.WithGrindProgram(program)).Grind(context.Background(), aGrindCall(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Finished() || result.Subtype != "error_max_structured_output_retries" || result.CostUSD != 0.01 {
		t.Fatalf("expected the error result read, got %+v", result)
	}
}

// A session that prints nothing it can read is an error naming what it said.
func TestGrindThatPrintsNothingReadableIsAnError(t *testing.T) {
	program, _ := grindStandIn(t, "echo 'Error: --json-schema is not a valid JSON Schema' >&2\nexit 1\n")
	_, err := claude.NewGrinder(claude.WithGrindProgram(program)).Grind(context.Background(), aGrindCall(t, t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "not a valid JSON Schema") {
		t.Fatalf("expected the session's own words in the error, got %v", err)
	}
	if errors.Is(err, application.ErrGrindTimedOut) {
		t.Fatalf("a session that failed did not time out: %v", err)
	}
}

// A grind that runs past its time is killed, with everything it started,
// and says it ran out of time.
func TestGrindIsKilledAtItsTimeout(t *testing.T) {
	program, _ := grindStandIn(t, "sleep 30 &\nwait\n")
	call := aGrindCall(t, t.TempDir())
	call.Timeout = 200 * time.Millisecond

	started := time.Now()
	_, err := claude.NewGrinder(claude.WithGrindProgram(program)).Grind(context.Background(), call)
	if !errors.Is(err, application.ErrGrindTimedOut) {
		t.Fatalf("expected ErrGrindTimedOut, got %v", err)
	}
	if waited := time.Since(started); waited > 10*time.Second {
		t.Fatalf("expected the grind stopped at its timeout, waited %s", waited)
	}
	if claude.DefaultGrindTimeout != 10*time.Minute {
		t.Fatalf("expected ten minutes by default, got %s", claude.DefaultGrindTimeout)
	}
}

func mustEval(t *testing.T, path string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A grind file's maxTurns is the one limit the command line carries; with none
// the command line is the verified one, and nothing in it turns subagents off.
func TestGrindArgsCarryMaxTurnsOnlyWhenTheGrindSetsIt(t *testing.T) {
	call := aGrindCall(t, "/tmp/mw-grist-1")
	call.MaxTurns = 6
	args, err := claude.GrindArgs(call)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\x00")
	if !strings.Contains(joined, "--max-turns\x006\x00"+call.Prompt) || args[len(args)-1] != call.Prompt {
		t.Fatalf("expected --max-turns 6 before the prompt, got %q", args)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--disallow") {
			t.Fatalf("expected nothing that forbids a tool, got %q", arg)
		}
	}
}

// A talk's first turn keeps its session under the id it is given, and a later
// turn resumes it: each in the place --no-session-persistence has otherwise,
// every other argument the same.
func TestGrindArgsKeepATalksSession(t *testing.T) {
	call := aGrindCall(t, "/tmp/mw-grist-1")
	cold, err := claude.GrindArgs(call)
	if err != nil {
		t.Fatal(err)
	}
	for flag, set := range map[string]func(*application.GrindCall){
		"--session-id": func(c *application.GrindCall) { c.SessionID = "0b3f6c1e-1111-4222-8333-444455556666" },
		"--resume":     func(c *application.GrindCall) { c.Resume = "0b3f6c1e-1111-4222-8333-444455556666" },
	} {
		talk := call
		set(&talk)
		args, err := claude.GrindArgs(talk)
		if err != nil {
			t.Fatal(err)
		}
		want := make([]string, 0, len(cold)+1)
		for _, a := range cold {
			if a == "--no-session-persistence" {
				want = append(want, flag, "0b3f6c1e-1111-4222-8333-444455556666")
				continue
			}
			want = append(want, a)
		}
		if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("%s: expected\n%q\ngot\n%q", flag, want, args)
		}
	}
}

// A talk's session runs with the one-hour prompt cache, in place of any the
// mill's own environment set; a grist with no talk is given the environment
// as it was.
func TestGrindEnvGivesATalkTheHoursCache(t *testing.T) {
	environ := []string{"HOME=/home/mill", claude.CacheTTLEnv + "=5m"}
	call := aGrindCall(t, "/tmp/mw-grist-1")
	if got := claude.GrindEnv(environ, call); strings.Join(got, "\n") != strings.Join(environ, "\n") {
		t.Fatalf("a grist with no talk was given %q", got)
	}
	call.Resume = "0b3f6c1e-1111-4222-8333-444455556666"
	got := claude.GrindEnv(environ, call)
	want := []string{"HOME=/home/mill", claude.CacheTTLEnv + "=1h"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("a talk's resume was given %q, want %q", got, want)
	}
}

// A system prompt past one argv string's limit (Linux: 131,072 bytes) reaches
// claude as a file in the grind's directory, byte for byte; no argument is
// longer than a path or a flag (mw-gq6.690: Lampas's bible-talk grind passed
// its 131,254 bytes as one string, and claude never started).
func TestGrindPassesTheSystemPromptAsAFileNotAnArgument(t *testing.T) {
	dir := t.TempDir()
	call := aGrindCall(t, dir)
	call.System = strings.Repeat("The mill's rules, over and over.\n", 200000/33+1)[:200000]

	args, err := claude.GrindArgs(call)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range args {
		if len(arg) > 4096 {
			t.Fatalf("expected no argument over 4096 bytes, got one of %d", len(arg))
		}
	}
	at := slices.Index(args, "--system-prompt-file")
	if at < 0 || args[at+1] != claude.SystemPromptPath(call) || slices.Contains(args, "--system-prompt") {
		t.Fatalf("expected --system-prompt-file in the grind's directory, got %q", args)
	}

	copied := filepath.Join(t.TempDir(), "system-prompt-as-claude-saw-it")
	program, _ := grindStandIn(t, "while [ $# -gt 0 ]; do [ \"$1\" = --system-prompt-file ] && cp \"$2\" "+copied+"; shift; done\ncat <<'EOF'\n"+liveResult+"\nEOF\n")
	if _, err := claude.NewGrinder(claude.WithGrindProgram(program)).Grind(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(copied)
	if err != nil || string(got) != call.System {
		t.Fatalf("expected the file to hold the system prompt byte for byte (%d bytes), got %d bytes, err %v", len(call.System), len(got), err)
	}
	info, err := os.Stat(claude.SystemPromptPath(call))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected the file private to its owner, got %v %v", info, err)
	}
}

// A talk's grinds share one directory: instructions that differ are kept in
// files of their own, and none is left half written or a temporary beside them.
func TestSystemPromptsOfDifferentGrindsDoNotShareAFile(t *testing.T) {
	dir := t.TempDir()
	one, two := aGrindCall(t, dir), aGrindCall(t, dir)
	two.System = "Another grind's rules."
	for _, call := range []application.GrindCall{one, two} {
		if err := claude.WriteSystemPrompt(call); err != nil {
			t.Fatal(err)
		}
	}
	if claude.SystemPromptPath(one) == claude.SystemPromptPath(two) {
		t.Fatal("expected a file for each text")
	}
	for _, call := range []application.GrindCall{one, two} {
		if got, _ := os.ReadFile(claude.SystemPromptPath(call)); string(got) != call.System {
			t.Fatalf("expected %q kept, got %q", call.System, got)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("expected the two files and nothing else, got %v", entries)
	}
}

// A prompt too long for one argument (a talk's long history, say) goes on the
// session's standard input, not on the command line; a shorter one stays the
// last argument.
func TestGrindSendsAPromptTooLongForAnArgumentOnStandardInput(t *testing.T) {
	seenPrompt := filepath.Join(t.TempDir(), "prompt")
	program, _ := grindStandIn(t, "cat > "+seenPrompt+"\ncat <<'EOF'\n"+liveResult+"\nEOF\n")
	call := aGrindCall(t, t.TempDir())
	call.Prompt = strings.Repeat("A long history, again and again.\n", 200000/33+1)

	args, err := claude.GrindArgs(call)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range args {
		if len(arg) > 4096 {
			t.Fatalf("expected no argument over 4096 bytes, got one of %d", len(arg))
		}
	}
	if !claude.PromptOnStdin(call) || slices.Contains(args, call.Prompt) {
		t.Fatalf("expected the prompt off the command line, got %q", args)
	}
	if _, err := claude.NewGrinder(claude.WithGrindProgram(program)).Grind(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(seenPrompt); string(got) != call.Prompt {
		t.Fatalf("expected the prompt whole on standard input, got %d bytes of %d", len(got), len(call.Prompt))
	}
	short := aGrindCall(t, t.TempDir())
	if claude.PromptOnStdin(short) {
		t.Fatal("expected a short prompt left on the command line")
	}
}
