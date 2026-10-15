package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func readings(rs ...application.SeatWindowContext) func(context.Context) ([]application.SeatWindowContext, error) {
	return func(context.Context) ([]application.SeatWindowContext, error) { return rs, nil }
}

func deputyAt(tokens int) application.SeatWindowContext {
	return application.SeatWindowContext{
		Seat: "deputy", Window: application.ReapWindow{ID: "@7", Name: "deputy-2026-10-15-03"},
		Tokens: tokens, HandoffAt: 200000,
	}
}

func TestSeatContextNamesADeputyWindowAt225K(t *testing.T) {
	verdict, why := NewSeatContext(readings(deputyAt(225000))).Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("verdict %v (%s), want faulty", verdict, why)
	}
	for _, want := range []string{"deputy-2026-10-15-03", "225000", "200000"} {
		if !strings.Contains(why, want) {
			t.Errorf("the reason %q does not name %s", why, want)
		}
	}
}

func TestSeatContextIsOkWithin20KOfTheLimit(t *testing.T) {
	for _, tokens := range []int{50000, 205000, 219999} {
		if verdict, why := NewSeatContext(readings(deputyAt(tokens))).Probe(context.Background()); verdict != application.DoctorOK {
			t.Errorf("%d tokens: verdict %v (%s), want ok", tokens, verdict, why)
		}
	}
}

func TestSeatContextIsOkWithNoSeatWindowsOpen(t *testing.T) {
	if verdict, _ := NewSeatContext(readings()).Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("verdict %v, want ok", verdict)
	}
}

func TestSeatContextCannotTellWhenTheWindowsCannotBeRead(t *testing.T) {
	read := func(context.Context) ([]application.SeatWindowContext, error) { return nil, errors.New("no tmux") }
	if verdict, why := NewSeatContext(read).Probe(context.Background()); verdict != application.DoctorCannotTell || !strings.Contains(why, "no tmux") {
		t.Fatalf("verdict %v (%s), want cannot-tell naming the cause", verdict, why)
	}
}

func TestSeatContextHasNoCure(t *testing.T) {
	if err := NewSeatContext(readings()).Cure(context.Background()); err == nil {
		t.Fatal("a cure was offered for a session that needs to hand off by itself")
	}
}
