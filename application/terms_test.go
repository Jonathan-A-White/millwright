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

const newSourceTitle = "sermon-index: source Example Trust"

// aTermsRig is mw-gq6.2, held with a path, on a rig whose file sets terms_title,
// under the title the rig's guard matches.
func aTermsRig(t *testing.T, title string) (*apptest.FakeTracker, *apptest.FakeEpicRules) {
	t.Helper()
	tracker, rules := aReleasingRig(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: title})
	if err := tracker.SetStatus("mw-gq6.9", application.StatusHeld); err != nil {
		t.Fatal(err)
	}
	rules.Require("millwright", domain.EpicRequirements{ReleaseOnPath: true, TermsTitle: `^sermon-index: source `})
	return tracker, rules
}

func pathAfterTheTermsStory(t *testing.T, tracker *apptest.FakeTracker, rules *apptest.FakeEpicRules) string {
	t.Helper()
	var out strings.Builder
	_, err := application.PathSet{Tracker: tracker, Rules: rules, Out: &out, After: []string{"mw-gq6.3"}}.
		Run(context.Background(), "mw-gq6.9", "mw-gq6.1", nil)
	if err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	return out.String()
}

func TestAWebSourceStoryWithNoTermsLineIsNotReleased(t *testing.T) {
	ctx := context.Background()
	refusal := []string{"mw-gq6.9", "TERMS:", "docs/adding-a-source.md"}

	t.Run("path set --after keeps it held and says why", func(t *testing.T) {
		tracker, rules := aTermsRig(t, newSourceTitle)
		out := pathAfterTheTermsStory(t, tracker, rules)
		if got := statusOf(t, tracker, "mw-gq6.9"); got != application.StatusHeld {
			t.Errorf("expected the story kept held, got %s", got)
		}
		for _, said := range append([]string{"held: mw-gq6.9:"}, refusal...) {
			if !strings.Contains(out, said) {
				t.Errorf("expected the held line to say %q, got:\n%s", said, out)
			}
		}
	})

	t.Run("mw release refuses it, even with --force", func(t *testing.T) {
		tracker, rules := aTermsRig(t, newSourceTitle)
		_, err := application.Release{Tracker: tracker, Rules: rules, Force: "no"}.Run(ctx, "mw-gq6.9")
		if err == nil {
			t.Fatal("expected the release to be refused")
		}
		for _, said := range refusal {
			if !strings.Contains(err.Error(), said) {
				t.Errorf("expected the refusal to say %q, got %q", said, err)
			}
		}
		if got := statusOf(t, tracker, "mw-gq6.9"); got != application.StatusHeld {
			t.Errorf("expected the story left held, got %s", got)
		}
	})

	t.Run("mw release of its epic refuses and releases none", func(t *testing.T) {
		tracker, rules := aTermsRig(t, newSourceTitle)
		_, err := application.Release{Tracker: tracker, Rules: rules}.Run(ctx, "mw-gq6")
		if err == nil || !strings.Contains(err.Error(), "mw-gq6.9") || !strings.Contains(err.Error(), "TERMS:") {
			t.Fatalf("expected a refusal naming mw-gq6.9 and TERMS:, got %v", err)
		}
		if got := statusOf(t, tracker, "mw-gq6.2"); got != application.StatusHeld {
			t.Errorf("expected nothing released, mw-gq6.2 is %s", got)
		}
	})

	t.Run("a TERMS: line in the description lets it out", func(t *testing.T) {
		tracker, rules := aTermsRig(t, newSourceTitle)
		if err := tracker.SetDescription("mw-gq6.9", "Crawl it.\n  TERMS: robots.txt allows /sermons; terms say \"you may browse\"; the crawl is allowed."); err != nil {
			t.Fatal(err)
		}
		pathAfterTheTermsStory(t, tracker, rules)
		if got := statusOf(t, tracker, "mw-gq6.9"); got == application.StatusHeld {
			t.Errorf("expected the story released, it is %s", got)
		}
	})

	t.Run("a TERMS: comment lets it out", func(t *testing.T) {
		tracker, rules := aTermsRig(t, newSourceTitle)
		if err := tracker.CommentOnStory(ctx, "mw-gq6.9", "TERMS: terms read, clause quoted, the crawl is allowed"); err != nil {
			t.Fatal(err)
		}
		if _, err := (application.Release{Tracker: tracker, Rules: rules}).Run(ctx, "mw-gq6.9"); err != nil {
			t.Fatalf("expected the release: %v", err)
		}
		if got := statusOf(t, tracker, "mw-gq6.9"); got == application.StatusHeld {
			t.Errorf("expected the story released, it is %s", got)
		}
	})

	t.Run("the word TERMS: in the middle of a line is no TERMS: line", func(t *testing.T) {
		tracker, rules := aTermsRig(t, newSourceTitle)
		if err := tracker.CommentOnStory(ctx, "mw-gq6.9", "I will add TERMS: later"); err != nil {
			t.Fatal(err)
		}
		if _, err := (application.Release{Tracker: tracker, Rules: rules}).Run(ctx, "mw-gq6.9"); err == nil {
			t.Fatal("expected the release to be refused")
		}
	})

	t.Run("a title that does not match is not touched", func(t *testing.T) {
		tracker, rules := aTermsRig(t, "sermon-index: fix the parser")
		if _, err := (application.Release{Tracker: tracker, Rules: rules}).Run(ctx, "mw-gq6.9"); err != nil {
			t.Fatalf("expected the release: %v", err)
		}
		if got := statusOf(t, tracker, "mw-gq6.9"); got == application.StatusHeld {
			t.Errorf("expected the story released, it is %s", got)
		}
	})

	t.Run("a rig without the key is not touched", func(t *testing.T) {
		tracker, rules := aTermsRig(t, newSourceTitle)
		rules.Require("millwright", domain.EpicRequirements{ReleaseOnPath: true})
		if _, err := (application.Release{Tracker: tracker, Rules: rules}).Run(ctx, "mw-gq6.9"); err != nil {
			t.Fatalf("expected the release: %v", err)
		}
		if got := statusOf(t, tracker, "mw-gq6.9"); got == application.StatusHeld {
			t.Errorf("expected the story released, it is %s", got)
		}
	})

	t.Run("a paced release skips it, names it and releases the next", func(t *testing.T) {
		held := application.StatusHeld
		tracker, ids := pacedList(t, 30, held, held)
		tracker.AddStory("p-epic", domain.Story{ID: "p-3", Title: newSourceTitle})
		if err := tracker.SetStatus("p-3", held); err != nil {
			t.Fatal(err)
		}
		rules := apptest.NewFakeEpicRules()
		rules.Require("septuagint", domain.EpicRequirements{TermsTitle: `^sermon-index: source `})
		var out strings.Builder
		released, err := application.ReleasePaced{
			Tracker: tracker, Rules: rules, Readings: application.UsageReadings{Notes: tracker},
			Now: func() time.Time { return pacedNow }, Out: &out,
		}.Run(ctx, append([]string{"p-3"}, ids...))
		if err != nil {
			t.Fatalf("releasing the list: %v", err)
		}
		if strings.Join(released, ",") != "p-1,p-2" {
			t.Errorf("expected p-1 and p-2 released, got %v", released)
		}
		if got := statusOf(t, tracker, "p-3"); got != application.StatusHeld {
			t.Errorf("expected p-3 left held, got %s", got)
		}
		for _, said := range []string{"p-3", "TERMS:", "skipped"} {
			if !strings.Contains(out.String(), said) {
				t.Errorf("expected the output to say %q, got:\n%s", said, out.String())
			}
		}
	})
}

// An approved plan releases each story it filed except a web source whose
// description holds no TERMS: line, which stays held and is named.
func TestFileApproveKeepsAWebSourceWithNoTermsLineHeld(t *testing.T) {
	plan := twoStoryPlan()
	plan.Stories[0].Title = newSourceTitle
	plan.Stories[1].Title = newSourceTitle + " Two"
	plan.Stories[1].Description = "Crawl it.\nTERMS: robots.txt allows it; the terms say \"you may browse\"; allowed."
	plan.Stories[1].Needs = nil
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	rules := apptest.NewFakeEpicRules()
	rules.Require("millwright", domain.EpicRequirements{TermsTitle: `^sermon-index: source `})
	var out strings.Builder

	filed, err := application.File{
		Tracker: tracker, Rules: rules, Out: &out,
		Approve: func(context.Context, application.FiledPlan) (bool, error) { return true, nil },
	}.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("filing the plan: %v", err)
	}

	if got := statusOf(t, tracker, filed.Stories[0].ID); got != application.StatusHeld {
		t.Errorf("expected %s left held, got %s", filed.Stories[0].ID, got)
	}
	if got := statusOf(t, tracker, filed.Stories[1].ID); got == application.StatusHeld {
		t.Errorf("expected %s released, it is %s", filed.Stories[1].ID, got)
	}
	for _, said := range []string{filed.Stories[0].ID, "TERMS:", "skipped"} {
		if !strings.Contains(out.String(), said) {
			t.Errorf("expected the output to say %q, got:\n%s", said, out.String())
		}
	}
}
