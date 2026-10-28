package application_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestReadSessionResultReadsTheFuelAHarnessReports(t *testing.T) {
	result, err := application.ReadSessionResult(`{
		"type": "result", "subtype": "success", "is_error": false,
		"num_turns": 37, "duration_ms": 1680000, "session_id": "s-1",
		"total_cost_usd": 4.21, "model": "claude-opus-5",
		"permission_denials": [{"tool":"Bash"},{"tool":"Write"}],
		"usage": {"input_tokens": 1200, "output_tokens": 18000,
		          "cache_read_input_tokens": 280000, "cache_creation_input_tokens": 12000}
	}`)
	if err != nil {
		t.Fatalf("reading a result: %v", err)
	}

	switch {
	case !result.Finished():
		t.Error("expected a success to count as finished")
	case result.Turns != 37:
		t.Errorf("expected 37 turns, got %d", result.Turns)
	case result.Duration != 28*time.Minute:
		t.Errorf("expected 28 minutes, got %s", result.Duration)
	case result.CostUSD != 4.21:
		t.Errorf("expected $4.21, got %v", result.CostUSD)
	case result.SessionID != "s-1":
		t.Errorf("expected the session id, got %q", result.SessionID)
	case result.Denials != 2:
		t.Errorf("expected 2 permission denials, got %d", result.Denials)
	case result.Fuel.Total() != 311200:
		t.Errorf("expected every token counted, got %d", result.Fuel.Total())
	}

	spent := result.Spent()
	for _, want := range []string{"311,200 tokens", "in 1,200", "out 18,000", "cache read 280,000", "cache write 12,000", "37 turns", "$4.21", "28m"} {
		if !strings.Contains(spent, want) {
			t.Errorf("expected the fuel line to hold %q, got %q", want, spent)
		}
	}
}

func TestReadSessionResultTakesEitherSpellingOfTheCost(t *testing.T) {
	result, err := application.ReadSessionResult(`{"subtype":"success","cost":0.25,"usage":{"input_tokens":1}}`)
	if err != nil {
		t.Fatalf("reading a result: %v", err)
	}
	if result.CostUSD != 0.25 {
		t.Errorf("expected the older cost field to be read, got %v", result.CostUSD)
	}
}

func TestReadSessionResultTellsAFailureFromNothingAtAll(t *testing.T) {
	for name, printed := range map[string]string{
		"nothing":        "",
		"not JSON":       "You've hit your session limit",
		"half a message": `{"subtype":"success"`,
	} {
		if _, err := application.ReadSessionResult(printed); err == nil {
			t.Errorf("expected %s to be refused rather than read as a result", name)
		}
	}
}

func TestASessionThatDidNotFinishSaysWhy(t *testing.T) {
	cases := map[string]struct {
		printed string
		says    string
	}{
		"an error with a reason": {
			`{"subtype":"error_during_execution","is_error":true,"result":"the session ran out of fuel"}`,
			"the session ran out of fuel",
		},
		"an error with only a subtype": {
			`{"subtype":"error_max_turns","is_error":true}`,
			"error_max_turns",
		},
		"a subtype that is not a success": {
			`{"subtype":"error_max_turns"}`,
			"error_max_turns",
		},
	}
	for name, c := range cases {
		result, err := application.ReadSessionResult(c.printed)
		if err != nil {
			t.Fatalf("%s: reading the result: %v", name, err)
		}
		if result.Finished() {
			t.Errorf("%s: expected it not to count as finished", name)
		}
		if !strings.Contains(result.Trouble(), c.says) {
			t.Errorf("%s: expected the trouble to say %q, got %q", name, c.says, result.Trouble())
		}
	}
}

func TestThousandsAndClockAreReadableByAPerson(t *testing.T) {
	for n, want := range map[int]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 311200: "311,200", 1234567: "1,234,567"} {
		if got := application.Thousands(n); got != want {
			t.Errorf("expected %d to read as %q, got %q", n, want, got)
		}
	}
	for d, want := range map[time.Duration]string{
		45 * time.Second: "45s",
		28 * time.Minute: "28m",
		95 * time.Minute: "1h35m",
	} {
		if got := application.Clock(d); got != want {
			t.Errorf("expected %s to read as %q, got %q", d, want, got)
		}
	}
}

// The two files under testdata are what Claude Code actually wrote for two
// real stories on 2026-09-19, transcribed field for field. Everything
// ReadSessionResult reads is verbatim; only the per-request `usage.iterations`
// array and the long prose of `result` were left out, neither of which mw
// reads. result-woken-session.json is mw-gq6.33, a fifteen-minute session that
// a background task notification woke, so the file Claude Code left is the
// third result message of the run. result-one-stretch.json is mw-rk4.1, a run
// that emitted one result message and nothing else.
func realResult(t *testing.T, name string) application.SessionResult {
	t.Helper()
	printed, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	result, err := application.ReadSessionResult(string(printed))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return result
}

func TestReadSessionResultCountsTheWholeSessionAfterAWakeUp(t *testing.T) {
	result := realResult(t, "result-woken-session.json")

	// `usage` in this file holds 65,375 tokens — the one turn that ran after
	// the notification woke the session. `modelUsage` holds the run: it is the
	// figure total_cost_usd was computed from, and $0.74 could never have
	// bought 65,375 tokens.
	switch {
	case result.Fuel.Total() != 2041736:
		t.Errorf("expected the whole run's 2,041,736 tokens, got %d", result.Fuel.Total())
	case result.Fuel.Output != 12827:
		t.Errorf("expected the run's 12,827 output tokens, got %d", result.Fuel.Output)
	case result.Fuel.CacheRead != 1973788:
		t.Errorf("expected the run's 1,973,788 cache-read tokens, got %d", result.Fuel.CacheRead)
	case !result.WholeSessionFuel:
		t.Error("expected the fuel to be known to cover the whole session")
	case !result.Woken:
		t.Error("expected a result that is not the run's first to say so")
	case result.Turns != 1:
		t.Errorf("expected the 1 turn the file reports, got %d", result.Turns)
	case result.Duration != 3625*time.Millisecond:
		t.Errorf("expected the last turn's 3.625s, got %s", result.Duration)
	case result.APIDuration != 151065*time.Millisecond:
		t.Errorf("expected the run's 151s of API time, got %s", result.APIDuration)
	}

	spent := result.Spent()
	for _, want := range []string{"2,041,736 tokens", "cache read 1,973,788", "whole session",
		"1 turn after the last wake-up", "$0.74", "2m of API time"} {
		if !strings.Contains(spent, want) {
			t.Errorf("expected the fuel line to hold %q, got %q", want, spent)
		}
	}
	// 65,375 tokens and 3s were the last turn's, and a ledger that printed
	// either as the session's own figure would be lying.
	for _, wrong := range []string{"65,375", "1 turns", ", 3s"} {
		if strings.Contains(spent, wrong) {
			t.Errorf("expected the fuel line not to hold %q, got %q", wrong, spent)
		}
	}
	// The whole cell, as docs/research/claude-code-result-json-fuel.md quotes it.
	want := "2,041,736 tokens (in 84 · out 12,827 · cache read 1,973,788 · cache write 55,037), " +
		"whole session, 1 turn after the last wake-up, $0.74, 2m of API time"
	if spent != want {
		t.Errorf("expected the fuel cell to read\n  %q\ngot\n  %q", want, spent)
	}
}

func TestReadSessionResultReadsARunThatWasNeverWoken(t *testing.T) {
	result := realResult(t, "result-one-stretch.json")

	// Here `usage` and `modelUsage` agree to the token: one stretch, no
	// subagents, nothing to reconcile.
	switch {
	case result.Fuel.Total() != 314082:
		t.Errorf("expected 314,082 tokens, got %d", result.Fuel.Total())
	case !result.WholeSessionFuel:
		t.Error("expected the fuel to be known to cover the whole session")
	case result.Woken:
		t.Error("expected the run's only result not to count as woken")
	case result.Turns != 12:
		t.Errorf("expected 12 turns, got %d", result.Turns)
	}

	spent := result.Spent()
	for _, want := range []string{"314,082 tokens", "whole session", "12 turns", "$0.22", "4m"} {
		if !strings.Contains(spent, want) {
			t.Errorf("expected the fuel line to hold %q, got %q", want, spent)
		}
	}
	if strings.Contains(spent, "wake-up") || strings.Contains(spent, "API time") {
		t.Errorf("expected no wake-up talk on a run that had one stretch, got %q", spent)
	}
	want := "314,082 tokens (in 18 · out 5,342 · cache read 281,094 · cache write 27,628), " +
		"whole session, 12 turns, $0.22, 4m"
	if spent != want {
		t.Errorf("expected the fuel cell to read\n  %q\ngot\n  %q", want, spent)
	}
}

func TestReadSessionResultSaysSoWhenAllItHasIsTheLastTurn(t *testing.T) {
	result, err := application.ReadSessionResult(`{
		"subtype": "success", "num_turns": 3, "duration_ms": 60000,
		"usage": {"input_tokens": 10, "output_tokens": 20, "cache_read_input_tokens": 23000}
	}`)
	if err != nil {
		t.Fatalf("reading a result: %v", err)
	}
	if result.WholeSessionFuel {
		t.Error("expected a result with no modelUsage not to claim the whole session")
	}
	if result.Fuel.Total() != 23030 {
		t.Errorf("expected the usage block's 23,030 tokens, got %d", result.Fuel.Total())
	}
	if spent := result.Spent(); !strings.Contains(spent, "last turn only") {
		t.Errorf("expected the fuel line to say what it is, got %q", spent)
	}
}

func TestReadSessionResultAddsUpEveryModelAModelUsageNames(t *testing.T) {
	result, err := application.ReadSessionResult(`{
		"subtype": "success", "num_turns": 2, "total_cost_usd": 1.5,
		"usage": {"input_tokens": 1, "output_tokens": 2},
		"modelUsage": {
			"claude-opus-5": {"inputTokens": 10, "outputTokens": 20,
			                  "cacheReadInputTokens": 30, "cacheCreationInputTokens": 40},
			"claude-haiku-4-5": {"inputTokens": 1, "outputTokens": 2,
			                     "cacheReadInputTokens": 3, "cacheCreationInputTokens": 4}
		}
	}`)
	if err != nil {
		t.Fatalf("reading a result: %v", err)
	}
	// A subagent runs on its own model, and its tokens are in modelUsage and
	// nowhere in usage.
	if result.Fuel.Total() != 110 {
		t.Errorf("expected both models' 110 tokens, got %d", result.Fuel.Total())
	}
	if result.Fuel.Input != 11 || result.Fuel.Output != 22 || result.Fuel.CacheRead != 33 || result.Fuel.CacheWrite != 44 {
		t.Errorf("expected each kind of token summed across models, got %+v", result.Fuel)
	}
}

// The 14 Builders stopped at the weekly limit on 2026-10-15 left a result
// with the harness's notice and no work (mw-gq6.433).
func TestAResultStoppedAtTheUsageLimitSaysSo(t *testing.T) {
	cases := map[string]struct {
		result application.SessionResult
		want   string
	}{
		"weekly":       {application.SessionResult{IsError: true, Said: "You've hit your weekly limit · resets Oct 16, 7am (UTC)"}, "You've hit your weekly limit · resets Oct 16, 7am (UTC)"},
		"session":      {application.SessionResult{IsError: true, Said: "Claude AI usage limit reached\nYou’ve hit your session limit · resets 3pm"}, "You’ve hit your session limit · resets 3pm"},
		"another fail": {application.SessionResult{IsError: true, Said: "API Error: Connection lost mid-response"}, ""},
		"finished":     {application.SessionResult{Subtype: "success", Said: "Done. You've hit your weekly limit was the story's title."}, ""},
	}
	for name, c := range cases {
		if got := c.result.UsageLimit(); got != c.want {
			t.Errorf("%s: UsageLimit() = %q, want %q", name, got, c.want)
		}
	}
}

// The model a session ran on is the id modelUsage names, not the alias it was
// started with: an alias moves, the id does not (mw-2zh33g.11). Where
// modelUsage names more than one, it is the one that wrote the most output,
// even when another took in more tokens all told.
func TestReadSessionResultNamesTheModelIDThatWroteTheMostOutput(t *testing.T) {
	if got := realResult(t, "result-haiku-grind.json").ModelID; got != "claude-haiku-5-5" {
		t.Errorf("expected the grind's model id claude-haiku-5-5, got %q", got)
	}
	if got := realResult(t, "result-one-stretch.json").ModelID; got != "claude-sonnet-5" {
		t.Errorf("expected the one model the run names, got %q", got)
	}
	none, err := application.ReadSessionResult(`{"subtype": "success", "usage": {"output_tokens": 20}}`)
	if err != nil {
		t.Fatal(err)
	}
	if none.ModelID != "" {
		t.Errorf("expected no model id from a result with no modelUsage, got %q", none.ModelID)
	}
}

const codexClean = `{"type":"thread.started","thread_id":"th-123"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"i0","type":"command_execution","command":"ls","exit_code":0}}
{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"first thought"}}
{"type":"item.completed","item":{"id":"i2","type":"file_change","changes":[]}}
{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":60,"output_tokens":10,"reasoning_output_tokens":4}}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"i3","type":"agent_message","text":"all done"}}
{"type":"turn.completed","usage":{"input_tokens":200,"cached_input_tokens":150,"output_tokens":20,"reasoning_output_tokens":8}}
`

func TestReadSessionResultFoldsACodexStreamIntoASessionResult(t *testing.T) {
	result, err := application.ReadSessionResultWith(codexClean, application.ReadOptions{Model: "gpt-5-codex"})
	if err != nil {
		t.Fatalf("reading a Codex stream: %v", err)
	}
	if result.SessionID != "th-123" {
		t.Errorf("SessionID = %q, want the thread id", result.SessionID)
	}
	if result.Said != "all done" {
		t.Errorf("Said = %q, want the last agent message", result.Said)
	}
	if result.IsError || !result.Finished() {
		t.Errorf("a stream that completed its turns is finished, got %+v", result)
	}
	want := application.Fuel{Input: 300, Output: 30, CacheRead: 210}
	if result.Fuel != want {
		t.Errorf("Fuel = %+v, want %+v", result.Fuel, want)
	}
	if !result.WholeSessionFuel {
		t.Errorf("the turns' sum is the whole session's fuel")
	}
	if result.Turns != 4 {
		t.Errorf("Turns = %d, want 4 item.completed", result.Turns)
	}
	if result.Model != "gpt-5-codex" {
		t.Errorf("Model = %q, want the -m model", result.Model)
	}
	if result.CostUSD != 0 {
		t.Errorf("CostUSD = %v, want 0", result.CostUSD)
	}
	if application.CappedAtTurns(result) {
		t.Errorf("a finished stream is not capped")
	}
}

func TestReadSessionResultReadsACodexStreamWithoutOptions(t *testing.T) {
	result, err := application.ReadSessionResult(codexClean)
	if err != nil || result.SessionID != "th-123" || result.Turns != 4 {
		t.Fatalf("got %+v, %v", result, err)
	}
}

func TestReadSessionResultMarksACodexStreamThatFailed(t *testing.T) {
	cases := map[string]struct {
		printed string
		said    string
	}{
		"turn.failed": {
			`{"type":"thread.started","thread_id":"t1"}
{"type":"item.completed","item":{"type":"agent_message","text":"working"}}
{"type":"turn.failed","error":{"message":"context window exceeded"}}
`, "context window exceeded"},
		"error event": {
			`{"type":"thread.started","thread_id":"t1"}
{"type":"error","message":"stream disconnected"}
`, "stream disconnected"},
		"no turn.completed": {
			`{"type":"thread.started","thread_id":"t1"}
{"type":"item.completed","item":{"type":"agent_message","text":"half way"}}
`, "half way"},
		"error after a completed turn": {
			`{"type":"thread.started","thread_id":"t1"}
{"type":"turn.completed","usage":{"input_tokens":5,"output_tokens":1}}
{"type":"error","message":"late failure"}
`, "late failure"},
	}
	for name, c := range cases {
		result, err := application.ReadSessionResult(c.printed)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !result.IsError || result.Finished() {
			t.Errorf("%s: expected an error that is not finished, got %+v", name, result)
		}
		if result.SessionID != "t1" {
			t.Errorf("%s: SessionID = %q", name, result.SessionID)
		}
		if !strings.Contains(result.Said, c.said) {
			t.Errorf("%s: Said = %q, want it to contain %q", name, result.Said, c.said)
		}
		if application.CappedAtTurns(result) {
			t.Errorf("%s: a failure is not a turn cap", name)
		}
	}
}

func TestReadSessionResultSkipsLinesAStreamPrintedThatAreNotEvents(t *testing.T) {
	result, err := application.ReadSessionResult("Reading prompt from stdin...\n" + codexClean + "not json\n")
	if err != nil || result.SessionID != "th-123" || result.IsError {
		t.Fatalf("got %+v, %v", result, err)
	}
}

func TestAnEmptyResultAfterExit124IsACodexTimeout(t *testing.T) {
	result, err := application.ReadSessionResultWith("", application.ReadOptions{ExitCode: 124, Model: "gpt-5-codex"})
	if err != nil {
		t.Fatalf("an empty file after a timeout is a result: %v", err)
	}
	if result.Subtype != "codex_timeout" {
		t.Errorf("Subtype = %q", result.Subtype)
	}
	if !result.IsError || result.Finished() {
		t.Errorf("a timeout is not finished: %+v", result)
	}
	if !application.CappedAtTurns(result) {
		t.Errorf("a timeout counts as the turn cap")
	}
	if result.Model != "gpt-5-codex" {
		t.Errorf("Model = %q", result.Model)
	}

	for _, code := range []int{0, 1} {
		if _, err := application.ReadSessionResultWith("", application.ReadOptions{ExitCode: code}); err == nil {
			t.Errorf("an empty file after exit %d is still refused", code)
		}
	}
	if _, err := application.ReadSessionResult(""); err == nil {
		t.Errorf("an empty file with no exit code is still refused")
	}
}

func TestACodexTimeoutWithPartialStreamIsCapped(t *testing.T) {
	partial := `{"type":"thread.started","thread_id":"t9"}
{"type":"item.completed","item":{"type":"agent_message","text":"still going"}}
`
	result, err := application.ReadSessionResultWith(partial, application.ReadOptions{ExitCode: 124})
	if err != nil {
		t.Fatal(err)
	}
	if result.Subtype != application.CodexTimeoutSubtype || !application.CappedAtTurns(result) || result.SessionID != "t9" {
		t.Errorf("got %+v", result)
	}
}
