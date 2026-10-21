package application

import (
	"encoding/json"
	"testing"
	"time"
)

// The cache's life and the talk's quiet are one number: the env value is
// GristTalkQuiet in the harness's spelling.
func TestGristTalkCacheTTLIsTheQuietHour(t *testing.T) {
	if GristTalkQuiet != time.Hour || GristTalkCacheTTL != "1h" {
		t.Fatalf("GristTalkQuiet %s gives CLAUDE_CODE_PROMPT_CACHE_TTL %q", GristTalkQuiet, GristTalkCacheTTL)
	}
}

// Over two turns of one session, the record counts the turns and keeps the
// session's cumulative cost, and the second turn's own cost is the difference;
// the line written for it names the talk, the session and that it resumed.
func TestGristTalkTurnsCostTheDifferenceOfTheSessionsTotal(t *testing.T) {
	key := GristTalkKey{App: "lampas", Sender: "abcd ef01 2345 6789", Talk: "rom.8@1"}
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	first := SessionResult{SessionID: "s-1", CostUSD: 0.10, Usage: Fuel{Input: 10, Output: 40, CacheRead: 0, CacheWrite: 4000}}
	one, cost := gristTalkRecord(GristTalk{}, false, key, "s-1", "/cwd", "spec", BillingSubscription, false, first, start)
	if one.Turns != 1 || cost != 0.10 || one.CostTotal != 0.10 || one.ContextTokens != 4010 || !one.Started.Equal(start) {
		t.Fatalf("the first turn left %+v, costing %v", one, cost)
	}
	second := SessionResult{SessionID: "s-1", CostUSD: 0.15, Usage: Fuel{Input: 12, Output: 30, CacheRead: 4000, CacheWrite: 300}}
	two, cost := gristTalkRecord(one, true, key, "s-1", "/cwd", "spec", BillingSubscription, true, second, start.Add(time.Minute))
	if two.Turns != 2 || two.CostTotal != 0.15 || two.ContextTokens != 4312 || two.CacheRead != 4000 || !two.Started.Equal(start) {
		t.Fatalf("the second turn left %+v", two)
	}
	if cost < 0.0499 || cost > 0.0501 {
		t.Fatalf("the second turn cost %v, not 0.05", cost)
	}

	w := &gristWork{
		plain:  GristPlaintext{Grist: GristName{App: "lampas", Kind: "talk", V: "1", Talk: key.Talk}},
		result: second, status: GristAnswered,
		talk: &gristTalkTurn{session: "s-1", resumed: true, record: two, cost: cost, sessionCost: second.CostUSD},
	}
	line := GrindLine{CostUSD: second.CostUSD}
	w.talk.mark(&line, w.plain.Grist.Talk, second.SessionID)
	raw, _ := json.Marshal(line)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["talk"] != "rom.8@1" || got["session_id"] != "s-1" || got["resumed"] != true ||
		got["total_cost_usd"].(float64) < 0.0499 || got["total_cost_usd"].(float64) > 0.0501 || got["session_cost_usd"] != 0.15 {
		t.Fatalf("the second turn's line is %s", raw)
	}
	if said := w.talk.answer(); *said != (GristAnswerTalk{Session: GristTalkResumed, Turns: 2, ContextTokens: 4312}) {
		t.Fatalf("the second turn's answer says %+v", *said)
	}
}

// A session started afresh ends the one the talk had.
func TestGristTalkFreshEndsTheSessionBefore(t *testing.T) {
	prev := GristTalk{Session: "s-1", Turns: 7, CostTotal: 0.9, Ended: []string{"s-0"}}
	fresh, cost := gristTalkRecord(prev, true, GristTalkKey{}, "s-2", "/cwd", "spec", BillingSubscription, false, SessionResult{CostUSD: 0.2}, time.Now())
	if fresh.Turns != 1 || cost != 0.2 || len(fresh.Ended) != 2 || fresh.Ended[1] != "s-1" || prev.Ended[0] != "s-0" || len(prev.Ended) != 1 {
		t.Fatalf("the fresh session left %+v (cost %v), the old %+v", fresh, cost, prev)
	}
}

// The history is left out of a resumed turn's request, its other keys in
// their own order; a request that is no object is kept.
func TestWithoutHistoryKeepsTheRestInOrder(t *testing.T) {
	for in, want := range map[string]string{
		`{"z":1,"history":[{"a":"b"}],"q":"what of my son?","summary":"x"}`: `{"z":1,"q":"what of my son?","summary":"x"}`,
		`{"q":"no history"}`: `{"q":"no history"}`,
		`[1,2]`:              `[1,2]`,
		`{}`:                 `{}`,
	} {
		if got := string(withoutHistory(json.RawMessage(in))); got != want {
			t.Errorf("withoutHistory(%s) = %s, want %s", in, got, want)
		}
	}
}
