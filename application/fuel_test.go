package application_test

import (
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
