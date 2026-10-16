package application_test

import (
	"bytes"
	"context"
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
			if err := job.Run(ctx); err != nil {
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
