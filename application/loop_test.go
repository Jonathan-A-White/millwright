package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// loopFixture is a week to 2026-10-16: lampas refused two stories (one with a
// cause, one without), a millwright change landed on 10-02 whose 14th day is
// 10-16, and one landed on 10-06 whose 14th day is 10-20, too soon.
func loopFixture() application.LoopInputs {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	window := application.ReportWindow{Since: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Until: now}
	previous := application.ReportWindow{Since: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Until: window.Since}
	causes := map[string]int{"code": 1, "test": 0, "tooling": 0, "process": 0, application.CauseUnset: 1}
	lampas := application.ReportBlock{Name: "lampas",
		Quality: application.Quality{Landed: 4, Refused: 2, RefusalRate: 33.3, Causes: causes, Uncaused: []string{"lampas-9"}}}
	whole := application.ReportBlock{Name: "factory",
		Quality: application.Quality{Landed: 9, Refused: 2, RefusalRate: 18.2, Invested: 3, Causes: causes}}
	kept := 10.0
	after := 8.0
	return application.LoopInputs{
		Now: now,
		Result: application.FactoryReportResult{
			Window: window, PreviousWindow: previous,
			Rigs: []application.ReportBlock{lampas}, Whole: &whole,
			InvestShare: &application.InvestShare{Invested: 3, Landed: 9, Share: 33.3, Target: 33},
			Changes: []application.FactoryChange{
				{Story: "mw-a.1", Title: "Quieter refusals", Landed: "2026-10-02", AfterEnds: "2026-10-16", Verdict: application.ChangeKept,
					Expect: domain.Expect{Metric: "refusals", Direction: "down"}, Before: &kept, After: &after},
				{Story: "mw-a.2", Title: "Not yet", Landed: "2026-10-06", AfterEnds: "2026-10-20", Verdict: application.ChangeTooSoon},
				{Story: "mw-a.0", Title: "Long ago", Landed: "2026-09-01", AfterEnds: "2026-09-15", Verdict: application.ChangeWorse},
			},
		},
		Classes: []application.ErrorClass{
			{Slug: "stale-lock", Kinds: []string{"go-cli"}, Symptom: "a lock", Shared: "candidate", Checks: []application.ErrorCheck{
				{Date: "2026-10-12", Rig: "lampas", Found: "lampas-1"}, {Date: "2026-10-13", Rig: "legend", Found: "legend-1"}}},
			{Slug: "one-off", Kinds: []string{"go-cli"}, Symptom: "once", Checks: []application.ErrorCheck{
				{Date: "2026-10-14", Rig: "lampas", Found: "lampas-2"}}},
			{Slug: "old-class", Kinds: []string{"go-cli"}, Symptom: "old", Checks: []application.ErrorCheck{
				{Date: "2026-09-01", Rig: "lampas", Found: "lampas-0"}}},
		},
		Investments: []application.Investment{{Made: application.InvestmentRecord{Kind: application.RecordInvestment, Bead: "mw-inv.1",
			At: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), Aim: "cut merges", Measure: "merge refusals", ReviewDays: 14}}},
		Ideas: []application.ImprovementIdea{
			{Title: "Cache the gate", Expect: "Expect: dispatch-to-landing down"},
			{Title: "Already filed", Expect: "Expect: refusals down", Bead: "mw-x.1"},
		},
		HaveIdeas: true,
	}
}

func headingsOf(text string) []string {
	var found []string
	for _, line := range strings.Split(text, "\n") {
		for _, h := range []string{"QUALITY, SPEED, FUEL", "CAUSES", "ERRORS", "VERDICTS", "IDEAS", "SHARE"} {
			if strings.HasPrefix(line, h) {
				found = append(found, h)
			}
		}
	}
	return found
}

func TestLoopBriefPrintsSixHeadingsInOrder(t *testing.T) {
	text := application.ReadLoopBrief(loopFixture()).String()
	want := []string{"QUALITY, SPEED, FUEL", "CAUSES", "ERRORS", "VERDICTS", "IDEAS", "SHARE"}
	if got := headingsOf(text); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("headings %v, want %v in:\n%s", got, want, text)
	}
	for _, line := range []string{
		"QUALITY landed 9", // the summary line mw report prints
		"lampas: refused 2\n    code 1 · unset 1",
		"unset 1",
		"stale-lock",
		"Quieter refusals",
		"KEPT",
		"mw-inv.1",
		"Cache the gate",
		"INVESTMENTS 3/9",
	} {
		if !strings.Contains(text, line) {
			t.Errorf("the brief lacks %q:\n%s", line, text)
		}
	}
	if strings.Index(text, "unset 1") < strings.Index(text, "code 1") {
		t.Errorf("the uncaused count is not last:\n%s", text)
	}
}

func TestLoopBriefFallsBackToItsNotYetWords(t *testing.T) {
	in := loopFixture()
	in.Ideas, in.HaveIdeas = nil, false
	text := application.ReadLoopBrief(in).String()
	for _, line := range []string{"models: not yet (mw-2zh33g.16)", "ideas: no ledger yet (mw-1ljpo9.11)"} {
		if !strings.Contains(text, line) {
			t.Errorf("the brief lacks %q:\n%s", line, text)
		}
	}
	if strings.Contains(text, "bound") {
		t.Errorf("a result with no bound printed one:\n%s", text)
	}
}

func TestLoopBriefLeavesOutFiledIdeasAndErrorsOutsideTheWindow(t *testing.T) {
	text := application.ReadLoopBrief(loopFixture()).String()
	if strings.Contains(text, "Already filed") {
		t.Errorf("an idea with a bead was listed:\n%s", text)
	}
	if strings.Contains(text, "old-class") {
		t.Errorf("an error class seen only before the window was listed:\n%s", text)
	}
	if strings.Contains(text, "Not yet") || strings.Contains(text, "Long ago") {
		t.Errorf("a change whose 14th day is not in the window was listed:\n%s", text)
	}
	if !strings.Contains(text, "lampas") || !strings.Contains(text, "candidate") {
		t.Errorf("the class seen in the window lacks its rig or verdict:\n%s", text)
	}
}

func TestLoopBriefJSONHasTheSixKeys(t *testing.T) {
	raw, err := json.Marshal(application.ReadLoopBrief(loopFixture()))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("not one object: %v", err)
	}
	if len(object) != 6 {
		t.Errorf("got %d keys: %s", len(object), raw)
	}
	for _, key := range []string{"quality_speed_fuel", "causes", "errors", "verdicts", "ideas", "share"} {
		if _, ok := object[key]; !ok {
			t.Errorf("no key %q in %s", key, raw)
		}
	}
}

func TestLoopBriefPrintsTheBoundTheResultCarries(t *testing.T) {
	in := loopFixture()
	in.Result.InvestShare.Bound = application.BoundMax
	in.Result.InvestShare.Max = 33
	in.Result.InvestShare.Reason = "quality falling: refusals up"
	text := application.ReadLoopBrief(in).String()
	if !strings.Contains(text, "bound: up to 33% allowed (quality falling: refusals up)") {
		t.Errorf("the brief lacks the bound line:\n%s", text)
	}
}

func errorsSection(text string) string {
	start := strings.Index(text, "ERRORS")
	end := strings.Index(text, "VERDICTS")
	return text[start:end]
}

func TestLoopBriefDefaultListsOnlyClassesSeenTwiceOrMoreWithTheCount(t *testing.T) {
	section := errorsSection(application.ReadLoopBrief(loopFixture()).String())
	if !strings.Contains(section, "2 classes seen in the window") {
		t.Errorf("the ERRORS count is missing:\n%s", section)
	}
	if !strings.Contains(section, "stale-lock: seen 2 times, in lampas, legend") {
		t.Errorf("a class seen twice in the week was not listed:\n%s", section)
	}
	if strings.Contains(section, "one-off") {
		t.Errorf("a class seen once was listed:\n%s", section)
	}
}

func TestLoopBriefNoErrorsOmitsTheRegister(t *testing.T) {
	brief := application.ReadLoopBrief(loopFixture()).WithoutErrors()
	section := errorsSection(brief.String())
	for _, slug := range []string{"stale-lock", "one-off"} {
		if strings.Contains(section, slug) {
			t.Errorf("--no-errors listed %s:\n%s", slug, section)
		}
	}
	if !strings.Contains(section, "2 classes seen in the window") {
		t.Errorf("--no-errors lost the count:\n%s", section)
	}
	raw, err := json.Marshal(brief)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "stale-lock") {
		t.Errorf("--no-errors left the register in the JSON: %s", raw)
	}
	if !strings.Contains(string(raw), `"errors":[]`) {
		t.Errorf("the errors key is not an empty list: %s", raw)
	}
}

func TestTheDampersNameAWeeklyLoopSittingMoreThanEightDaysOld(t *testing.T) {
	facts := aCleanDamperFixture() // now is 2026-09-18
	facts.LoopSitting = application.ParseLoopSitting("2026-09-09")
	var b strings.Builder
	application.ReadDampers(facts, application.DamperLimits{}).Write(&b)
	if !strings.Contains(b.String(), "2026-09-09") || !strings.Contains(b.String(), "loop sitting") {
		t.Errorf("expected a DAMPERS line naming the last sitting's date, got:\n%s", b.String())
	}
}

func TestTheDampersSayNothingOfASittingRecordedInTheLastEightDays(t *testing.T) {
	for _, day := range []string{"2026-09-10", "2026-09-18"} {
		facts := aCleanDamperFixture()
		facts.LoopSitting = application.ParseLoopSitting(day)
		var b strings.Builder
		application.ReadDampers(facts, application.DamperLimits{}).Write(&b)
		if strings.Contains(b.String(), "loop sitting") {
			t.Errorf("a sitting of %s is in time, got:\n%s", day, b.String())
		}
	}
}

func TestTheDampersSayNoSittingIsRecordedWhenNoneIs(t *testing.T) {
	facts := aCleanDamperFixture()
	facts.LoopSitting = application.ParseLoopSitting("")
	var b strings.Builder
	application.ReadDampers(facts, application.DamperLimits{}).Write(&b)
	if !strings.Contains(b.String(), "no weekly loop sitting recorded") {
		t.Errorf("expected the line for no sitting, got:\n%s", b.String())
	}
}

func TestTheDampersJudgeNoSittingWhereTheNotesWereNotRead(t *testing.T) {
	var b strings.Builder
	application.ReadDampers(aCleanDamperFixture(), application.DamperLimits{}).Write(&b)
	if strings.Contains(b.String(), "loop sitting") {
		t.Errorf("a reader with no notes says nothing of the sitting, got:\n%s", b.String())
	}
}

func TestTheSurveyReadsTheSittingFromTheNoteMwLoopDoneWrites(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	survey := application.DamperSurvey{Tracker: tracker, Notes: tracker}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	read := func() string {
		report, err := survey.Dampers(ctx, application.WorkInHand{}, now)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, a := range report.Alarms {
			b.WriteString(a.Line() + "\n")
		}
		return b.String()
	}
	if got := read(); !strings.Contains(got, "no weekly loop sitting recorded") {
		t.Errorf("before any sitting expected the line, got %q", got)
	}
	var out strings.Builder
	done := application.LoopDone{Notes: tracker, Now: func() time.Time { return now }, Day: "2026-10-25", Out: &out}
	if err := done.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := read(); !strings.Contains(got, "2026-10-25") {
		t.Errorf("a sitting of 10-25 is 11 days old: expected the line naming it, got %q", got)
	}
	done.Day = ""
	if err := done.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := read(); strings.Contains(got, "loop sitting") {
		t.Errorf("a sitting of today leaves no line, got %q", got)
	}
}

func TestMwLoopDoneKeepsTheLaterDayAndRefusesAFutureOrBadOne(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	run := func(day string) error {
		return application.LoopDone{Notes: tracker, Now: func() time.Time { return now }, Day: day}.Run(ctx)
	}
	if err := run("2026-11-01"); err != nil {
		t.Fatal(err)
	}
	if err := run("2026-10-25"); err != nil {
		t.Fatal(err)
	}
	if got, _ := tracker.Note(ctx, application.LoopSittingKey); got != "2026-11-01" {
		t.Errorf("an earlier day must not replace a later one, the note is %q", got)
	}
	for _, bad := range []string{"2026-11-09", "last sunday"} {
		if err := run(bad); err == nil {
			t.Errorf("--day %q should be refused", bad)
		}
	}
}
