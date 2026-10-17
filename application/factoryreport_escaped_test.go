package application_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// landing files a story of a rig run on a model and lands it on day.
func (f *changeFactory) landing(id, rig, model, acceptance, day string) {
	f.t.Helper()
	f.tracker.AddStory("", domain.Story{ID: id, Title: "The landing " + id, Overrides: domain.Path{Rig: rig, Host: "laptop", Model: domain.Model(model)}})
	if err := f.tracker.SetAcceptance(id, acceptance); err != nil {
		f.t.Fatal(err)
	}
	if err := f.tracker.SetUpdated(id, f.now); err != nil {
		f.t.Fatal(err)
	}
	at, err := time.Parse(application.LedgerDate, day)
	if err != nil {
		f.t.Fatal(err)
	}
	f.ledger(at, id, "landed on main")
}

// filed files a story of a rig, of a kind ("bug" or "task"), at a moment, with
// a description.
func (f *changeFactory) filed(id, kind, title, rig, created, description string) {
	f.t.Helper()
	at, err := time.Parse(time.RFC3339, created)
	if err != nil {
		f.t.Fatal(err)
	}
	f.tracker.AddStory("", domain.Story{ID: id, Title: title, Overrides: domain.Path{Rig: rig, Host: "laptop", Model: "sonnet"}})
	for _, set := range []func() error{
		func() error { return f.tracker.SetType(id, kind) },
		func() error { return f.tracker.SetDescription(id, description) },
		func() error { return f.tracker.SetCreated(id, at) },
		func() error { return f.tracker.SetUpdated(id, f.now) },
	} {
		if err := set(); err != nil {
			f.t.Fatal(err)
		}
	}
}

// change finds a factory change of the result by its story.
func changeOf(t *testing.T, result application.FactoryReportResult, story string) application.FactoryChange {
	t.Helper()
	for _, c := range result.Changes {
		if c.Story == story {
			return c
		}
	}
	t.Fatalf("no factory change %s in %+v", story, result.Changes)
	return application.FactoryChange{}
}

func TestAnEscapedBugIsABugOfTheSameRigFiledWithinFourteenDaysAfterTheLandingThatNamesIt(t *testing.T) {
	f := newChangeFactory(t)
	f.landing("mw-e.1", "millwright", "opus", "Expect: none, because it is a test", "2026-10-05")
	// Escaped: named in the title, in the description, on the landing day, on the 14th day after.
	f.filed("e-title", "task", "[bug] the list is empty after mw-e.1 landed", "millwright", "2026-10-06T09:00:00Z", "")
	f.filed("e-desc", "bug", "Login fails", "millwright", "2026-10-19T23:00:00Z", "Since mw-e.1, the login fails.")
	f.filed("e-sameday", "bug", "Broken at once", "millwright", "2026-10-05T15:00:00Z", "It broke (mw-e.1).")
	// Not escaped: filed the 15th day after, filed before the landing day, on
	// another rig, naming a longer id, and not a bug.
	f.filed("x-late", "bug", "Late", "millwright", "2026-10-20T00:00:00Z", "after mw-e.1 landed")
	f.filed("x-before", "bug", "Before", "millwright", "2026-10-04T23:00:00Z", "after mw-e.1 landed")
	f.filed("x-rig", "bug", "Elsewhere", "lampas", "2026-10-07T09:00:00Z", "after mw-e.1 landed")
	f.filed("x-longer", "bug", "Another story", "millwright", "2026-10-07T09:00:00Z", "after mw-e.12 and mw-e.1x landed, not amw-e.1")
	f.filed("x-feature", "feature", "A wish", "millwright", "2026-10-07T09:00:00Z", "after mw-e.1 landed")

	result, printed := f.report(false)
	c := changeOf(t, result, "mw-e.1")
	if c.EscapedBugs != 3 || strings.Join(c.EscapedIDs, ",") != "e-desc,e-sameday,e-title" || c.EscapedTooSoon {
		t.Errorf("escaped bugs %d %v, too soon %t; want 3 e-desc,e-sameday,e-title and over", c.EscapedBugs, c.EscapedIDs, c.EscapedTooSoon)
	}
	if !strings.Contains(printed, "  escaped bugs 3 in the 14 days after\n") {
		t.Errorf("FACTORY CHANGES does not show the escaped bugs:\n%s", printed)
	}
}

func TestALandingsEscapedBugsAreTooSoonUntilItsFourteenDaysAreOver(t *testing.T) {
	f := newChangeFactory(t)
	f.landing("mw-e.2", "millwright", "opus", "Expect: refusals down", "2026-10-10")
	f.filed("e-1", "bug", "Broke", "millwright", "2026-10-12T09:00:00Z", "since mw-e.2")

	result, printed := f.report(false)
	c := changeOf(t, result, "mw-e.2")
	if c.EscapedBugs != 1 || !c.EscapedTooSoon || c.EscapedEnds != "2026-10-24" {
		t.Errorf("escaped %d, too soon %t, ends %s; want 1 so far, too soon until 2026-10-24", c.EscapedBugs, c.EscapedTooSoon, c.EscapedEnds)
	}
	if !strings.Contains(printed, "  escaped bugs 1 so far, too soon: 14 days end 2026-10-24\n") {
		t.Errorf("FACTORY CHANGES does not say too soon:\n%s", printed)
	}
}

func TestQualityCountsEscapedBugsPerRigAndPerModelOfTheLandingWithTheArrow(t *testing.T) {
	f := newChangeFactory(t)
	// The window is 2026-10-13 to 10-20, the window before 10-06 to 10-13.
	f.landing("mw-e.3", "millwright", "opus", "Expect: none, because it is a test", "2026-10-01")
	f.filed("e-before", "bug", "One", "millwright", "2026-10-08T09:00:00Z", "after mw-e.3 landed")
	f.filed("e-now-1", "bug", "Two", "millwright", "2026-10-14T09:00:00Z", "after mw-e.3 landed")
	f.filed("e-now-2", "bug", "Three", "millwright", "2026-10-15T09:00:00Z", "since mw-e.3")

	result, printed := f.report(false)
	for _, blocks := range []struct {
		name   string
		blocks []application.ReportBlock
		key    string
	}{{"rig", result.Rigs, "millwright"}, {"model", result.Models, "opus"}} {
		var found *application.ReportBlock
		for i := range blocks.blocks {
			if blocks.blocks[i].Name == blocks.key {
				found = &blocks.blocks[i]
			}
		}
		if found == nil {
			t.Fatalf("no %s block %s in %+v", blocks.name, blocks.key, blocks.blocks)
		}
		if found.Quality.EscapedBugs != 2 || found.Previous.EscapedBugs != 1 || found.Arrows["escaped_bugs"].Verdict != application.VerdictWorse {
			t.Errorf("%s %s: escaped %d, before %d, arrow %+v; want 2, 1, worse", blocks.name, blocks.key,
				found.Quality.EscapedBugs, found.Previous.EscapedBugs, found.Arrows["escaped_bugs"])
		}
	}
	if !strings.Contains(printed, "  escaped bugs 2 (Tester 0) ↑ worse\n") {
		t.Errorf("QUALITY does not show the escaped bugs:\n%s", printed)
	}
	if summary := result.Summary(); !strings.Contains(summary, "  escaped bugs 2 (Tester 0) ↑ worse\n") {
		t.Errorf("the summary does not show the escaped bugs:\n%s", summary)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"escaped_bugs":2`, `"escaped_bugs":1`, `"escaped_ids":["e-before","e-now-1","e-now-2"]`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the JSON does not carry %s:\n%s", want, raw)
		}
	}
}

func TestEscapedBugsIsAnExpectMetricJudgedTheDaysBeforeAgainstTheDaysAfter(t *testing.T) {
	got, err := domain.ParseExpect("1. It works.\n2. Expect: escaped bugs down, because the Tester looks harder")
	if err != nil || got.Metric != "escaped bugs" || got.Direction != "down" {
		t.Fatalf("got %+v, %v; want escaped bugs down", got, err)
	}

	f := newChangeFactory(t)
	f.landing("l-0", "lampas", "sonnet", "", "2026-09-22")
	f.filed("e-1", "bug", "One", "lampas", "2026-09-26T09:00:00Z", "after l-0 landed")
	f.filed("e-2", "bug", "Two", "lampas", "2026-09-27T09:00:00Z", "after l-0 landed")
	f.rows("2026-10-03", "lampas", 1, 0)
	f.landing("mw-e.4", "millwright", "opus", "Expect: escaped bugs down", "2026-10-01")

	result, printed := f.report(false)
	c := changeOf(t, result, "mw-e.4")
	if c.Verdict != application.ChangeKept || c.Before == nil || *c.Before != 2 || c.After == nil || *c.After != 0 {
		t.Errorf("change %+v; want 2 → 0, KEPT", c)
	}
	if !strings.Contains(printed, "2 → 0 (-100%): KEPT") {
		t.Errorf("FACTORY CHANGES does not show the move:\n%s", printed)
	}
}

func TestQualitySplitsEscapedBugsByFinderAndMODELSAndTheBoundReadOnlyTheOthers(t *testing.T) {
	f := newChangeFactory(t)
	f.landing("mw-e.4", "millwright", "opus", "Expect: none, because it is a test", "2026-10-13")
	f.filed("e-tester", "bug", "Tester says it broke", "millwright", "2026-10-14T09:00:00Z",
		"after mw-e.4 landed.\n\n"+application.TesterFindingMarker+". Source: Tester on mw-e.4 (haiku).")
	f.filed("e-other", "bug", "He says it broke", "millwright", "2026-10-15T09:00:00Z", "after mw-e.4 landed")

	result, printed := f.report(false)
	for _, block := range append(append([]application.ReportBlock{}, result.Rigs...), result.Models...) {
		if block.Name != "millwright" && block.Name != "opus" {
			continue
		}
		if block.Quality.EscapedBugs != 2 || block.Quality.EscapedTester != 1 {
			t.Errorf("%s: escaped %d (Tester %d), want 2 (Tester 1)", block.Name, block.Quality.EscapedBugs, block.Quality.EscapedTester)
		}
	}
	if !strings.Contains(printed, "  escaped bugs 2 (Tester 1)") {
		t.Errorf("QUALITY does not split the escaped bugs:\n%s", printed)
	}
	if summary := result.Summary(); !strings.Contains(summary, "escaped bugs 2 (Tester 1)") {
		t.Errorf("the summary does not split the escaped bugs:\n%s", summary)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"escaped_bugs":2,"escaped_bugs_tester":1`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the JSON does not carry %s:\n%s", want, raw)
		}
	}
	if !strings.Contains(printed, "escaped/landing (not Tester)") {
		t.Errorf("MODELS does not say its escapes leave the Tester's out:\n%s", printed)
	}
	// The one landing ran on sonnet (no worker on its ledger row); one escape
	// of the two is not the Tester's.
	var landed application.ModelUse
	for _, m := range result.ModelCompare.Models {
		if m.Landed == 1 {
			landed = m
		}
	}
	if landed.EscapedBugs != 1 || landed.EscapedPerLanding != 1 {
		t.Errorf("MODELS' landed model is %+v, want 1 escaped bug and 1.00 a landing", landed)
	}
}
