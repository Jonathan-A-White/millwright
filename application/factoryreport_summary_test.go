package application_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// fullestSummaryResult is the report with everything the phone summary can
// say switched on: every QUALITY, SPEED and FUEL line, the investment share far
// off its target with its parts and its bound, his time with two slow-making
// boxes, a WORSE change, every MODELS column and the most CHEAP WINS, the
// wrapping ones at their longest.
func fullestSummaryResult() application.FactoryReportResult {
	up := application.Trend{Arrow: application.ArrowUp, Verdict: application.VerdictBetter}
	whole := &application.ReportBlock{
		Name: "factory",
		Quality: application.Quality{Landed: 9, Refused: 3, RefusalRate: 25, AttemptsPerLanding: 1.5, BugsPerLanding: 0.3,
			EscapedBugs: 4, EscapedTester: 1},
		Speed: application.Speed{WithPar: 5, Measured: 5, ParMedian: 1.2, ParP90: 2.4, DispatchToLandingS: 1800},
		Fuel:  application.FuelUse{WithTokens: 9, WithCost: 9, TokensPerLanding: 1_250_000, USDPerLanding: 3.5},
		Arrows: map[string]application.Trend{"landed": up, "refusal_rate": up, "attempts_per_landing": up,
			"bugs_per_landing": up, "escaped_bugs": up, "par_median": up, "par_p90": up,
			"dispatch_to_landing": up, "tokens_per_landing": up, "usd_per_landing": up},
	}
	var models []application.ModelUse
	for _, m := range application.ModelColumns {
		models = append(models, application.ModelUse{Model: string(m), Sessions: 4})
	}
	slow := func(box string) string {
		return "cloud make: " + box + " has been made for 56m (limit 30m), Vultr says active locked; mw cloud destroy " + box
	}
	return application.FactoryReportResult{
		Window: application.ReportWindow{
			Since: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC), Until: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)},
		Whole: whole,
		InvestShare: &application.InvestShare{Invested: 1, Picked: 0, Friction: 1, Landed: 9, Share: 11, PreviousInvested: 2,
			PreviousLanded: 9, Arrow: up, Target: 33, Off: application.ShareUnder, Min: 10, Max: 33,
			Bound: application.BoundMax, Reason: "quality falling: escaped bugs, not counting the Tester's, 1 -> 3"},
		Review:       &application.ReviewTime{Queue: 6, QueueLimit: 5, DemosWaiting: 2, CloudMake: []string{slow("cloud1"), slow("cloud2")}},
		Changes:      []application.FactoryChange{{Story: "mw-x.1", Title: "A change that made things worse", Verdict: application.ChangeWorse}},
		ModelCompare: &application.ModelCompare{Models: models},
		CheapWins: []application.CheapWin{
			{Rule: application.WinWorseChange, Text: "mw-x.1 made escaped bugs worse: file a fix (Fixes: mw-x.1)", Shown: true},
			{Rule: application.WinCausesUnset, Text: "8 refusals have no cause: mw cause mw-a.1 mw-a.2 mw-a.3 mw-a.4 mw-a.5 +3 more", Shown: true},
			{Rule: application.WinDominantReason, Text: "merge-conflict is 80% of millwright refusals: CHANGELOG.md (changelog) 9, package.json 7, go.sum 2", Shown: true},
		},
	}
}

// The phone summary holds to SummaryLines in the fullest report it can print.
func TestTheFullestPhoneSummaryHoldsToSummaryLines(t *testing.T) {
	text := fullestSummaryResult().Summary()
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > application.SummaryLines {
		t.Errorf("the fullest summary is %d lines, SummaryLines is %d:\n%s", len(lines), application.SummaryLines, text)
	}
	for _, line := range lines {
		if len([]rune(line)) > application.ReportWidth {
			t.Errorf("a line is wider than %d columns: %q", application.ReportWidth, line)
		}
	}
	// The test is only worth its name when it really printed everything.
	for _, want := range []string{"QUALITY", "SPEED", "FUEL", "HIS TIME", "cloud1", "cloud2", "CHANGES", "MODELS", "CHEAP WINS", "bound:", "friction-register", "3. "} {
		if !strings.Contains(text, want) {
			t.Errorf("the fullest summary lacks %q:\n%s", want, text)
		}
	}
}
