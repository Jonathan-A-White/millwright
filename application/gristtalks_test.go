package application

import (
	"encoding/json"
	"errors"
	"strings"
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
	first := SessionResult{SessionID: "s-1", CostUSD: 0.10, Usage: Fuel{Input: 10, Output: 40, CacheRead: 0, CacheWrite: 4000},
		Fuel: Fuel{Input: 10, Output: 40, CacheRead: 0, CacheWrite: 4000}}
	one, cost, _ := gristTalkRecord(GristTalk{}, false, key, "s-1", "/cwd", "spec", BillingSubscription, false, first, start)
	if one.Turns != 1 || cost != 0.10 || one.CostTotal != 0.10 || one.ContextTokens != 4010 || !one.Started.Equal(start) {
		t.Fatalf("the first turn left %+v, costing %v", one, cost)
	}
	second := SessionResult{SessionID: "s-1", CostUSD: 0.15, Usage: Fuel{Input: 12, Output: 30, CacheRead: 4000, CacheWrite: 300},
		Fuel: Fuel{Input: 22, Output: 70, CacheRead: 4000, CacheWrite: 4300}}
	two, cost, fuel := gristTalkRecord(one, true, key, "s-1", "/cwd", "spec", BillingSubscription, true, second, start.Add(time.Minute))
	if two.Turns != 2 || two.CostTotal != 0.15 || two.ContextTokens != 4312 || two.CacheRead != 4000 || !two.Started.Equal(start) {
		t.Fatalf("the second turn left %+v", two)
	}
	if cost < 0.0499 || cost > 0.0501 {
		t.Fatalf("the second turn cost %v, not 0.05", cost)
	}
	if fuel != (Fuel{Input: 12, Output: 30, CacheRead: 4000, CacheWrite: 300}) {
		t.Fatalf("the second turn's own fuel is %+v", fuel)
	}

	w := &gristWork{
		plain:  GristPlaintext{Grist: GristName{App: "lampas", Kind: "talk", V: "1", Talk: key.Talk}},
		result: second, status: GristAnswered,
		talk: &gristTalkTurn{session: "s-1", resumed: true, record: two, cost: cost, sessionCost: second.CostUSD, fuel: fuel},
	}
	line := GrindLine{CostUSD: second.CostUSD}
	w.talk.mark(&line, w.plain.Grist.Talk, second.SessionID)
	raw, _ := json.Marshal(line)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["talk"] != "rom.8@1" || got["session_id"] != "s-1" || got["resumed"] != true ||
		got["total_cost_usd"].(float64) < 0.0499 || got["total_cost_usd"].(float64) > 0.0501 || got["session_cost_usd"] != 0.15 ||
		line.Tokens != 4342 || line.Fuel == nil || line.Fuel.CacheRead != 4000 {
		t.Fatalf("the second turn's line is %s", raw)
	}
	if said := w.talk.answer(); *said != (GristAnswerTalk{Session: GristTalkResumed, Turns: 2, ContextTokens: 4312}) {
		t.Fatalf("the second turn's answer says %+v", *said)
	}
}

// A session started afresh ends the one the talk had.
func TestGristTalkFreshEndsTheSessionBefore(t *testing.T) {
	prev := GristTalk{Session: "s-1", Turns: 7, CostTotal: 0.9, Ended: []string{"s-0"}}
	fresh, cost, _ := gristTalkRecord(prev, true, GristTalkKey{}, "s-2", "/cwd", "spec", BillingSubscription, false, SessionResult{CostUSD: 0.2}, time.Now())
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

// Key points are the session's answer trimmed and held to GristKeyPointsMax
// characters (not bytes); an error, an unfinished session, a bad shape or
// empty points are none, with the reason.
func TestKeyPointsOf(t *testing.T) {
	long := strings.Repeat("é", GristKeyPointsMax+10)
	got, why := keyPointsOf(SessionResult{Answer: json.RawMessage(`{"key_points":"  Luke is 14.\n"}`)}, nil)
	if got != "Luke is 14." || why != "" {
		t.Errorf("trimmed key points are %q (%s)", got, why)
	}
	got, _ = keyPointsOf(SessionResult{Answer: json.RawMessage(`{"key_points":"` + long + `"}`)}, nil)
	if len([]rune(got)) != GristKeyPointsMax {
		t.Errorf("long key points are %d characters", len([]rune(got)))
	}
	for name, c := range map[string]struct {
		result SessionResult
		err    error
	}{
		"error":      {err: errors.New("exit status 1")},
		"unfinished": {result: SessionResult{IsError: true, Said: "no"}},
		"shape":      {result: SessionResult{Answer: json.RawMessage(`[1]`)}},
		"empty":      {result: SessionResult{Answer: json.RawMessage(`{"key_points":" "}`)}},
		"missing":    {},
	} {
		if got, why := keyPointsOf(c.result, c.err); got != "" || why == "" {
			t.Errorf("%s: key points %q, why %q", name, got, why)
		}
	}
}

// A session ended with key points leaves no session and names it ended, with
// the time; a session started after it ends nothing again.
func TestGristTalkEndKeepsTheSessionForTidy(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	talk := GristTalk{Session: "s-1", Turns: 3, Ended: []string{"s-0"}}
	talk.end(now, "Luke is 14.")
	if talk.Session != "" || talk.KeyPoints != "Luke is 14." || len(talk.Ended) != 2 || !talk.EndedAt["s-1"].Equal(now) {
		t.Fatalf("the ended talk is %+v", talk)
	}
	next, _, _ := gristTalkRecord(talk, true, GristTalkKey{}, "s-2", "/cwd", "spec", BillingSubscription, false, SessionResult{}, now)
	if next.Session != "s-2" || next.KeyPoints != "" || len(next.Ended) != 2 || !next.EndedAt["s-1"].Equal(now) {
		t.Fatalf("the talk after it is %+v", next)
	}
}
