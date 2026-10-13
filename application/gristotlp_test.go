package application

import (
	"strings"
	"testing"
	"time"
)

func TestOtlpTraceIDIsTheTxidsFirstSixteenBytesOrAHashOfIt(t *testing.T) {
	if got := OtlpTraceID("direct:CACAF9063f4a4e1fa3b8e2b5f0f1b8b1849a"); got != "cacaf9063f4a4e1fa3b8e2b5f0f1b8b1" {
		t.Errorf("a hex txid gave %s", got)
	}
	for _, txid := range []string{"direct:old", "", "direct:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", "direct:00000000000000000000000000000000"} {
		got := OtlpTraceID(txid)
		if len(got) != 32 || strings.Trim(got, "0") == "" || got != OtlpTraceID(txid) {
			t.Errorf("%q gave %q", txid, got)
		}
	}
}

// Without a total the phases run back from the line's time, and the deliver
// phase is left out of an answer that was not delivered.
func TestOtlpRunsPhasesBackFromTheLineWhenThereIsNoTotal(t *testing.T) {
	wake, model := 2.0, 4.0
	at := time.Date(2026, 10, 7, 12, 0, 10, 0, time.UTC)
	phases := otlpPhases(GrindLine{Time: at, Seconds: 5, WakeSeconds: &wake, ModelSeconds: &model}, nil)
	if len(phases) != 2 || phases[0].name != "wake" || phases[1].name != "model" {
		t.Fatalf("phases %+v", phases)
	}
	if !phases[1].end.Equal(at) || !phases[1].start.Equal(at.Add(-4*time.Second)) || !phases[0].end.Equal(phases[1].start) {
		t.Errorf("phases not laid back from the line: %+v", phases)
	}
}
