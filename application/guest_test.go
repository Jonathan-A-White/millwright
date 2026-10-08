package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func guestRules() *apptest.FakeEpicRules {
	rules := apptest.NewFakeEpicRules()
	rules.Require("millwright", domain.EpicRequirements{Guest: "Luke"})
	return rules
}

func TestAPlanInAGuestRigIsRefusedUnlessTheOwnerAsked(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")

	_, err := application.File{Tracker: tracker, Rules: guestRules()}.Run(context.Background(), twoStoryPlan())
	if err == nil {
		t.Fatal("expected a plan in a guest rig to be refused")
	}
	for _, want := range []string{"millwright", "Luke", "--guest-ask"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to name %q, got: %v", want, err)
		}
	}
	if len(tracker.Epics()) != 0 || len(tracker.Stories()) != 0 {
		t.Errorf("expected nothing written, got epics %v stories %v", tracker.Epics(), tracker.Stories())
	}
}

func TestAPlanWithOneStoryOverridingIntoAGuestRigIsRefused(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	plan := twoStoryPlan()
	plan.Epic.Defaults.Rig = "elsewhere"
	if err := plan.Stories[1].Overrides.Set("rig", "millwright"); err != nil {
		t.Fatal(err)
	}

	_, err := application.File{Tracker: tracker, Rules: guestRules()}.Run(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "Luke") {
		t.Fatalf("expected the story in the guest rig to be refused, got %v", err)
	}
	if len(tracker.Epics()) != 0 {
		t.Errorf("expected nothing written, got %v", tracker.Epics())
	}
}

func TestTheOwnersWordsLetAGuestRigPlanThroughAndAreWrittenOnTheEpic(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	words := "Luke said: go ahead and file the Argus story"

	filed, err := application.File{Tracker: tracker, Rules: guestRules(), GuestAsk: words}.Run(context.Background(), twoStoryPlan())
	if err != nil {
		t.Fatalf("filing with the owner's words: %v", err)
	}
	comments, err := tracker.StoryComments(context.Background(), filed.EpicID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range comments {
		found = found || (strings.Contains(c.Text, words) && strings.Contains(c.Text, "Luke"))
	}
	if !found {
		t.Errorf("expected the epic to carry a comment quoting %q, got %+v", words, comments)
	}
}

func TestGuestAskWithNoGuestRigIsHarmlessButRecordsNothing(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")

	filed, err := application.File{Tracker: tracker, Rules: apptest.NewFakeEpicRules(), GuestAsk: "words"}.Run(context.Background(), twoStoryPlan())
	if err != nil {
		t.Fatalf("filing: %v", err)
	}
	comments, _ := tracker.StoryComments(context.Background(), filed.EpicID)
	if len(comments) != 0 {
		t.Errorf("expected no comment on an epic of a rig that is no guest, got %+v", comments)
	}
}

func TestStatusShowsGuestBesideAGuestRigsStories(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A ready story", "vps")
	story := domain.Story{ID: "mw-gq6.31", Title: "An unguested one"}
	if err := story.Overrides.Set("rig", "plain"); err != nil {
		t.Fatal(err)
	}
	if err := story.Overrides.Set("host", "vps"); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-gq6", story)

	report, err := application.Status{
		Tracker: tracker,
		Notes:   tracker,
		Rules:   guestRules(),
		Host:    "vps",
		Seat:    "builder",
		Now:     func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	printed := report.String()
	if !strings.Contains(printed, "mw-gq6.30 · millwright · guest: Luke") {
		t.Errorf("expected the guest rig's row to say guest: Luke, got:\n%s", printed)
	}
	if strings.Contains(printed, "mw-gq6.31 · plain · guest") {
		t.Errorf("expected no guest mark on a rig that is no guest, got:\n%s", printed)
	}
}

func TestBriefShowsGuestBesideAGuestRigsStories(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A ready story", "vps")

	var out strings.Builder
	if _, err := (application.Brief{Tracker: tracker, Rules: guestRules(), Out: &out}).Run(context.Background(), "mw-gq6"); err != nil {
		t.Fatalf("briefing: %v", err)
	}
	if !strings.Contains(out.String(), "guest: Luke") {
		t.Errorf("expected the brief to say guest: Luke, got:\n%s", out.String())
	}

	out.Reset()
	if _, err := (application.Brief{Tracker: tracker, Out: &out}).Run(context.Background(), "mw-gq6"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "guest") {
		t.Errorf("expected no guest mark without rules, got:\n%s", out.String())
	}
}
