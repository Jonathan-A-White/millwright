package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func centCats(giving, college int64) []application.YNABCategory {
	return []application.YNABCategory{
		{ID: "g1", Name: "Giving", GoalTarget: giving, HasTarget: true, GoalCadence: 1, GoalCadenceFrequency: 1},
		{ID: "c1", Name: "College", GoalTarget: college, HasTarget: true, GoalCadence: 1, GoalCadenceFrequency: 1},
	}
}

func centRows() [][]string {
	return [][]string{{"Item", "Total Per Month"}, {"Giving", "$2,158.33"}, {"College", "$833.33"}}
}

func TestAMonthlyTargetOneCentFromTheSheetIsNoChangeInADryRun(t *testing.T) {
	y := &fakeYNAB{cats: centCats(2158340, 833340)}
	out := runSync(t, true, &fakeSheet{rows: centRows()}, y)
	if strings.Contains(out, "Giving") || strings.Contains(out, "College") || !strings.HasPrefix(out, "nothing to change\n") {
		t.Errorf("a target one cent from the sheet was listed as a change:\n%s", out)
	}
}

func TestAMonthlyTargetOneCentBelowTheSheetIsNoChangeEither(t *testing.T) {
	y := &fakeYNAB{cats: centCats(2158320, 833320)}
	out := runSync(t, false, &fakeSheet{rows: centRows()}, y)
	if len(y.patches) != 0 || !strings.HasPrefix(out, "nothing to change\n") {
		t.Errorf("wrote %v\n%s", y.patches, out)
	}
}

func TestAMonthlyTargetTwoCentsFromTheSheetIsStillWritten(t *testing.T) {
	y := &fakeYNAB{cats: centCats(2158350, 833310)}
	out := runSync(t, false, &fakeSheet{rows: centRows()}, y)
	if len(y.patches) != 2 || y.patches[0] != (patch{"g1", 2158330}) || y.patches[1] != (patch{"c1", 833330}) {
		t.Fatalf("patches = %v\n%s", y.patches, out)
	}
	if !strings.Contains(out, "changed Giving: 2158.35 -> 2158.33") {
		t.Errorf("output:\n%s", out)
	}
}
