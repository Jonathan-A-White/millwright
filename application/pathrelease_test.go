package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aModelPassRig is a releasing rig (mw-gq6) whose story mw-gq6.2 is held, pathed
// from mw-gq6.1 and labelled model-story, with a usage reading of percent kept
// ten minutes before pacedNow (none when percent is negative).
func aModelPassRig(t *testing.T, percent float64) (*apptest.FakeTracker, *apptest.FakeEpicRules) {
	t.Helper()
	tracker, rules := aReleasingRig(t)
	if err := tracker.SetLabels("mw-gq6.2", application.LabelModelStory); err != nil {
		t.Fatal(err)
	}
	if percent >= 0 {
		reading := application.UsageReading{Percent: percent, At: pacedNow.Add(-10 * time.Minute), Resets: pacedNow.Add(72 * time.Hour)}
		if err := (application.UsageReadings{Notes: tracker}).Add(context.Background(), reading); err != nil {
			t.Fatal(err)
		}
	}
	return tracker, rules
}

// setPathAfter runs mw path set --after on mw-gq6.2 at pacedNow and reports what
// it printed.
func setPathAfter(t *testing.T, tracker *apptest.FakeTracker, rules *apptest.FakeEpicRules) string {
	t.Helper()
	var out strings.Builder
	_, err := application.PathSet{Tracker: tracker, Rules: rules, Out: &out, After: []string{"mw-gq6.3"},
		Readings: application.UsageReadings{Notes: tracker}, Now: func() time.Time { return pacedNow }}.
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil)
	if err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	return out.String()
}

func TestPathSetKeepsAModelStoryHeldWhenTheUsageIsOverTheLine(t *testing.T) {
	tracker, rules := aModelPassRig(t, 74)
	printed := setPathAfter(t, tracker, rules)

	if got := statusOf(t, tracker, "mw-gq6.2"); got != application.StatusHeld {
		t.Errorf("expected the model story held at 74%% of the week, got %s", got)
	}
	if !strings.Contains(printed, "held: mw-gq6.2: usage 74.0% of the week") || !strings.Contains(printed, "over 60%") {
		t.Errorf("expected a held: line naming the usage reading, got:\n%s", printed)
	}
	if n := strings.Count(printed, "held: "); n != 1 {
		t.Errorf("expected one held: line, got %d:\n%s", n, printed)
	}
	if comments, _ := tracker.StoryComments(context.Background(), "mw-gq6.2"); len(comments) != 0 {
		t.Errorf("expected no released-on-path-set comment, got %+v", comments)
	}
}

func TestPathSetKeepsAModelStoryHeldWhenTheUsageIsMissingOrStale(t *testing.T) {
	missing, rules := aModelPassRig(t, -1)
	printed := setPathAfter(t, missing, rules)
	if got := statusOf(t, missing, "mw-gq6.2"); got != application.StatusHeld || !strings.Contains(printed, "held: mw-gq6.2: no usage was reported") {
		t.Errorf("missing reading: expected the story held with a held: line, got %s and:\n%s", got, printed)
	}

	stale, rules := aModelPassRig(t, -1)
	old := application.UsageReading{Percent: 20, At: pacedNow.Add(-30 * time.Hour), Resets: pacedNow.Add(72 * time.Hour)}
	if err := (application.UsageReadings{Notes: stale}).Add(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	printed = setPathAfter(t, stale, rules)
	if got := statusOf(t, stale, "mw-gq6.2"); got != application.StatusHeld || !strings.Contains(printed, "held: mw-gq6.2: usage 20.0% is STALE") {
		t.Errorf("stale reading: expected the story held with a held: line, got %s and:\n%s", got, printed)
	}
}

func TestPathSetKeepsAModelStoryHeldWhenItsEpicHasTheMostModelStoriesOpen(t *testing.T) {
	tracker, rules := aModelPassRig(t, 30)
	for i := 0; i < application.ReleasePacedMax; i++ {
		id := fmt.Sprintf("mw-gq6.1%d", i)
		tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "pass " + id})
		if err := tracker.SetLabels(id, application.LabelModelStory); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if err := tracker.ClaimAs(id, "mw@cloud3", pacedNow.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	printed := setPathAfter(t, tracker, rules)

	if got := statusOf(t, tracker, "mw-gq6.2"); got != application.StatusHeld {
		t.Errorf("expected the model story held with 3 of its epic open or running, got %s", got)
	}
	if !strings.Contains(printed, "held: mw-gq6.2: 3 of its epic's model stories running or open, at most 3 at once") {
		t.Errorf("expected a held: line naming the count, got:\n%s", printed)
	}
}

func TestPathSetReleasesAModelStoryUnderTheLineAndUnderTheCount(t *testing.T) {
	tracker, rules := aModelPassRig(t, 59.9)
	// Two others open and a closed one, a held one and an unlabelled open one
	// do not make the three.
	for i, status := range []string{application.StatusOpen, application.StatusInProgress, application.StatusHeld, "closed", application.StatusOpen} {
		id := fmt.Sprintf("mw-gq6.1%d", i)
		tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "pass " + id})
		if i < 4 {
			if err := tracker.SetLabels(id, application.LabelModelStory); err != nil {
				t.Fatal(err)
			}
		}
		switch status {
		case application.StatusInProgress:
			if err := tracker.ClaimAs(id, "mw@cloud3", pacedNow.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		default:
			if err := tracker.SetStatus(id, status); err != nil {
				t.Fatal(err)
			}
		}
	}
	printed := setPathAfter(t, tracker, rules)

	if got := statusOf(t, tracker, "mw-gq6.2"); got == application.StatusHeld {
		t.Errorf("expected the model story released under the line and the count, got:\n%s", printed)
	}
	if !strings.Contains(printed, application.ReleasedOnPathSet) {
		t.Errorf("expected the released-on-path-set line, got:\n%s", printed)
	}
}

func TestPathSetReleasesAStoryWithoutTheModelLabelWhateverTheUsage(t *testing.T) {
	for _, percent := range []float64{74, -1} {
		tracker, rules := aModelPassRig(t, percent)
		if err := tracker.SetLabels("mw-gq6.2"); err != nil {
			t.Fatal(err)
		}
		printed := setPathAfter(t, tracker, rules)
		if got := statusOf(t, tracker, "mw-gq6.2"); got == application.StatusHeld {
			t.Errorf("usage %v: expected an unlabelled story released, got:\n%s", percent, printed)
		}
	}
}

// The pace is one check: release-paced and path set hold a story back for the
// same words.
func TestPathSetAndReleasePacedGiveTheSameReasonForTheUsage(t *testing.T) {
	tracker, ids := pacedList(t, 74, application.StatusHeld)
	_, pacedOut := pacedRelease(t, tracker, ids, false)
	tracker2, rules := aModelPassRig(t, 74)
	printed := setPathAfter(t, tracker2, rules)

	_, reason, _ := strings.Cut(strings.TrimSpace(printed), "held: mw-gq6.2: ")
	if reason == "" || !strings.Contains(pacedOut, reason) {
		t.Errorf("expected release-paced to say %q, it said %q", reason, pacedOut)
	}
}
