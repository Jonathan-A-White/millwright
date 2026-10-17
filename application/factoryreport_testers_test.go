package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// testerRow files a Tester story of a rig and puts its ledger row, written by
// a haiku session, on a day.
func (f *changeFactory) testerRow(day, id, outcome string) {
	f.t.Helper()
	f.tracker.AddStory("", domain.Story{ID: id, Title: "Test: " + id, Overrides: domain.Path{Rig: "lampas", Host: "cloud1", Model: "haiku", Formula: application.TesterFormula}})
	if err := f.tracker.SetLabels(id, application.LabelTester); err != nil {
		f.t.Fatal(err)
	}
	at, err := time.Parse(application.LedgerDate, day)
	if err != nil {
		f.t.Fatal(err)
	}
	line := application.LedgerLine{When: at, StoryID: id, Title: "Test: " + id, Outcome: outcome,
		Path: domain.Path{Model: "haiku", Effort: "max"}}.String()
	if err := f.files.AppendToLedger(context.Background(), "builder", line); err != nil {
		f.t.Fatal(err)
	}
}

func TestReportModelsCountsAHaikuTestersSessionsAndSaysTheyAreNotQualitys(t *testing.T) {
	f := newChangeFactory(t)
	f.rows("2026-10-18", "lampas", 2, 0)
	f.testerRow("2026-10-18", "mw-x.1", "tested mw-x.0: 3 findings (1 bug, 2 taste); nothing merged")
	f.testerRow("2026-10-18", "mw-x.2", "tested mw-x.0: 2 findings (0 bug, 2 taste); nothing merged")
	f.testerRow("2026-10-19", "mw-x.3", application.NotLanded(application.Reason("open-steps"), "6 formula step(s) of mw-mol-1 are still open"))

	result, printed := f.report(false)
	haiku := modelUse(result.ModelCompare, "haiku")
	if haiku == nil {
		t.Fatalf("MODELS has no haiku column:\n%s", printed)
	}
	if haiku.Landed != 2 || haiku.Refused != 1 || haiku.Sessions != 3 {
		t.Errorf("haiku is %+v, want 2 tested as landed and 1 refused", *haiku)
	}
	if got := result.Rigs[0].Quality.Landed; got != 2 {
		t.Errorf("QUALITY landed %d: a Tester's row moved it, want 2", got)
	}
	if !strings.Contains(printed, "haiku") {
		t.Errorf("the printed MODELS has no haiku:\n%s", printed)
	}
	// MODELS lands 2 more than QUALITY: one line says what the gap is.
	if !strings.Contains(printed, "landed vs QUALITY's: +2 Tester sessions\n") {
		t.Errorf("the gap between MODELS and QUALITY is not said:\n%s", printed)
	}
}

func TestReportModelsSayNothingOfAGapWhenTheTotalsAgree(t *testing.T) {
	f := newChangeFactory(t)
	f.rows("2026-10-18", "lampas", 2, 1)
	_, printed := f.report(false)
	if strings.Contains(printed, "than QUALITY's") {
		t.Errorf("a gap is said though the totals agree:\n%s", printed)
	}
}

// modelUse is the column of a model in MODELS, nil when it has none.
func modelUse(c *application.ModelCompare, model string) *application.ModelUse {
	if c == nil {
		return nil
	}
	for i := range c.Models {
		if c.Models[i].Model == model {
			return &c.Models[i]
		}
	}
	return nil
}
