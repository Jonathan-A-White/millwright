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
