package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func TestAGrindLineLeavesOutAPhaseItDoesNotKnowAndKeepsAZeroItDoes(t *testing.T) {
	zero := 0.0
	encoded, err := json.Marshal(application.GrindLine{Txid: "direct:a", Seconds: 4.2, SlotSeconds: &zero})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"slot_s":0`) || !strings.Contains(text, `"seconds":4.2`) {
		t.Errorf("expected seconds and a slot_s of 0 in %s", text)
	}
	for _, absent := range []string{"wake_s", "pickup_s", "startup_s", "model_s", "deliver_s", "total_s"} {
		if strings.Contains(text, absent) {
			t.Errorf("%s is written though nobody measured it: %s", absent, text)
		}
	}
}

func TestGristTimingTakesTheMedianOfAnEvenCountAndTheNearestRankP90(t *testing.T) {
	state := &apptest.FakeGristState{}
	for i := 1; i <= 10; i++ {
		_ = state.Append(context.Background(), application.GrindLine{
			Time: time.Date(2026, 10, 7, 9, i, 0, 0, time.UTC), App: "lampas", Kind: "verse-read", Seconds: float64(i),
		})
	}
	var out bytes.Buffer
	groups, err := application.GristTiming{State: state, Out: &out}.Run(context.Background(), time.Time{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Rows) != 1 {
		t.Fatalf("expected one group with only the seconds row, got %+v", groups)
	}
	row := groups[0].Rows[0]
	if row.Count != 10 || row.Median != 5.5 || row.P90 != 9 {
		t.Errorf("expected 10, 5.5 and 9, got %+v", row)
	}
}

func TestGristTimingSaysSoWhenNothingFits(t *testing.T) {
	var out bytes.Buffer
	groups, err := application.GristTiming{State: &apptest.FakeGristState{}, Out: &out}.Run(context.Background(), time.Time{}, "lampas", "")
	if err != nil || len(groups) != 0 || !strings.Contains(out.String(), "no grinds") {
		t.Errorf("got %v, %v, %q", groups, err, out.String())
	}
}
