package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// digestFixture is a tracker holding the parent mw-6ww.119 and whatever demos
// a test adds under the epic mw-e, and a sender that keeps every card.
type digestFixture struct {
	tracker *apptest.FakeTracker
	sender  *apptest.FakePosternSender
	out     bytes.Buffer
}

func newDigestFixture() *digestFixture {
	f := &digestFixture{tracker: apptest.NewFakeTracker(), sender: &apptest.FakePosternSender{}}
	f.tracker.AddEpic("mw-6ww.119", domain.Path{})
	f.tracker.AddEpic("mw-e", domain.Path{})
	return f
}

// demo adds an open, unclaimed demo labelled as given, with comments.
func (f *digestFixture) demo(t *testing.T, id, title string, labels []string, comments ...string) {
	t.Helper()
	f.tracker.AddStory("mw-e", domain.Story{ID: id, Title: title})
	mustDo(t, f.tracker.SetLabels(id, labels...))
	for _, c := range comments {
		mustDo(t, f.tracker.CommentOnStory(context.Background(), id, c))
	}
}

func (f *digestFixture) run(t *testing.T, dryRun bool) application.DigestReport {
	t.Helper()
	report, err := application.Digest{
		Tracker: f.tracker,
		Sender:  f.sender,
		Parent:  "mw-6ww.119",
		DryRun:  dryRun,
		Now:     func() time.Time { return time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC) },
		Out:     &f.out,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("mw digest send: %v", err)
	}
	return report
}

func (f *digestFixture) threeDemos(t *testing.T) {
	t.Helper()
	long := "VERIFIED by the Mayor: " + strings.Repeat("x", 200)
	f.demo(t, "mw-e.1", "Demo: typed rig memory", []string{application.LabelDemo, application.LabelDigest},
		"landed", "VERIFIED by the Mayor: mw memory query finds the typed facts\nsecond line")
	f.demo(t, "mw-e.2", "Demo: self-adjusting caps", []string{application.LabelDemo, application.LabelDigest}, long)
	f.demo(t, "mw-e.3", "Demo: Vultr boosts", []string{application.LabelDemo, application.LabelDigest}, "landed, not checked yet")
}

func TestDigestSendFilesOneBeadAndOneCardForTheVerifiedDemos(t *testing.T) {
	f := newDigestFixture()
	f.threeDemos(t)

	report := f.run(t, false)

	created, err := f.tracker.ShowStory(context.Background(), report.Bead)
	if err != nil {
		t.Fatalf("the digest bead %q cannot be read: %v", report.Bead, err)
	}
	if created.EpicID != "mw-6ww.119" || created.Status != application.StatusOpen {
		t.Fatalf("the digest bead is under %q and %q, want under mw-6ww.119 and open", created.EpicID, created.Status)
	}
	if want := "Weekly digest 2026-10-19: 2 factory demos"; created.Story.Title != want {
		t.Fatalf("the digest bead is titled %q, want %q", created.Story.Title, want)
	}
	for _, want := range []string{"mw-e.1", "Demo: typed rig memory", "VERIFIED by the Mayor: mw memory query finds the typed facts", "mw-e.2"} {
		if !strings.Contains(created.Description, want) {
			t.Fatalf("the digest bead's description does not hold %q:\n%s", want, created.Description)
		}
	}
	if strings.Contains(created.Description, "second line") || strings.Contains(created.Description, strings.Repeat("x", 120)) {
		t.Fatalf("the description carries more than the VERIFIED line's first 120 characters:\n%s", created.Description)
	}
	if strings.Contains(created.Description, "mw-e.3") {
		t.Fatalf("the unverified demo is in the digest bead:\n%s", created.Description)
	}

	sent := f.sender.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d cards, want one", len(sent))
	}
	card := sent[0]
	if card.Class != "decision-needed" || card.Bead != report.Bead {
		t.Fatalf("the card is %q on %q, want decision-needed on %q", card.Class, card.Bead, report.Bead)
	}
	wantOptions := []string{"A: Looks good on all 2", "B: Hold, I'll name which"}
	if strings.Join(card.Options, "|") != strings.Join(wantOptions, "|") || card.Recommend != wantOptions[0] {
		t.Fatalf("the card offers %q recommending %q, want %q recommending %q", card.Options, card.Recommend, wantOptions, wantOptions[0])
	}
	for _, id := range []string{"mw-e.1", "mw-e.2"} {
		if !strings.Contains(card.Text, id) {
			t.Fatalf("the card does not name %s:\n%s", id, card.Text)
		}
	}
	if strings.Contains(card.Text, "mw-e.3") {
		t.Fatalf("the card names the unverified demo:\n%s", card.Text)
	}
	if !strings.Contains(f.out.String(), "mw-e.3") || !strings.Contains(f.out.String(), "not verified, left out") {
		t.Fatalf("the output does not say mw-e.3 was not verified, left out:\n%s", f.out.String())
	}
}

func TestDigestSendWithNothingLabelledSendsNothing(t *testing.T) {
	f := newDigestFixture()
	f.demo(t, "mw-e.1", "Demo: typed rig memory", []string{application.LabelDemo}, "VERIFIED by the Mayor")
	writes := f.tracker.Writes()

	report := f.run(t, false)

	if report.Bead != "" || f.tracker.Writes() != writes {
		t.Fatalf("filed %q, %d writes, with nothing labelled digest", report.Bead, f.tracker.Writes()-writes)
	}
	if len(f.sender.Sent()) != 0 {
		t.Fatalf("sent %d cards with nothing labelled digest", len(f.sender.Sent()))
	}
	if !strings.Contains(f.out.String(), "nothing to digest") {
		t.Fatalf("the output does not say nothing to digest:\n%s", f.out.String())
	}
}

func TestDigestSendDryRunCreatesAndSendsNothing(t *testing.T) {
	f := newDigestFixture()
	f.threeDemos(t)
	writes := f.tracker.Writes()

	report := f.run(t, true)

	if report.Bead != "" || f.tracker.Writes() != writes {
		t.Fatalf("a dry run filed %q, %d writes", report.Bead, f.tracker.Writes()-writes)
	}
	if len(f.sender.Sent()) != 0 {
		t.Fatalf("a dry run sent %d cards", len(f.sender.Sent()))
	}
	for _, want := range []string{"mw-e.1", "mw-e.2", "A: Looks good on all 2", "not verified, left out"} {
		if !strings.Contains(f.out.String(), want) {
			t.Fatalf("the dry run's output does not hold %q:\n%s", want, f.out.String())
		}
	}
}

// weeklyDigest is the digest job over a fake tracker's notes, at the given
// time, counting the sends it makes.
func weeklyDigest(notes *apptest.FakeTracker, now *time.Time, home bool, sends *int) *application.DigestWeekly {
	return &application.DigestWeekly{
		Notes: notes,
		Home:  func(context.Context) (bool, error) { return home, nil },
		Send:  func(context.Context) error { *sends++; return nil },
		Now:   func() time.Time { return *now },
	}
}

func TestDigestJobIsDueMondayAtElevenUTCAndNotBefore(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name string
		at   time.Time
		due  bool
	}{
		{"Monday 10:59Z", time.Date(2026, 10, 19, 10, 59, 0, 0, time.UTC), false},
		{"Monday 11:00Z", time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC), true},
		{"Monday 23:00Z", time.Date(2026, 10, 19, 23, 0, 0, 0, time.UTC), true},
		{"Tuesday 11:00Z", time.Date(2026, 10, 20, 11, 0, 0, 0, time.UTC), false},
		{"Sunday 11:00Z", time.Date(2026, 10, 18, 11, 0, 0, 0, time.UTC), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			sends, at := 0, c.at
			job := weeklyDigest(apptest.NewFakeTracker(), &at, true, &sends).Job()
			if _, due := job.Probe(ctx); due != c.due {
				t.Fatalf("at %s the digest job is due = %v, want %v", c.name, due, c.due)
			}
			if err := madeOrPassedOver(job.Run(ctx)); err != nil {
				t.Fatalf("running the job: %v", err)
			}
			if (sends == 1) != c.due {
				t.Fatalf("at %s the job sent %d times, want sent = %v", c.name, sends, c.due)
			}
		})
	}
}

func TestDigestJobSendsOncePerMondayAndOnlyOnTheHome(t *testing.T) {
	ctx := context.Background()
	notes := apptest.NewFakeTracker()
	at := time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC)

	away := 0
	if _, due := weeklyDigest(notes, &at, false, &away).Job().Probe(ctx); due {
		t.Fatal("expected the digest job not due away from the home")
	}

	sends := 0
	job := weeklyDigest(notes, &at, true, &sends).Job()
	if err := job.Run(ctx); err != nil {
		t.Fatalf("running the job: %v", err)
	}
	at = at.Add(3 * time.Hour)
	if _, due := job.Probe(ctx); due {
		t.Fatal("expected the job not due again the same Monday")
	}
	// A follower that restarted the same Monday reads the day from the notes.
	again := 0
	if _, due := weeklyDigest(notes, &at, true, &again).Job().Probe(ctx); due {
		t.Fatal("expected a restarted follower not to send the same Monday again")
	}
	at = at.AddDate(0, 0, 7)
	if _, due := weeklyDigest(notes, &at, true, &again).Job().Probe(ctx); !due {
		t.Fatal("expected the job due the next Monday")
	}
	if sends != 1 {
		t.Fatalf("sent %d times, want 1", sends)
	}
}

// fakeLoop is the Factory loop section's source: whatever the test lays out.
type fakeLoop struct {
	in    application.LoopInputs
	err   error
	reads int
}

func (l *fakeLoop) Gather(context.Context) (application.LoopInputs, error) {
	l.reads++
	return l.in, l.err
}

// loopWeek is a week with two picks, one KEPT verdict, one outcome and a share
// of 33% with a bound, all dated inside the window 2026-10-12 to 2026-10-19,
// beside a pick, a verdict and an outcome dated outside it.
func loopWeek() application.LoopInputs {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 9, 0, 0, 0, time.UTC) }
	pick := func(id, title, acceptance string, created time.Time) application.StoryDetail {
		return application.StoryDetail{Story: domain.Story{ID: id, Title: title}, Labels: []string{application.LabelInvestment},
			Acceptance: acceptance, Created: created}
	}
	return application.LoopInputs{
		Result: application.FactoryReportResult{
			Window: application.ReportWindow{Since: day(12), Until: day(19)},
			Changes: []application.FactoryChange{
				{Story: "mw-old.1", Title: "Shorter formula", Verdict: application.ChangeKept, AfterEnds: "2026-10-15"},
				{Story: "mw-old.2", Title: "Earlier change", Verdict: application.ChangeWorse, AfterEnds: "2026-10-05"},
				{Story: "mw-old.3", Title: "Too soon to say", Verdict: application.ChangeNoData, AfterEnds: "2026-10-16"},
			},
			InvestShare: &application.InvestShare{Invested: 3, Landed: 9, Share: 33.3, Target: 33, Min: 10, Max: 33,
				Bound: application.BoundMax, Reason: "quality falling: refusals 10% -> 20%"},
		},
		Investments: []application.Investment{
			{Made: application.InvestmentRecord{Kind: application.RecordInvestment, Bead: "mw-inv.1", At: day(1)},
				Outcomes: []application.InvestmentRecord{{Kind: application.RecordOutcome, Bead: "mw-inv.1", At: day(14),
					Result: application.ResultWin, Lesson: "the shorter formula saved a third of the attempts " + strings.Repeat("y", 100)}}},
			{Made: application.InvestmentRecord{Kind: application.RecordInvestment, Bead: "mw-inv.2", At: day(1)},
				Outcomes: []application.InvestmentRecord{{Kind: application.RecordOutcome, Bead: "mw-inv.2", At: day(5),
					Result: application.ResultLoss, Lesson: "outside the window"}}},
		},
		Picks: []application.StoryDetail{
			pick("mw-pick.1", "Faster gate", "1. It is faster.\nExpect: dispatch-to-landing down", day(13)),
			pick("mw-pick.2", "Fewer refusals", "1. Fewer.\n2. Expect: refusals down", day(15)),
			pick("mw-pick.3", "Last month's pick", "Expect: tokens/landing down", time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)),
		},
		Now: day(19),
	}
}

func (f *digestFixture) runLoop(t *testing.T, dryRun bool, loop application.LoopSource) application.DigestReport {
	t.Helper()
	report, err := application.Digest{
		Tracker: f.tracker, Sender: f.sender, Parent: "mw-6ww.119", DryRun: dryRun, Loop: loop,
		Now: func() time.Time { return time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC) },
		Out: &f.out,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("mw digest send: %v", err)
	}
	return report
}

func TestDigestCardCarriesTheFactoryLoopSectionAfterTheDemos(t *testing.T) {
	f := newDigestFixture()
	f.threeDemos(t)

	f.runLoop(t, false, &fakeLoop{in: loopWeek()})

	sent := f.sender.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d cards, want one", len(sent))
	}
	text := sent[0].Text
	// Each in its place, in this order, full ids present.
	order := []string{"mw-e.2", "Factory loop", "PICKS", "mw-pick.1", "Faster gate", "Expect: dispatch-to-landing down",
		"mw-pick.2", "Expect: refusals down", "VERDICTS", "KEPT", "Shorter formula", "mw-old.1", "mw-inv.1", "win",
		"the shorter formula saved a third of the attempts", "SHARE", "INVESTMENTS 3/9 (33%)", "bound: up to 33% allowed (quality falling"}
	at := 0
	for _, want := range order {
		i := strings.Index(text[at:], want)
		if i < 0 {
			t.Fatalf("the card does not hold %q after position %d:\n%s", want, at, text)
		}
		at += i + len(want)
	}
	for _, not := range []string{"mw-pick.3", "mw-old.2", "mw-old.3", "mw-inv.2", strings.Repeat("y", 100)} {
		if strings.Contains(text, not) {
			t.Fatalf("the card holds %q, which is not this week's:\n%s", not, text)
		}
	}
	if strings.Contains(text, "moved to") {
		t.Fatalf("the card says the share moved with no digest before it:\n%s", text)
	}
}

func TestDigestCardKeepsItsOptionTexts(t *testing.T) {
	f := newDigestFixture()
	f.threeDemos(t)

	f.runLoop(t, false, &fakeLoop{in: loopWeek()})

	card := f.sender.Sent()[0]
	if got, want := strings.Join(card.Options, "|"), "A: Looks good on all 2|B: Hold, I'll name which"; got != want {
		t.Fatalf("the card offers %q, want %q", got, want)
	}
	if card.Recommend != "A: Looks good on all 2" {
		t.Fatalf("the card recommends %q", card.Recommend)
	}
}

func TestDigestWithNoDemosButAPickStillGoesOut(t *testing.T) {
	f := newDigestFixture()
	in := loopWeek()
	in.Picks = in.Picks[:1]
	in.Result.Changes, in.Investments = nil, nil

	report := f.runLoop(t, false, &fakeLoop{in: in})

	sent := f.sender.Sent()
	if report.Bead == "" || len(sent) != 1 {
		t.Fatalf("filed %q and sent %d cards, want the card out", report.Bead, len(sent))
	}
	for _, want := range []string{"no factory demos this week", "Factory loop", "mw-pick.1"} {
		if !strings.Contains(sent[0].Text, want) {
			t.Fatalf("the card does not hold %q:\n%s", want, sent[0].Text)
		}
	}
	if strings.Contains(sent[0].Text, "mw-pick.2") {
		t.Fatalf("the card holds the pick left out:\n%s", sent[0].Text)
	}
}

func TestDigestWithNoDemosAndNoLoopContentSendsNothing(t *testing.T) {
	f := newDigestFixture()
	in := loopWeek()
	in.Picks, in.Investments = nil, nil
	in.Result.Changes = []application.FactoryChange{{Story: "mw-old.3", Title: "Too soon", Verdict: application.ChangeNoData, AfterEnds: "2026-10-16"}}
	writes := f.tracker.Writes()

	report := f.runLoop(t, false, &fakeLoop{in: in})

	if report.Bead != "" || f.tracker.Writes() != writes || len(f.sender.Sent()) != 0 {
		t.Fatalf("filed %q, %d writes, %d cards, with nothing to digest", report.Bead, f.tracker.Writes()-writes, len(f.sender.Sent()))
	}
	if !strings.Contains(f.out.String(), "nothing to digest") {
		t.Fatalf("the output does not say nothing to digest:\n%s", f.out.String())
	}
}

func TestDigestSaysTheShareMovedSinceThePreviousDigestBead(t *testing.T) {
	f := newDigestFixture()
	f.threeDemos(t)
	f.tracker.AddStory("mw-6ww.119", domain.Story{ID: "mw-6ww.119.1", Title: "Weekly digest 2026-10-12: 2 factory demos"})
	description := "- mw-x.1: Demo\n  VERIFIED\ninvest_share: 25%\n"
	mustDo(t, f.tracker.EditStory(context.Background(), "mw-6ww.119.1", application.StoryEdit{Description: &description}))
	mustDo(t, f.tracker.SetCreated("mw-6ww.119.1", time.Date(2026, 10, 12, 11, 0, 0, 0, time.UTC)))

	report := f.runLoop(t, false, &fakeLoop{in: loopWeek()})

	if text := f.sender.Sent()[0].Text; !strings.Contains(text, "moved to 33% on 2026-10-19") {
		t.Fatalf("the SHARE line does not say the share moved to 33%%:\n%s", text)
	}
	created, err := f.tracker.ShowStory(context.Background(), report.Bead)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(created.Description, "invest_share: 33%") {
		t.Fatalf("the digest bead does not record the share it saw:\n%s", created.Description)
	}
	// The batch-close path reads the demos off the lines that begin "- ":
	// the share line must not be one of them.
	for _, line := range strings.Split(created.Description, "\n") {
		if strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "- mw-e.1:") && !strings.HasPrefix(line, "- mw-e.2:") {
			t.Fatalf("the description has a listed line that is no demo: %q", line)
		}
	}
}

func TestDigestSaysNothingOfAMoveWhenTheShareIsTheSame(t *testing.T) {
	f := newDigestFixture()
	f.threeDemos(t)
	f.tracker.AddStory("mw-6ww.119", domain.Story{ID: "mw-6ww.119.1", Title: "Weekly digest 2026-10-12: 2 factory demos"})
	description := "invest_share: 33%\n"
	mustDo(t, f.tracker.EditStory(context.Background(), "mw-6ww.119.1", application.StoryEdit{Description: &description}))

	f.runLoop(t, false, &fakeLoop{in: loopWeek()})

	if text := f.sender.Sent()[0].Text; strings.Contains(text, "moved to") {
		t.Fatalf("the card says the share moved when it did not:\n%s", text)
	}
}

func TestDigestDryRunPrintsTheWholeCardAndCreatesAndSendsNothing(t *testing.T) {
	f := newDigestFixture()
	f.threeDemos(t)
	writes := f.tracker.Writes()

	report := f.runLoop(t, true, &fakeLoop{in: loopWeek()})

	if report.Bead != "" || f.tracker.Writes() != writes || len(f.sender.Sent()) != 0 {
		t.Fatalf("a dry run filed %q, %d writes, %d cards", report.Bead, f.tracker.Writes()-writes, len(f.sender.Sent()))
	}
	for _, want := range []string{"Factory loop", "mw-pick.1", "mw-old.1", "mw-inv.1", "SHARE", "A: Looks good on all 2"} {
		if !strings.Contains(f.out.String(), want) {
			t.Fatalf("the dry run does not print %q:\n%s", want, f.out.String())
		}
	}
}

func TestDigestGoesOutWithTheDemosWhenTheLoopCannotBeRead(t *testing.T) {
	f := newDigestFixture()
	f.threeDemos(t)

	f.runLoop(t, false, &fakeLoop{err: errors.New("the report would not run")})

	sent := f.sender.Sent()
	if len(sent) != 1 || strings.Contains(sent[0].Text, "Factory loop") {
		t.Fatalf("sent %d cards; want the demos' card without the section", len(sent))
	}
	if !strings.Contains(f.out.String(), "the report would not run") {
		t.Fatalf("the output does not say why the loop section is missing:\n%s", f.out.String())
	}
}

func TestDigestSaysTheShareMovedWhenTheParentIsATaskBead(t *testing.T) {
	f := newDigestFixture()
	f.threeDemos(t)
	// The default parent mw-6ww.119 is a task, not an epic.
	f.tracker.AddStory("mw-e", domain.Story{ID: "mw-task", Title: "Weekly digests"})
	f.tracker.AddStory("mw-task", domain.Story{ID: "mw-task.1", Title: "Weekly digest 2026-10-12: 2 factory demos"})
	description := "- mw-x.1: Demo\n  VERIFIED\ninvest_share: 20%\n"
	mustDo(t, f.tracker.EditStory(context.Background(), "mw-task.1", application.StoryEdit{Description: &description}))
	mustDo(t, f.tracker.SetCreated("mw-task.1", time.Date(2026, 10, 12, 11, 0, 0, 0, time.UTC)))

	if _, err := (application.Digest{
		Tracker: f.tracker, Sender: f.sender, Parent: "mw-task", Loop: &fakeLoop{in: loopWeek()},
		Now: func() time.Time { return time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC) },
		Out: &f.out,
	}.Run(context.Background())); err != nil {
		t.Fatalf("mw digest send: %v", err)
	}

	if text := f.sender.Sent()[0].Text; !strings.Contains(text, "moved to 33% on 2026-10-19") {
		t.Fatalf("the SHARE line does not say the share moved to 33%%:\n%s", text)
	}
	if strings.Contains(f.out.String(), "not known") {
		t.Fatalf("the output says the previous share is not known:\n%s", f.out.String())
	}
}

func TestDigestOptionWithNoDemosDoesNotCountThemAndWithSomeStillDoes(t *testing.T) {
	if got := application.DigestLooksGoodOption(0); strings.Contains(got, "all 0") || got != "A: Looks good" {
		t.Fatalf("the option for no demos is %q, want \"A: Looks good\"", got)
	}
	if got := application.DigestLooksGoodOption(2); got != "A: Looks good on all 2" {
		t.Fatalf("the option for two demos is %q", got)
	}
	f := newDigestFixture()
	f.runLoop(t, false, &fakeLoop{in: loopWeek()})
	if got := f.sender.Sent()[0].Options[0]; strings.Contains(got, "all 0") {
		t.Fatalf("the card with no demos offers %q", got)
	}
}
