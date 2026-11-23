package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// lampasAC4 is the acceptance of mw-u2w5klk.320 (mw-gq6.1686): its criterion 4
// needs the story's own deploy, so no Builder can meet it, and it was refused at
// close-out on 2026-11-23.
const lampasAC4 = "1. mw verify passes.\n" +
	"2. go test ./... passes.\n" +
	"3. make lint passes.\n" +
	"4. After the next deploy, /data-lxx/index.json on https://lampas.allmymind.org has the new rows."

const runnableAC = "1. mw verify passes.\n2. go test ./... passes."

// aHeldStoryWithAcceptance is mw-gq6.2, held with a path, carrying acceptance.
func aHeldStoryWithAcceptance(t *testing.T, acceptance string) *apptest.FakeTracker {
	t.Helper()
	tracker, _ := aReleasingRig(t)
	if err := tracker.SetAcceptance("mw-gq6.2", acceptance); err != nil {
		t.Fatal(err)
	}
	return tracker
}

func TestReleaseRefusesAStoryWithACriterionThatNeedsItsOwnDeploy(t *testing.T) {
	tracker := aHeldStoryWithAcceptance(t, lampasAC4)
	var out strings.Builder

	_, err := application.Release{Tracker: tracker, Out: &out}.Run(context.Background(), "mw-gq6.2")
	if err == nil {
		t.Fatal("expected the release to be refused")
	}
	for _, said := range []string{"mw-gq6.2", "criterion 4", "Verify strings:", "mw verify", "--force"} {
		if !strings.Contains(err.Error(), said) {
			t.Errorf("expected the refusal to say %q, got %q", said, err)
		}
	}
	if got := statusOf(t, tracker, "mw-gq6.2"); got != application.StatusHeld {
		t.Errorf("expected the story left held, got %s", got)
	}
}

func TestReleaseForceReleasesThatStoryAndWritesWhy(t *testing.T) {
	tracker := aHeldStoryWithAcceptance(t, lampasAC4)

	_, err := application.Release{Tracker: tracker, Force: "the Mayor checks it after landing"}.Run(context.Background(), "mw-gq6.2")
	if err != nil {
		t.Fatalf("expected --force to release it: %v", err)
	}
	if got := statusOf(t, tracker, "mw-gq6.2"); got == application.StatusHeld {
		t.Errorf("expected the story released, it is %s", got)
	}
	comments, _ := tracker.StoryComments(context.Background(), "mw-gq6.2")
	if len(comments) != 1 || !strings.Contains(comments[0].Text, application.ForceReleaseMarker) ||
		!strings.Contains(comments[0].Text, "the Mayor checks it after landing") {
		t.Errorf("expected one %s comment with the why, got %+v", application.ForceReleaseMarker, comments)
	}
}

func TestReleaseOfAStoryWithOnlyRunnableCriteriaGoesAsToday(t *testing.T) {
	tracker := aHeldStoryWithAcceptance(t, runnableAC)

	if _, err := (application.Release{Tracker: tracker}).Run(context.Background(), "mw-gq6.2"); err != nil {
		t.Fatalf("expected the release: %v", err)
	}
	if got := statusOf(t, tracker, "mw-gq6.2"); got == application.StatusHeld {
		t.Errorf("expected the story released, it is %s", got)
	}
	if comments, _ := tracker.StoryComments(context.Background(), "mw-gq6.2"); len(comments) != 0 {
		t.Errorf("expected no comment, got %+v", comments)
	}
}

func TestReleaseOfAnEpicRefusesWhenAStoryHasSuchACriterionAndReleasesNone(t *testing.T) {
	tracker := aHeldStoryWithAcceptance(t, lampasAC4)

	_, err := application.Release{Tracker: tracker}.Run(context.Background(), "mw-gq6")
	if err == nil || !strings.Contains(err.Error(), "mw-gq6.2") || !strings.Contains(err.Error(), "criterion 4") {
		t.Fatalf("expected a refusal naming mw-gq6.2 criterion 4, got %v", err)
	}
	if got := statusOf(t, tracker, "mw-gq6.2"); got != application.StatusHeld {
		t.Errorf("expected nothing released, got %s", got)
	}
}

func TestPathSetAfterKeepsSuchAStoryHeldAndSaysWhy(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	if err := tracker.SetAcceptance("mw-gq6.2", lampasAC4); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder

	_, err := application.PathSet{Tracker: tracker, Rules: rules, Out: &out, After: []string{"mw-gq6.3"}}.
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil)
	if err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if got := statusOf(t, tracker, "mw-gq6.2"); got != application.StatusHeld {
		t.Errorf("expected the story kept held, got %s", got)
	}
	for _, said := range []string{"held: mw-gq6.2:", "criterion 4", "Verify strings:", "--force"} {
		if !strings.Contains(out.String(), said) {
			t.Errorf("expected the held line to say %q, got:\n%s", said, out.String())
		}
	}
}

func TestPathSetAfterWithForceReleasesItAndWritesWhy(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	if err := tracker.SetAcceptance("mw-gq6.2", lampasAC4); err != nil {
		t.Fatal(err)
	}

	_, err := application.PathSet{Tracker: tracker, Rules: rules, After: []string{"mw-gq6.3"}, Force: "judged after landing by the Mayor"}.
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil)
	if err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if got := statusOf(t, tracker, "mw-gq6.2"); got == application.StatusHeld {
		t.Errorf("expected the story released, it is %s", got)
	}
	var texts []string
	comments, _ := tracker.StoryComments(context.Background(), "mw-gq6.2")
	for _, c := range comments {
		texts = append(texts, c.Text)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, application.ForceReleaseMarker+" judged after landing by the Mayor") ||
		!strings.Contains(joined, application.ReleasedOnPathSet) {
		t.Errorf("expected the force comment and the released comment, got %q", joined)
	}
}

func TestPathSetAfterOfAStoryWithoutSuchACriterionReleasesAsToday(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	if err := tracker.SetAcceptance("mw-gq6.2", runnableAC); err != nil {
		t.Fatal(err)
	}
	_, err := application.PathSet{Tracker: tracker, Rules: rules, After: []string{"mw-gq6.3"}}.
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil)
	if err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if got := statusOf(t, tracker, "mw-gq6.2"); got == application.StatusHeld {
		t.Errorf("expected the story released, it is %s", got)
	}
}

func TestReleaseLeavesAVerifyAfterLandingParagraphAlone(t *testing.T) {
	tracker := aHeldStoryWithAcceptance(t, "1. go test ./... passes.\n2. A case that dates a video the day before.\n"+
		"Verify after landing (the Mayor's check, not this story's): https://sermons.allmymind.org/v1/rephil shows 481.")

	if _, err := (application.Release{Tracker: tracker}).Run(context.Background(), "mw-gq6.2"); err != nil {
		t.Fatalf("expected the release, the live URL is under a Verify after landing line: %v", err)
	}
}
