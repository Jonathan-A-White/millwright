package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// changeFactory is a factory whose clock reads 2026-10-20, with the story
// mw-c.1 on the millwright rig landed on 2026-10-01: its week before is
// 2026-09-24 to 09-30 and its week after is 2026-10-02 to 10-08.
type changeFactory struct {
	t         *testing.T
	tracker   *apptest.FakeTracker
	files     *vault.Vault
	mailbox   *apptest.FakeMailbox
	snapshots *apptest.FakeMetricSnapshots
	now       time.Time
	n         int
}

func newChangeFactory(t *testing.T) *changeFactory {
	t.Helper()
	return &changeFactory{
		t: t, tracker: apptest.NewFakeTracker(), files: vault.New(t.TempDir()),
		mailbox: apptest.NewFakeMailbox(), snapshots: &apptest.FakeMetricSnapshots{},
		now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
	}
}

// story files a story of a rig, with acceptance criteria.
func (f *changeFactory) story(id, title, rig, acceptance string) {
	f.t.Helper()
	f.tracker.AddStory("", domain.Story{ID: id, Title: title, Overrides: domain.Path{Rig: rig, Host: "laptop", Model: "sonnet"}})
	if err := f.tracker.SetAcceptance(id, acceptance); err != nil {
		f.t.Fatal(err)
	}
	if err := f.tracker.SetUpdated(id, f.now); err != nil {
		f.t.Fatal(err)
	}
}

// rows puts landed and refused ledger rows of a rig on a day.
func (f *changeFactory) rows(day, rig string, landed, refused int) {
	f.t.Helper()
	at, err := time.Parse(application.LedgerDate, day)
	if err != nil {
		f.t.Fatal(err)
	}
	for i := 0; i < landed+refused; i++ {
		f.n++
		id := "row-" + rig + "-" + day + "-" + string(rune('a'+i))
		outcome := "landed on main"
		if i >= landed {
			outcome = application.NotLanded(application.Reason("tests-fail"), "red")
		}
		f.story(id, "Row "+id, rig, "")
		f.ledger(at, id, outcome)
	}
}

func (f *changeFactory) ledger(at time.Time, id, outcome string) {
	f.t.Helper()
	line := application.LedgerLine{When: at, StoryID: id, Title: "The story " + id, Outcome: outcome,
		Path: domain.Path{Model: "sonnet", Effort: "high"}}.String()
	if err := f.files.AppendToLedger(context.Background(), "builder", line); err != nil {
		f.t.Fatal(err)
	}
}

// change files the millwright story mw-c.1 and lands it on day.
func (f *changeFactory) change(acceptance, day string) {
	f.t.Helper()
	f.story("mw-c.1", "Make refusals rarer", "millwright", acceptance)
	at, err := time.Parse(application.LedgerDate, day)
	if err != nil {
		f.t.Fatal(err)
	}
	f.ledger(at, "mw-c.1", "landed on main")
}

func (f *changeFactory) report(withWrites bool) (application.FactoryReportResult, string) {
	f.t.Helper()
	var out strings.Builder
	report := application.FactoryReport{
		Tracker: f.tracker, Files: f.files, Seat: "builder", Benchmarks: &apptest.FakeBenchmarks{},
		Now: func() time.Time { return f.now }, Out: &out,
	}
	if withWrites {
		report.Notes, report.Mailbox, report.Snapshots = f.tracker, f.mailbox, f.snapshots
	}
	result, err := report.Run(context.Background())
	if err != nil {
		f.t.Fatalf("mw report failed: %v", err)
	}
	return result, out.String()
}

func TestParseExpectReadsTheMetricAndTheDirection(t *testing.T) {
	for text, want := range map[string]application.Expect{
		"1. It works.\n2. Expect: refusals down":                        {Metric: "refusals", Direction: "down"},
		"Expect: attempts/landing down, because the formula is shorter": {Metric: "attempts/landing", Direction: "down"},
		"- expect: $/landing UP":                                        {Metric: "$/landing", Direction: "up"},
		"3) Expect: dispatch-to-landing down.":                          {Metric: "dispatch-to-landing", Direction: "down"},
		"Expect: tokens/landing down":                                   {Metric: "tokens/landing", Direction: "down"},
		"Expect: bugs/landing down":                                     {Metric: "bugs/landing", Direction: "down"},
		"Expect: none, because this is a doc":                           {Metric: application.ExpectNone},
	} {
		got, err := application.ParseExpect(text)
		if err != nil || got.Metric != want.Metric || got.Direction != want.Direction {
			t.Errorf("%q: got %+v, %v; want %+v", text, got, err, want)
		}
	}
	if _, err := application.ParseExpect("1. It works\nSweep: none, because it is the tool"); err != application.ErrNoExpect {
		t.Errorf("acceptance with no Expect line: got %v, want ErrNoExpect", err)
	}
	for _, bad := range []string{"Expect: speed up", "Expect: refusals", "Expect: refusals sideways", "Expect:"} {
		if _, err := application.ParseExpect(bad); err == nil || err == application.ErrNoExpect {
			t.Errorf("%q was accepted or taken for no line: %v", bad, err)
		}
	}
}

func TestAChangeThatMovedItsMetricTheWrongWayPastTheBandIsWorse(t *testing.T) {
	f := newChangeFactory(t)
	f.rows("2026-09-27", "lampas", 4, 1) // before: 1 of 5 refused, 20%
	f.rows("2026-10-05", "lampas", 2, 2) // after: 2 of 4 refused, 50%
	f.change("1. It works.\n2. Expect: refusals down", "2026-10-01")

	result, printed := f.report(false)
	if len(result.Changes) != 1 || result.Changes[0].Verdict != application.ChangeWorse {
		t.Fatalf("changes %+v, want one WORSE", result.Changes)
	}
	for _, want := range []string{"FACTORY CHANGES", "10%", "Make refusals rarer (mw-c.1)", "Expect: refusals down", "20% → 50%", "WORSE"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the report does not say %q:\n%s", want, printed)
		}
	}
	for _, line := range strings.Split(printed, "\n") {
		if len([]rune(line)) > application.ReportWidth {
			t.Errorf("a line is wider than %d columns: %q", application.ReportWidth, line)
		}
	}
}

func TestAChangeIsBetterLevelOrWorseOnlyPastTheBand(t *testing.T) {
	for _, c := range []struct {
		name, acceptance string
		beforeL, beforeR int
		afterL, afterR   int
		verdict          string
	}{
		{"down and fell", "Expect: refusals down", 4, 1, 5, 0, application.ChangeBetter},
		{"down and rose", "Expect: refusals down", 4, 1, 3, 2, application.ChangeWorse},
		{"up and fell", "Expect: refusals up", 3, 2, 4, 1, application.ChangeWorse},
		{"up and rose", "Expect: refusals up", 4, 1, 3, 2, application.ChangeBetter},
		{"level", "Expect: refusals down", 4, 1, 4, 1, application.ChangeLevel},
		{"none is never worse", "Expect: none, because a doc", 4, 1, 1, 4, application.ChangeNone},
		{"no Expect named", "1. It works.", 4, 1, 1, 4, application.ChangeNoExpect},
		{"no landings after", "Expect: attempts/landing down", 4, 1, 0, 0, application.ChangeNoData},
	} {
		f := newChangeFactory(t)
		f.rows("2026-09-27", "lampas", c.beforeL, c.beforeR)
		f.rows("2026-10-05", "lampas", c.afterL, c.afterR)
		f.change(c.acceptance, "2026-10-01")
		result, _ := f.report(false)
		if len(result.Changes) != 1 || result.Changes[0].Verdict != c.verdict {
			t.Errorf("%s: changes %+v, want the verdict %q", c.name, result.Changes, c.verdict)
		}
	}
}

func TestTheWorseBandIsTenPercentOfTheWeekBefore(t *testing.T) {
	if application.ChangeBand != 0.10 {
		t.Fatalf("the band is %v, the story says 10%%", application.ChangeBand)
	}
	// Attempts per landing: 1.00 the week before; with ten landings after, one
	// that took two attempts is 1.10, on the band, and two is 1.20, past it.
	for slow, verdict := range map[int]string{1: application.ChangeLevel, 2: application.ChangeWorse} {
		f := newChangeFactory(t)
		f.rows("2026-09-27", "lampas", 10, 0)
		f.rows("2026-10-05", "lampas", 10, 0)
		for i := 0; i < slow; i++ {
			id := "row-lampas-2026-10-05-" + string(rune('a'+i))
			if err := f.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.AttemptsField: "2"}); err != nil {
				t.Fatal(err)
			}
		}
		f.change("Expect: attempts/landing down", "2026-10-01")
		result, printed := f.report(false)
		if len(result.Changes) != 1 || result.Changes[0].Verdict != verdict {
			t.Errorf("%d slow landings: changes %+v, want %q\n%s", slow, result.Changes, verdict, printed)
		}
	}
}

func TestAChangeLessThanAWeekOldIsTooSoon(t *testing.T) {
	f := newChangeFactory(t)
	f.rows("2026-10-15", "lampas", 4, 1)
	f.change("Expect: refusals down", "2026-10-17") // the week after runs to 10-24
	result, printed := f.report(false)
	if len(result.Changes) != 1 || result.Changes[0].Verdict != application.ChangeTooSoon {
		t.Fatalf("changes %+v, want one too soon", result.Changes)
	}
	if !strings.Contains(printed, "too soon") {
		t.Errorf("the report does not say too soon:\n%s", printed)
	}
}

func TestOnlyMillwrightLandingsOfTheLastThirtyDaysAreChanges(t *testing.T) {
	f := newChangeFactory(t)
	f.story("lampas-x", "A lampas feature", "lampas", "Expect: refusals down")
	f.ledger(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "lampas-x", "landed on main")
	f.story("mw-old", "Long ago", "millwright", "Expect: refusals down")
	f.ledger(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), "mw-old", "landed on main")
	f.story("mw-new", "In window", "millwright", "Expect: refusals down")
	f.ledger(time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC), "mw-new", "landed on main")
	result, _ := f.report(false)
	if len(result.Changes) != 1 || result.Changes[0].Story != "mw-new" {
		t.Errorf("changes %+v, want only mw-new", result.Changes)
	}
}

func TestAWorseChangeWritesOneEventToTheMayorOnce(t *testing.T) {
	f := newChangeFactory(t)
	f.rows("2026-09-27", "lampas", 4, 1)
	f.rows("2026-10-05", "lampas", 2, 2)
	f.change("Expect: refusals down", "2026-10-01")

	f.report(true)
	f.report(true)
	inbox, err := f.mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("the Mayor's inbox holds %d messages (%v), want 1", len(inbox), err)
	}
	for _, want := range []string{"Make refusals rarer", "mw-c.1", "refusals", "20%", "50%"} {
		if !strings.Contains(inbox[0].Subject+inbox[0].Body, want) {
			t.Errorf("the message does not say %q: %+v", want, inbox[0])
		}
	}
}

func TestAChangeThatIsNotWorseWritesNoEvent(t *testing.T) {
	f := newChangeFactory(t)
	f.rows("2026-09-27", "lampas", 4, 1)
	f.rows("2026-10-05", "lampas", 5, 0)
	f.change("Expect: refusals down", "2026-10-01")
	f.report(true)
	if inbox, _ := f.mailbox.Inbox(context.Background(), application.MayorMailbox); len(inbox) != 0 {
		t.Errorf("the Mayor was written to for a change that got better: %+v", inbox)
	}
}

func TestAReportWithNoWritersWritesNoEventAndNoSnapshot(t *testing.T) {
	f := newChangeFactory(t)
	f.rows("2026-09-27", "lampas", 4, 1)
	f.rows("2026-10-05", "lampas", 2, 2)
	f.change("Expect: refusals down", "2026-10-01")
	f.report(false)
	if inbox, _ := f.mailbox.Inbox(context.Background(), application.MayorMailbox); len(inbox) != 0 {
		t.Errorf("a plain report wrote to the Mayor: %+v", inbox)
	}
	if got := f.snapshots.All(); len(got) != 0 {
		t.Errorf("a plain report took a snapshot: %+v", got)
	}
}

func TestOneSnapshotADayOfTheKeyMetricsIsKept(t *testing.T) {
	f := newChangeFactory(t)
	f.rows("2026-10-16", "lampas", 3, 1) // inside the week to 2026-10-20
	f.rows("2026-10-01", "lampas", 9, 9) // outside it

	f.report(true)
	f.report(true)
	got := f.snapshots.All()
	if len(got) != 1 {
		t.Fatalf("two reports on one day left %d snapshots, want 1: %+v", len(got), got)
	}
	s := got[0]
	if s.Day != "2026-10-20" || s.Landed != 3 || s.Refused != 1 || s.RefusalRate != 25 {
		t.Errorf("snapshot %+v, want 2026-10-20 with 3 landed, 1 refused, 25%%", s)
	}

	f.now = f.now.AddDate(0, 0, 1)
	f.report(true)
	if got := f.snapshots.All(); len(got) != 2 || got[0].Day != "2026-10-20" || got[1].Day != "2026-10-21" {
		t.Errorf("the next day's report left %+v, want the two days in order", got)
	}
}
