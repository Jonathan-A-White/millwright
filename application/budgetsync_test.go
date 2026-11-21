package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

type fakeSheet struct {
	rows        [][]string
	id, tab     string
	err         error
	readsCalled int
}

func (f *fakeSheet) Read(_ context.Context, id, tab, _ string) ([][]string, error) {
	f.readsCalled++
	f.id, f.tab = id, tab
	return f.rows, f.err
}

type patch struct {
	category string
	milli    int64
}

type fakeYNAB struct {
	cats    []application.YNABCategory
	patches []patch
	failOn  string
}

func (f *fakeYNAB) Categories(context.Context, string) ([]application.YNABCategory, error) {
	return f.cats, nil
}

func (f *fakeYNAB) SetGoalTarget(_ context.Context, _, category string, milli int64) error {
	if category == f.failOn {
		return errors.New("YNAB said no")
	}
	f.patches = append(f.patches, patch{category, milli})
	for i := range f.cats {
		if f.cats[i].ID == category {
			f.cats[i].GoalTarget, f.cats[i].HasTarget = milli, true
		}
	}
	return nil
}

func sheetRows() [][]string {
	return [][]string{
		{"Monthly budget 2025"},
		{"Item", "Notes", "Total Per Month"},
		{"Personal Maintenance", "", "$239.00"},
		{"utilities", "", "$1,215.00"},
		{"Rent", "", "$2,000"},
		{"Gifts", "", "#REF!"},
		{"Fun Money", "", ""},
		{"Pets", "", "$40"},
		{"", "", "$99"},
	}
}

func ynabCats() []application.YNABCategory {
	return []application.YNABCategory{
		{ID: "c1", Name: "Personal  Maintenance", GoalTarget: 102000, HasTarget: true},
		{ID: "c2", Name: "Utilities", GoalTarget: 1009000, HasTarget: true},
		{ID: "c3", Name: "Rent", GoalTarget: 2000000, HasTarget: true},
		{ID: "c4", Name: "Gifts", GoalTarget: 50000, HasTarget: true},
		{ID: "c5", Name: "Fun Money", GoalTarget: 25000, HasTarget: true},
		{ID: "c6", Name: "Old Hobby", GoalTarget: 12000, HasTarget: true},
		{ID: "c7", Name: "Groceries"},
		{ID: "c8", Name: "Hidden Thing", GoalTarget: 7000, HasTarget: true, Hidden: true},
	}
}

func runSync(t *testing.T, dry bool, sheet *fakeSheet, y *fakeYNAB) string {
	t.Helper()
	var out bytes.Buffer
	err := application.BudgetSync{Sheet: sheet, YNAB: y, Out: &out, SheetID: "sheet1", Tab: "2025", BudgetID: "b1", DryRun: dry}.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	return out.String()
}

func TestADryRunSaysOldAndNewTargetsAndWritesNothing(t *testing.T) {
	sheet, y := &fakeSheet{rows: sheetRows()}, &fakeYNAB{cats: ynabCats()}
	out := runSync(t, true, sheet, y)

	if len(y.patches) != 0 {
		t.Fatalf("a dry run wrote %v", y.patches)
	}
	if sheet.id != "sheet1" || sheet.tab != "2025" {
		t.Fatalf("read %q tab %q", sheet.id, sheet.tab)
	}
	for _, want := range []string{
		"Personal Maintenance: 102.00 -> 239.00",
		"Utilities: 1009.00 -> 1215.00",
		"sheet items with no YNAB category:\n  Pets\n",
		"YNAB categories with a target but no sheet row:\n  Old Hobby (12.00)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Rent:") {
		t.Errorf("an equal amount was listed as a change:\n%s", out)
	}
	if strings.Contains(out, "Hidden Thing") {
		t.Errorf("a hidden category was listed:\n%s", out)
	}
}

func TestASyncWritesOnlyTheChangedTargetsInMilliunitsAndASecondRunChangesNothing(t *testing.T) {
	sheet, y := &fakeSheet{rows: sheetRows()}, &fakeYNAB{cats: ynabCats()}
	out := runSync(t, false, sheet, y)

	want := []patch{{"c1", 239000}, {"c2", 1215000}}
	if len(y.patches) != len(want) {
		t.Fatalf("patches = %v, want %v", y.patches, want)
	}
	for i, p := range want {
		if y.patches[i] != p {
			t.Errorf("patch %d = %v, want %v", i, y.patches[i], p)
		}
	}
	if !strings.Contains(out, "changed Personal Maintenance: 102.00 -> 239.00") {
		t.Errorf("each change is printed:\n%s", out)
	}

	again := runSync(t, false, sheet, y)
	if !strings.HasPrefix(again, "nothing to change\n") {
		t.Errorf("second run printed:\n%s", again)
	}
	if len(y.patches) != len(want) {
		t.Errorf("the second run wrote %v", y.patches[len(want):])
	}
}

func TestABlankOrErrorTotalIsSkippedAndNamedNeverWrittenAsZero(t *testing.T) {
	sheet, y := &fakeSheet{rows: sheetRows()}, &fakeYNAB{cats: ynabCats()}
	out := runSync(t, false, sheet, y)

	for _, p := range y.patches {
		if p.category == "c4" || p.category == "c5" {
			t.Fatalf("a skipped row was written: %v", p)
		}
	}
	for _, want := range []string{"Gifts: the Total Per Month is #REF!", "Fun Money: the Total Per Month is blank"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "no sheet row:\n  Gifts") || strings.Contains(out, "  Fun Money (") {
		t.Errorf("a skipped row's category was also called rowless:\n%s", out)
	}
}

func TestACategoryWithNoTargetGetsOneAndZeroIsARealAmount(t *testing.T) {
	rows := [][]string{{"Item", "Total Per Month"}, {"Groceries", "$0.10"}, {"Rent", "0"}}
	y := &fakeYNAB{cats: ynabCats()}
	out := runSync(t, false, &fakeSheet{rows: rows}, y)
	if len(y.patches) != 2 || y.patches[0] != (patch{"c7", 100}) || y.patches[1] != (patch{"c3", 0}) {
		t.Fatalf("patches = %v\n%s", y.patches, out)
	}
	if !strings.Contains(out, "Groceries: no target -> 0.10") {
		t.Errorf("output:\n%s", out)
	}
}

func TestAnItemOnTheSheetTwiceWithDifferentAmountsIsSkipped(t *testing.T) {
	rows := [][]string{{"Item", "Total Per Month"}, {"Rent", "1"}, {"rent", "2"}, {"Utilities", "5"}, {"utilities", "5"}}
	y := &fakeYNAB{cats: ynabCats()}
	out := runSync(t, false, &fakeSheet{rows: rows}, y)
	if len(y.patches) != 1 || y.patches[0].category != "c2" {
		t.Fatalf("patches = %v\n%s", y.patches, out)
	}
	if !strings.Contains(out, "twice with different amounts") {
		t.Errorf("output:\n%s", out)
	}
}

func TestASheetWithoutTheTwoColumnsIsRefusedAndNothingIsWritten(t *testing.T) {
	y := &fakeYNAB{cats: ynabCats()}
	err := application.BudgetSync{Sheet: &fakeSheet{rows: [][]string{{"Item", "Amount"}}}, YNAB: y, Out: &bytes.Buffer{}}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Total Per Month") {
		t.Fatalf("err = %v", err)
	}
	if len(y.patches) != 0 {
		t.Fatalf("wrote %v", y.patches)
	}
}

func TestARefusedWriteStopsTheSyncAndSaysWhatWasChanged(t *testing.T) {
	y := &fakeYNAB{cats: ynabCats(), failOn: "c2"}
	var out bytes.Buffer
	err := application.BudgetSync{Sheet: &fakeSheet{rows: sheetRows()}, YNAB: y, Out: &out, Tab: "2025"}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Utilities") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out.String(), "changed Personal Maintenance") {
		t.Errorf("output:\n%s", out.String())
	}
}
