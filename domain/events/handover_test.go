package events_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

func TestAHandoverDetailReadsBackAsWritten(t *testing.T) {
	h := events.Handover{Seat: "mayor", Successor: "mayor-2026-10-01-160", At: 105}
	if got := h.Detail(); got != "mayor to mayor-2026-10-01-160 at 105" {
		t.Fatalf("detail %q", got)
	}
	back, ok := events.ParseHandover(h.Detail())
	if !ok || back != h {
		t.Fatalf("read back %+v, %v", back, ok)
	}
	ev := events.Event{Kind: events.KindHandover, Detail: h.Detail()}
	if got, ok := events.HandoverOf(ev); !ok || got != h {
		t.Fatalf("HandoverOf: %+v, %v", got, ok)
	}
}

func TestOnlyAHandoverEventWithAReadableDetailIsAHandover(t *testing.T) {
	for _, detail := range []string{"", "mayor", "mayor to x at", "mayor to x at -1", "mayor to x at two", "a b c d e"} {
		if _, ok := events.ParseHandover(detail); ok {
			t.Errorf("%q read as a handover", detail)
		}
	}
	if _, ok := events.HandoverOf(events.Event{Kind: events.KindJob, Detail: "mayor to x at 3"}); ok {
		t.Error("a job event read as a handover")
	}
}

func TestAHandoverEventIsValidWithNoBeadAndNoMachine(t *testing.T) {
	ev := events.Event{Seq: 1, Kind: events.KindHandover, Actor: "mayor", Detail: "mayor to x at 0", Lane: events.LaneNormal}
	ev.Ts = ev.Ts.AddDate(2026, 0, 0)
	if err := ev.Validate(); err != nil {
		t.Fatal(err)
	}
}
