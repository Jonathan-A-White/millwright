package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-6ww.185.10: a Boost that has not answered for 30 minutes has the home give
// its lapsed claims back, as mw retry --force does, and tell the Mayor once.

var boostOutageNow = time.Date(2026, time.November, 21, 10, 0, 0, 0, time.UTC)

// aBoostWorld is the home (laptop) with a desktop Boost that holds the stories
// the test adds, each claimed by mw@desktop with its lease lapsed an hour ago.
type aBoostWorld struct {
	tracker *apptest.FakeTracker
	landing *fakeRetryLanding
	mail    *apptest.FakeMailbox
	give    application.BoostGiveBack
}

func newBoostWorld(t *testing.T) *aBoostWorld {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	landing := &fakeRetryLanding{AheadCount: 2, OriginTip: "abc1234def5678"}
	var out strings.Builder
	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
	retry.Host = "laptop"
	retry.Notes = tracker
	retry.Now = func() time.Time { return boostOutageNow }
	mail := apptest.NewFakeMailbox()
	return &aBoostWorld{tracker: tracker, landing: landing, mail: mail, give: application.BoostGiveBack{
		Retry:   retry,
		Notes:   tracker,
		Mailbox: mail,
		Home:    "laptop",
		Now:     func() time.Time { return boostOutageNow },
	}}
}

// holds makes the desktop hold id, pathed to pathHost, its lease lapsed.
func (w *aBoostWorld) holds(t *testing.T, id, pathHost string) {
	t.Helper()
	aClaimedRetryStory(t, w.tracker, id, "mw@desktop", pathHost)
	if err := w.tracker.SetLeaseExpires(id, boostOutageNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func (w *aBoostWorld) story(t *testing.T, id string) application.StoryDetail {
	t.Helper()
	detail, err := w.tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return detail
}

func (w *aBoostWorld) mailed(t *testing.T) application.Message {
	t.Helper()
	inbox, err := w.mail.Inbox(context.Background(), application.MayorMailbox)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("expected one mail to the mayor, got %d (%v)", len(inbox), err)
	}
	return inbox[0]
}

func TestABoostThatDoesNotAnswerHasItsLapsedClaimGivenBackAsRetryForceDoes(t *testing.T) {
	w := newBoostWorld(t)
	w.holds(t, "mw-gq6.1", "desktop")

	report, err := w.give.Run(context.Background(), "desktop")
	if err != nil {
		t.Fatalf("expected the give-back to go through, got: %v", err)
	}
	if len(report.Gave) != 1 || report.Gave[0].StoryID != "mw-gq6.1" {
		t.Fatalf("expected mw-gq6.1 given back, got %+v", report)
	}
	detail := w.story(t, "mw-gq6.1")
	if detail.Status != application.StatusOpen || detail.Assignee != "" {
		t.Errorf("expected the story open and unclaimed, got %q held by %q", detail.Status, detail.Assignee)
	}
	if got := detail.Merged().Host; got != "auto" {
		t.Errorf("expected a story pinned to the desktop with no host_why set to host=auto, got host=%q", got)
	}
	// The cause is host, and an attempt named so is not counted against the story.
	if got := detail.AttemptCauses[1]; got != application.CauseHost {
		t.Errorf("expected attempt 1 named %q, got %q (%v)", application.CauseHost, got, detail.AttemptCauses)
	}
	if own, uncounted := application.OwnAttempts(detail); own != 0 || len(uncounted) != 1 {
		t.Errorf("expected the attempt not counted, got own=%d uncounted=%v", own, uncounted)
	}
	// The pushed branch is not touched: nothing deleted from origin, and the story
	// comment says what the retry did.
	if said := strings.Join(w.tracker.Comments("mw-gq6.1"), "\n"); !strings.Contains(said, "desktop") || !strings.Contains(said, "30 min") {
		t.Errorf("expected a comment naming the desktop and the 30 minutes, got %q", said)
	}
	if w.landing.removed || w.landing.branchDeleted {
		t.Errorf("expected nothing on the home's own checkout removed")
	}
}

func TestAGiveBackOfAStoryPinnedToAnotherHostLeavesItsPathAlone(t *testing.T) {
	w := newBoostWorld(t)
	w.holds(t, "mw-gq6.1", "vps")

	if _, err := w.give.Run(context.Background(), "desktop"); err != nil {
		t.Fatal(err)
	}
	if got := w.story(t, "mw-gq6.1").Merged().Host; got != "vps" {
		t.Errorf("expected the host key left as vps, got %q", got)
	}
}

func TestAClaimWhoseLeaseHasNotLapsedIsLeftAlone(t *testing.T) {
	w := newBoostWorld(t)
	w.holds(t, "mw-gq6.1", "desktop")
	if err := w.tracker.SetLeaseExpires("mw-gq6.1", boostOutageNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	report, err := w.give.Run(context.Background(), "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Gave) != 0 || w.story(t, "mw-gq6.1").Assignee != "mw@desktop" {
		t.Errorf("expected a live lease left alone, got %+v", report)
	}
}

func TestAQueuedStoryAndALandedOneAreLeftAloneAndAPinnedOneWithAHostWhyIsNamedInTheMail(t *testing.T) {
	w := newBoostWorld(t)
	w.holds(t, "mw-gq6.1", "desktop")
	w.holds(t, "mw-gq6.2", "desktop")
	w.holds(t, "mw-gq6.3", "desktop")
	w.holds(t, "mw-gq6.4", "desktop")
	ctx := context.Background()
	if err := w.tracker.AddLabel(ctx, "mw-gq6.2", "run:queued"); err != nil {
		t.Fatal(err)
	}
	if err := w.tracker.AddLabel(ctx, "mw-gq6.3", "run:landed"); err != nil {
		t.Fatal(err)
	}
	if err := w.tracker.SetStoryMetadata(ctx, "mw-gq6.4", map[string]string{"host_why": "needs the age key"}); err != nil {
		t.Fatal(err)
	}

	report, err := w.give.Run(ctx, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Gave) != 1 || report.Gave[0].StoryID != "mw-gq6.1" {
		t.Errorf("expected only mw-gq6.1 given back, got %+v", report.Gave)
	}
	for _, id := range []string{"mw-gq6.2", "mw-gq6.3", "mw-gq6.4"} {
		detail := w.story(t, id)
		if detail.Assignee != "mw@desktop" || detail.Status != application.StatusInProgress {
			t.Errorf("expected %s left claimed by the desktop, got %q held by %q", id, detail.Status, detail.Assignee)
		}
		if len(w.tracker.Comments(id)) != 0 {
			t.Errorf("expected nothing written on %s, got %v", id, w.tracker.Comments(id))
		}
	}
	if got := w.story(t, "mw-gq6.4").Merged().Host; got != "desktop" {
		t.Errorf("expected the pin with a host_why kept, got host=%q", got)
	}
	mail := w.mailed(t)
	said := mail.Subject + "\n" + mail.Body
	for _, want := range []string{"mw-gq6.1", "mw-gq6.2", "queued", "mw-gq6.3", "landed", "mw-gq6.4", "needs the age key"} {
		if !strings.Contains(said, want) {
			t.Errorf("expected the mail to say %q, got %q", want, said)
		}
	}
}

func TestTheMailNamesWhatWasKeptAndWhereTheStoryMayGoNow(t *testing.T) {
	w := newBoostWorld(t)
	w.holds(t, "mw-gq6.1", "desktop")
	w.holds(t, "mw-gq6.2", "desktop")

	if _, err := w.give.Run(context.Background(), "desktop"); err != nil {
		t.Fatal(err)
	}
	mail := w.mailed(t)
	said := mail.Subject + "\n" + mail.Body
	for _, want := range []string{"desktop", "30 min", "mw-gq6.1", "mw-gq6.2", "mw/mw-gq6.1", "abc1234", "host=auto", "any host"} {
		if !strings.Contains(said, want) {
			t.Errorf("expected the mail to say %q, got %q", want, said)
		}
	}
	if w.mail.Writes() != 1 {
		t.Errorf("expected one mail for the outage, got %d", w.mail.Writes())
	}
}

func TestTheMailSaysNothingWasPushedWhenTheRemoteHasNoBranch(t *testing.T) {
	w := newBoostWorld(t)
	w.landing.OriginTip = ""
	w.holds(t, "mw-gq6.1", "desktop")

	if _, err := w.give.Run(context.Background(), "desktop"); err != nil {
		t.Fatal(err)
	}
	if said := w.mailed(t).Body; !strings.Contains(said, "nothing was pushed") {
		t.Errorf("expected the mail to say nothing was pushed, got %q", said)
	}
}

func TestNoMailIsSentWhenTheBoostHoldsNoClaim(t *testing.T) {
	w := newBoostWorld(t)

	report, err := w.give.Run(context.Background(), "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Gave) != 0 || w.mail.Writes() != 0 {
		t.Errorf("expected nothing given back and no mail, got %+v with %d mails", report, w.mail.Writes())
	}
}

func TestAHostThatGivesWayIsNotGivenBack(t *testing.T) {
	w := newBoostWorld(t)
	w.holds(t, "mw-gq6.1", "desktop")
	note := application.GiveWayNote{Since: boostOutageNow.Add(-time.Hour), By: "j", Seen: boostOutageNow.Add(-time.Minute)}
	if err := w.tracker.SetNote(context.Background(), application.GiveWayKey("desktop"), note.String()); err != nil {
		t.Fatal(err)
	}

	report, err := w.give.Run(context.Background(), "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if report.Skipped == "" || !strings.Contains(report.Skipped, "gives way") {
		t.Errorf("expected the give-back skipped because the host gives way, got %q", report.Skipped)
	}
	if w.story(t, "mw-gq6.1").Assignee != "mw@desktop" || w.mail.Writes() != 0 {
		t.Errorf("expected the claim kept and no mail")
	}
}

func TestAStaleGiveWayNoteIsNotBelieved(t *testing.T) {
	w := newBoostWorld(t)
	w.holds(t, "mw-gq6.1", "desktop")
	note := application.GiveWayNote{Since: boostOutageNow.Add(-72 * time.Hour), By: "j", Seen: boostOutageNow.Add(-72 * time.Hour)}
	if err := w.tracker.SetNote(context.Background(), application.GiveWayKey("desktop"), note.String()); err != nil {
		t.Fatal(err)
	}

	report, err := w.give.Run(context.Background(), "desktop")
	if err != nil || len(report.Gave) != 1 {
		t.Fatalf("expected a stale note not believed and the claim given back, got %+v, %v", report, err)
	}
}

func TestAPausedHostIsNotGivenBack(t *testing.T) {
	w := newBoostWorld(t)
	w.holds(t, "mw-gq6.1", "desktop")
	paused := `{"seq":7,"at":"2026-11-21T09:00:00Z","actor":"j"}`
	if err := w.tracker.SetNote(context.Background(), application.PausedKey("desktop"), paused); err != nil {
		t.Fatal(err)
	}

	report, err := w.give.Run(context.Background(), "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Skipped, "paused") {
		t.Errorf("expected the give-back skipped because the host is paused, got %q", report.Skipped)
	}
	if w.story(t, "mw-gq6.1").Assignee != "mw@desktop" || w.mail.Writes() != 0 {
		t.Errorf("expected the claim kept and no mail")
	}
}

func TestTheHomeIsNeverGivenBack(t *testing.T) {
	w := newBoostWorld(t)
	aClaimedRetryStory(t, w.tracker, "mw-gq6.1", "mw@laptop", "laptop")
	if err := w.tracker.SetLeaseExpires("mw-gq6.1", boostOutageNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	report, err := w.give.Run(context.Background(), "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Skipped, "home") {
		t.Errorf("expected the give-back skipped because this is the home, got %q", report.Skipped)
	}
	if w.story(t, "mw-gq6.1").Assignee != "mw@laptop" || w.mail.Writes() != 0 {
		t.Errorf("expected the home's claim kept and no mail")
	}
}

func TestARetiredHostIsNotGivenBack(t *testing.T) {
	w := newBoostWorld(t)
	w.give.Retired = []string{"desktop"}
	w.holds(t, "mw-gq6.1", "desktop")

	report, err := w.give.Run(context.Background(), "desktop")
	if err != nil || report.Skipped == "" || w.story(t, "mw-gq6.1").Assignee != "mw@desktop" {
		t.Errorf("expected a retired host left alone, got %+v, %v", report, err)
	}
}

func TestAGiveBackIsNotedForMwStatus(t *testing.T) {
	w := newBoostWorld(t)
	w.holds(t, "mw-gq6.1", "desktop")
	w.holds(t, "mw-gq6.2", "desktop")
	if err := w.tracker.AddLabel(context.Background(), "mw-gq6.2", "run:queued"); err != nil {
		t.Fatal(err)
	}

	if _, err := w.give.Run(context.Background(), "desktop"); err != nil {
		t.Fatal(err)
	}
	value, err := w.tracker.Note(context.Background(), application.GaveBackKey("desktop"))
	if err != nil {
		t.Fatal(err)
	}
	gave := application.ParseGaveBack(value)
	if !gave.At.Equal(boostOutageNow) || len(gave.Gave) != 1 || gave.Gave[0] != "mw-gq6.1" || gave.Left["mw-gq6.2"] == "" {
		t.Errorf("expected the note to say what was given back and what was left, got %+v from %q", gave, value)
	}
	if line := gave.Line("mw-gq6.1"); line != "given back at 11-21 10:00Z: desktop did not answer for 30 min" {
		t.Errorf("expected the status line for a given-back story, got %q", line)
	}
}
