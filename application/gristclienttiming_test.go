package application

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAClientTimingIsReadInMillisecondsAndLeavesOutWhatTheAppDidNotSend(t *testing.T) {
	got, err := parseGristClientTiming(json.RawMessage(`{"txid":"direct:a","tap_to_sent":900,"network":0,"seen_to_shown":null,"extra":"x"}`), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got.Txid != "direct:a" || got.TapToSent == nil || *got.TapToSent != 900 || got.Network == nil || *got.Network != 0 || got.SeenToShown != nil || got.PollNetwork != nil {
		t.Fatalf("unexpected %+v", got)
	}
}

func TestAClientTimingTheMillCannotTrustIsAnError(t *testing.T) {
	for name, raw := range map[string]string{
		"not an object": `[1]`,
		"no txid":       `{"tap_to_sent":1}`,
		"empty txid":    `{"txid":"","tap_to_sent":1}`,
		"no timing":     `{"txid":"direct:a"}`,
		"negative":      `{"txid":"direct:a","tap_to_sent":-1}`,
		"ten minutes+":  `{"txid":"direct:a","seen_to_shown":600001}`,
		"a string":      `{"txid":"direct:a","network":"3"}`,
		"infinite":      `{"txid":"direct:a","network":1e999}`,
	} {
		if _, err := parseGristClientTiming(json.RawMessage(raw), time.Unix(0, 0)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := parseGristClientTiming(json.RawMessage(`{"txid":"direct:a","tap_to_sent":600000}`), time.Unix(0, 0)); err != nil {
		t.Errorf("ten minutes exactly is allowed: %v", err)
	}
}

func TestTheRoundTripNeedsTheMillsTotalAndAllThreePhonePhases(t *testing.T) {
	total := 3.6
	line := GrindLine{TotalSeconds: &total}
	tap, seen, shown, poll := 900.0, 3000.0, 300.0, 100.0
	full := GristClientTiming{TapToSent: &tap, DeliveredToSeen: &seen, SeenToShown: &shown, PollNetwork: &poll}
	if got := full.phaseSeconds("round_trip", line); got == nil || *got < 7.79 || *got > 7.81 {
		t.Errorf("expected 7.8, got %v", got)
	}
	if got := full.phaseSeconds("wait", line); got == nil || *got < 2.89 || *got > 2.91 {
		t.Errorf("expected 2.9, got %v", got)
	}
	if got := full.phaseSeconds("round_trip", GrindLine{}); got != nil {
		t.Errorf("no total, so no round trip, got %v", *got)
	}
	partial := GristClientTiming{TapToSent: &tap, DeliveredToSeen: &seen}
	if got := partial.phaseSeconds("round_trip", line); got != nil {
		t.Errorf("not shown, so no round trip, got %v", *got)
	}
	if got := partial.phaseSeconds("wait", line); got != nil {
		t.Errorf("no poll_network, so no wait, got %v", *got)
	}
}
