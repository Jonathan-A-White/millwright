package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// shareFactory is a factory whose clock reads 2026-10-11: its window is
// 2026-10-04 to 10-11 and the window before is 2026-09-27 to 10-04.
type shareFactory struct {
	t       *testing.T
	tracker *apptest.FakeTracker
	files   *vault.Vault
	now     time.Time
	n       int
	// rig is the rig the rows are landed in; lampas unless a test sets it.
	rig string
}

func newShareFactory(t *testing.T) *shareFactory {
	t.Helper()
	return &shareFactory{t: t, tracker: apptest.NewFakeTracker(), files: vault.New(t.TempDir()),
		now: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC), rig: "lampas"}
}

// rows puts landings on a day, the first invested of them labelled investment,
// and refused rows after them, the refused ones labelled investment too: a
// refusal is no landing, investment or not.
func (f *shareFactory) rows(day string, landed, invested, refused int) {
	f.t.Helper()
	at, err := time.Parse(application.LedgerDate, day)
	if err != nil {
		f.t.Fatal(err)
	}
	for i := 0; i < landed+refused; i++ {
		f.n++
		id := fmt.Sprintf("share-%d", f.n)
		f.tracker.AddStory("", domain.Story{ID: id, Title: "Story " + id, Overrides: domain.Path{Rig: f.rig, Host: "laptop", Model: "sonnet"}})
		if err := f.tracker.SetUpdated(id, f.now); err != nil {
			f.t.Fatal(err)
		}
		outcome := "landed on main"
		if i >= landed {
			outcome = application.NotLanded(application.Reason("tests-fail"), "red")
		}
		if i < invested || i >= landed {
			if err := f.tracker.SetLabels(id, "run:landed", application.LabelInvestment); err != nil {
				f.t.Fatal(err)
			}
		}
		line := application.LedgerLine{When: at, StoryID: id, Title: "Story " + id, Outcome: outcome,
			Path: domain.Path{Model: "sonnet", Effort: "high"}}.String()
		if err := f.files.AppendToLedger(context.Background(), "builder", line); err != nil {
			f.t.Fatal(err)
		}
	}
}

// run runs mw report over the week with the target given, 0 for the default.
func (f *shareFactory) run(target float64, asJSON bool) (application.FactoryReportResult, string) {
	f.t.Helper()
	var out strings.Builder
	result, err := application.FactoryReport{Tracker: f.tracker, Files: f.files, Seat: "builder",
		InvestShare: target, JSON: asJSON, Now: func() time.Time { return f.now }, Out: &out}.Run(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	return result, out.String()
}

func TestTheInvestmentShareIsInvestedLandingsOverAllLandingsAgainstTheTarget(t *testing.T) {
	f := newShareFactory(t)
	f.rows("2026-10-08", 9, 3, 2)
	f.rows("2026-09-30", 4, 1, 1) // refusals not up on the week before, so no bound
	result, printed := f.run(0, false)
	share := result.InvestShare
	if share == nil {
		t.Fatalf("the report has no investment share:\n%s", printed)
	}
	if share.Invested != 3 || share.Landed != 9 || share.Share != 33.3 || share.Target != 33 {
		t.Errorf("the share is %+v, want 3 of 9 landings, 33.3%%, target 33%%", *share)
	}
	if share.PreviousInvested != 1 || share.PreviousLanded != 4 || share.PreviousShare != 25 {
		t.Errorf("the share before is %+v, want 1 of 4, 25%%", *share)
	}
	if share.Arrow.Arrow != application.ArrowUp || share.Off != "" {
		t.Errorf("a share from 25%% to 33.3%% against 33%% reads %+v", *share)
	}
	want := "INVESTMENTS 3/9 (33%) ↑, before 1/4 (25%), target 33%"
	quality, _, _ := strings.Cut(printed, "\nSPEED\n")
	lines := strings.Split(strings.TrimRight(quality, "\n"), "\n")
	if got := lines[len(lines)-1]; got != want {
		t.Errorf("QUALITY ends with %q, want %q:\n%s", got, want, printed)
	}
	if strings.Contains(printed, "target:") || strings.Contains(printed, "under half") || strings.Contains(printed, "over double") {
		t.Errorf("a share on its target is flagged:\n%s", printed)
	}
}

func TestTheInvestmentShareArrowIsLevelInsideTheBandAndFallsBelowIt(t *testing.T) {
	for _, c := range []struct {
		name                   string
		before, beforeInvested int
		now, nowInvested       int
		arrow                  string
		previousText           string
	}{
		{"level", 6, 2, 9, 3, application.ArrowLevel, "before 2/6 (33%)"},
		{"down", 4, 2, 9, 3, application.ArrowDown, "before 2/4 (50%)"},
		{"nothing before", 0, 0, 9, 3, "", "before none landed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newShareFactory(t)
			f.rows("2026-10-08", c.now, c.nowInvested, 0)
			f.rows("2026-09-30", c.before, c.beforeInvested, 0)
			result, printed := f.run(0, false)
			if got := result.InvestShare.Arrow; got.Arrow != c.arrow || got.Verdict != "" {
				t.Errorf("the arrow is %+v, want %q and no verdict: the target judges the share", got, c.arrow)
			}
			if !strings.Contains(printed, ", "+c.previousText+", target 33%") {
				t.Errorf("expected %q on the INVESTMENTS line:\n%s", c.previousText, printed)
			}
		})
	}
}

func TestAShareUnderHalfTheTargetIsSaidInWordsAndIsACheapWin(t *testing.T) {
	f := newShareFactory(t)
	f.rows("2026-10-08", 9, 1, 0)
	result, printed := f.run(0, false)
	if result.InvestShare.Off != application.ShareUnder {
		t.Errorf("1 of 9 against 33%% is %q, want under", result.InvestShare.Off)
	}
	if !strings.Contains(printed, "INVESTMENTS 1/9 (11%), before none landed, target 33%\n  under half the 33% target\n") {
		t.Errorf("the line does not say the share is under half the target:\n%s", printed)
	}
	if !strings.Contains(printed, "CHEAP WINS\n1. investments 11%, under half the 33% target: file more\n") {
		t.Errorf("CHEAP WINS does not carry the share:\n%s", printed)
	}
	if !strings.Contains(result.Summary(), "  investments 1/9 (11%), under half the 33% target\n") {
		t.Errorf("the summary does not carry the share:\n%s", result.Summary())
	}
}

func TestAShareOverDoubleTheTargetIsSaidInWordsAndIsACheapWin(t *testing.T) {
	f := newShareFactory(t)
	f.rows("2026-10-08", 9, 5, 0)
	result, printed := f.run(0.2, false)
	if result.InvestShare.Off != application.ShareOver || result.InvestShare.Target != 20 {
		t.Errorf("5 of 9 against 20%% reads %+v, want over", *result.InvestShare)
	}
	if !strings.Contains(printed, "target 20%\n  over double the 20% target\n") {
		t.Errorf("the line does not say the share is over double the target:\n%s", printed)
	}
	if !strings.Contains(printed, "investments 56%, over double the 20% target: file fewer") {
		t.Errorf("CHEAP WINS does not carry the share:\n%s", printed)
	}
}

func TestTheThresholdsAreHalfAndDoubleTheTargetNotAtThem(t *testing.T) {
	for _, c := range []struct {
		landed, invested int
		target           float64
		off              string
	}{
		{10, 1, 0.2, ""}, // 10% is half of 20%, not under it
		{11, 1, 0.2, application.ShareUnder},
		{10, 4, 0.2, ""}, // 40% is double 20%, not over it
		{9, 4, 0.2, application.ShareOver},
		{6, 1, 0, ""}, // 16.7% against 33%: half is 16.5%
		{7, 1, 0, application.ShareUnder},
		{3, 2, 0, application.ShareOver}, // 66.7% is over double 33%, 66%
		{2, 1, 0.5, ""},                  // nothing is over double a half
	} {
		f := newShareFactory(t)
		f.rows("2026-10-08", c.landed, c.invested, 0)
		result, _ := f.run(c.target, false)
		if got := result.InvestShare.Off; got != c.off {
			t.Errorf("%d of %d against %v: got %q, want %q", c.invested, c.landed, c.target, got, c.off)
		}
	}
}

func TestAWeekWithNoLandingsHasAShareOfNothingAndNoFlag(t *testing.T) {
	f := newShareFactory(t)
	f.rows("2026-10-08", 0, 0, 3)
	result, printed := f.run(0, false)
	if s := result.InvestShare; s == nil || s.Share != 0 || s.Landed != 0 || s.Off != "" {
		t.Fatalf("a week of refusals has the share %+v", s)
	}
	if !strings.Contains(printed, "INVESTMENTS none landed, before none landed, target 33%\n") {
		t.Errorf("the line of a week with no landings:\n%s", printed)
	}
}

func TestTheReportJSONCarriesTheShareAndTheTarget(t *testing.T) {
	f := newShareFactory(t)
	f.rows("2026-10-08", 9, 3, 0)
	_, printed := f.run(0, true)
	var got struct {
		Share map[string]any `json:"invest_share"`
	}
	if err := json.Unmarshal([]byte(printed), &got); err != nil {
		t.Fatal(err)
	}
	if got.Share["share"] != 33.3 || got.Share["target"] != 33.0 || got.Share["invested"] != 3.0 || got.Share["landed"] != 9.0 {
		t.Errorf("the JSON's invest_share is %v", got.Share)
	}
}

// escapedBug files a bug of the rig on a day that names the landing share-1.
func (f *shareFactory) escapedBug(day string) { f.escapedBugBy(day, "") }

// testerBug is escapedBug filed by the Tester: its description carries the marker.
func (f *shareFactory) testerBug(day string) {
	f.escapedBugBy(day, application.TesterFindingMarker+". Source: Tester on share-1 (haiku).")
}

func (f *shareFactory) escapedBugBy(day, description string) {
	f.t.Helper()
	at, err := time.Parse(application.LedgerDate, day)
	if err != nil {
		f.t.Fatal(err)
	}
	f.tracker.AddStory("", domain.Story{ID: "share-bug", Title: "[bug] it breaks after share-1 landed", Overrides: domain.Path{Rig: f.rig, Host: "laptop", Model: "sonnet"}})
	for _, err := range []error{f.tracker.SetType("share-bug", "bug"), f.tracker.SetDescription("share-bug", description), f.tracker.SetCreated("share-bug", at), f.tracker.SetUpdated("share-bug", f.now)} {
		if err != nil {
			f.t.Fatal(err)
		}
	}
}

// readyP1 files an open P1 story of the rig, created ago before now, claimed
// claimedAgo before now unless that is zero.
func (f *shareFactory) readyP1(id string, ago, claimedAgo time.Duration) {
	f.t.Helper()
	f.tracker.AddStory("", domain.Story{ID: id, Title: "Ready " + id, Overrides: domain.Path{Rig: f.rig, Host: "laptop", Model: "sonnet"}})
	for _, err := range []error{f.tracker.SetPriority(id, 1), f.tracker.SetCreated(id, f.now.Add(-ago)), f.tracker.SetUpdated(id, f.now.Add(-ago))} {
		if err != nil {
			f.t.Fatal(err)
		}
	}
	if claimedAgo > 0 {
		at := f.now.Add(-claimedAgo).Format(time.RFC3339)
		if err := f.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.ClaimedAtField: at}); err != nil {
			f.t.Fatal(err)
		}
	}
}

func TestTheShareHasABoundAndAReasonWhenQualityIsFallingOrAnAppStarves(t *testing.T) {
	const day = 24 * time.Hour
	for _, c := range []struct {
		name, bound, reason string
		setup               func(f *shareFactory)
	}{
		{"nothing", "", "", func(f *shareFactory) {
			f.rows("2026-09-30", 9, 3, 1)
			f.rows("2026-10-08", 9, 3, 1)
		}},
		{"escaped bugs up", application.BoundMax, "quality falling: escaped bugs, not counting the Tester's, 0 -> 1", func(f *shareFactory) {
			f.rows("2026-09-30", 9, 3, 0)
			f.rows("2026-10-08", 9, 3, 0)
			f.escapedBug("2026-10-08")
		}},
		{"escaped bugs up only by the Tester", "", "", func(f *shareFactory) {
			f.rows("2026-09-30", 9, 3, 0)
			f.rows("2026-10-08", 9, 3, 0)
			f.testerBug("2026-10-08")
		}},
		{"refusals up", application.BoundMax, "quality falling: refusals 0% -> 20%", func(f *shareFactory) {
			f.rows("2026-09-30", 10, 3, 0)
			f.rows("2026-10-08", 8, 3, 2)
		}},
		{"landings down a third", application.BoundMin, "an app starving: lampas landings 9 -> 6", func(f *shareFactory) {
			f.rows("2026-09-30", 9, 3, 0)
			f.rows("2026-10-08", 6, 2, 0)
		}},
		{"landings down less than a third", "", "", func(f *shareFactory) {
			f.rows("2026-09-30", 9, 3, 0)
			f.rows("2026-10-08", 7, 2, 0)
		}},
		{"a ready P1 untouched", application.BoundMin, "an app starving: lampas has a ready P1 untouched 24 h", func(f *shareFactory) {
			f.rows("2026-09-30", 9, 3, 0)
			f.rows("2026-10-08", 9, 3, 0)
			f.readyP1("share-p1", 3*day, 0)
		}},
		{"a ready P1 claimed since", "", "", func(f *shareFactory) {
			f.rows("2026-09-30", 9, 3, 0)
			f.rows("2026-10-08", 9, 3, 0)
			f.readyP1("share-p1", 3*day, 2*time.Hour)
		}},
		{"a ready P1 filed today", "", "", func(f *shareFactory) {
			f.rows("2026-09-30", 9, 3, 0)
			f.rows("2026-10-08", 9, 3, 0)
			f.readyP1("share-p1", 2*time.Hour, 0)
		}},
		{"quality wins over starving", application.BoundMax, "quality falling: escaped bugs, not counting the Tester's, 0 -> 1", func(f *shareFactory) {
			f.rows("2026-09-30", 9, 3, 0)
			f.rows("2026-10-08", 6, 2, 0)
			f.escapedBug("2026-10-08")
			f.readyP1("share-p1", 3*day, 0)
		}},
		{"a ready P1 before landings down", application.BoundMin, "an app starving: lampas has a ready P1 untouched 24 h", func(f *shareFactory) {
			f.rows("2026-09-30", 9, 3, 0)
			f.rows("2026-10-08", 6, 2, 0)
			f.readyP1("share-p1", 3*day, 0)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newShareFactory(t)
			c.setup(f)
			result, printed := f.run(0, false)
			got := result.InvestShare
			if got.Bound != c.bound || got.Reason != c.reason {
				t.Fatalf("the bound is %q for %q, want %q for %q", got.Bound, got.Reason, c.bound, c.reason)
			}
			var words string
			switch c.bound {
			case application.BoundMax:
				words = "bound: up to 33% allowed (" + c.reason + ")"
			case application.BoundMin:
				words = "bound: down to 10% asked (" + c.reason + ")"
			}
			// The words wrap at the report's width; they are one sentence.
			flat := func(text string) string { return strings.Join(strings.Fields(text), " ") }
			if words != "" && !strings.Contains(flat(printed), "target 33% "+words+" ") {
				t.Errorf("the INVESTMENTS line is not followed by %q:\n%s", words, printed)
			}
			if words != "" && !strings.Contains(flat(result.Summary()), words) {
				t.Errorf("the phone summary lacks %q:\n%s", words, result.Summary())
			}
			if words == "" && (strings.Contains(printed, "bound:") || strings.Contains(result.Summary(), "bound:")) {
				t.Errorf("a share with no bound prints one:\n%s", printed)
			}
			for _, line := range strings.Split(printed, "\n") {
				if len([]rune(line)) > application.ReportWidth {
					t.Errorf("the line %q is wider than %d columns", line, application.ReportWidth)
				}
			}
		})
	}
}

func TestTheShareBoundsAreTheConfigsAndShowInTheJSON(t *testing.T) {
	f := newShareFactory(t)
	f.rows("2026-09-30", 9, 3, 0)
	f.rows("2026-10-08", 9, 3, 0)
	f.escapedBug("2026-10-08")
	var out strings.Builder
	_, err := application.FactoryReport{Tracker: f.tracker, Files: f.files, Seat: "builder", InvestShareMax: 0.5, JSON: true,
		Now: func() time.Time { return f.now }, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Share map[string]any `json:"invest_share"`
	}
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Share["bound"] != "max" || got.Share["max"] != 50.0 || got.Share["min"] != 10.0 || got.Share["reason"] != "quality falling: escaped bugs, not counting the Tester's, 0 -> 1" {
		t.Errorf("the JSON's invest_share is %v", got.Share)
	}
}
