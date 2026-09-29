package claude_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
		"--no-session-persistence", "--disable-slash-commands",
		"--system-prompt", call.System, "--json-schema", schema, call.Prompt,
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
