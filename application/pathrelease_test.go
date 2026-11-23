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

// overCallers sets the path of mw-gq6.2 from mw-gq6.1 with the given keys, on a
// rig whose checkout has callers test callers for its files, and says what it
// printed on stdout and stderr. rules is the rig's file (aReleasingRig's, or one
// with no release_on_path).
func overCallers(t *testing.T, tracker *apptest.FakeTracker, rules *apptest.FakeEpicRules, callers int, pairs ...string) (string, string) {
	t.Helper()
	files := apptest.NewFakeRigFiles()
	files.SetTestCallers("/rigs/millwright", callers)
	var out, errs strings.Builder
	_, err := application.PathSet{Tracker: tracker, Rules: rules, Out: &out, Err: &errs, After: []string{},
		Rigs: map[string]string{"millwright": "/rigs/millwright"}, RigFiles: files}.
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", pairs)
	if err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	return out.String(), errs.String()
}

func TestPathSetKeepsAStoryOverItsTestCallersHeldOnAReleasingRig(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	out, _ := overCallers(t, tracker, rules, 11, "size=S", "files=application/a.go")

	if got := statusOf(t, tracker, "mw-gq6.2"); got != application.StatusHeld {
		t.Errorf("expected the story held at 11 test callers for size S, got %s", got)
	}
	comments, _ := tracker.StoryComments(context.Background(), "mw-gq6.2")
	if len(comments) != 1 || !application.IsHoldComment(comments[0].Text) ||
		!strings.Contains(comments[0].Text, "test callers 11 for size S (allows 10)") {
		t.Errorf("expected one HELD: comment naming the count and the limit, got %+v", comments)
	}
	if !strings.Contains(out, "NOT released") || !strings.Contains(out, "test callers 11 for size S (allows 10)") {
		t.Errorf("expected stdout to say the story was NOT released and why, got:\n%s", out)
	}
}

func TestPathSetReleasesAStoryOverItsCallersWhenSizedUpOrWaived(t *testing.T) {
	for name, pairs := range map[string][]string{
		"sized up": {"size=M", "files=application/a.go"},
		"waived":   {"size=S", "files=application/a.go", "callers_ok=additive only"},
	} {
		tracker, rules := aReleasingRig(t)
		out, _ := overCallers(t, tracker, rules, 11, pairs...)
		if got := statusOf(t, tracker, "mw-gq6.2"); got == application.StatusHeld {
			t.Errorf("%s: expected the story released, got %s", name, got)
		}
		if !strings.Contains(out, application.ReleasedOnPathSet) || strings.Contains(out, "NOT released") {
			t.Errorf("%s: expected the story released on path set, got:\n%s", name, out)
		}
	}
}

func TestPathSetReleasesAStoryHeldForItsCallersOnceItIsSizedUp(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	overCallers(t, tracker, rules, 11, "size=S", "files=application/a.go")
	out, _ := overCallers(t, tracker, rules, 11, "size=M")

	if got := statusOf(t, tracker, "mw-gq6.2"); got == application.StatusHeld {
		t.Errorf("expected the story released by the second path set, got %s with:\n%s", got, out)
	}
}

func TestPathSetKeepsAStillOverStoryHeldWithoutAnotherComment(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	overCallers(t, tracker, rules, 11, "size=S", "files=application/a.go")
	overCallers(t, tracker, rules, 11, "size=S")

	if got := statusOf(t, tracker, "mw-gq6.2"); got != application.StatusHeld {
		t.Errorf("expected the story still held, got %s", got)
	}
	if comments, _ := tracker.StoryComments(context.Background(), "mw-gq6.2"); len(comments) != 1 {
		t.Errorf("expected the one HELD: comment, got %+v", comments)
	}
}

func TestPathSetOnlyWarnsOfCallersOnARigThatDoesNotReleaseOnPath(t *testing.T) {
	tracker, _ := aReleasingRig(t)
	rules := apptest.NewFakeEpicRules()
	out, errs := overCallers(t, tracker, rules, 11, "size=S", "files=application/a.go")

	if !strings.Contains(errs, "WARN: 11 test callers for size S") {
		t.Errorf("expected the WARN on stderr, got %q", errs)
	}
	if strings.Contains(out, "NOT released") {
		t.Errorf("expected no NOT released line on a rig without release_on_path, got:\n%s", out)
	}
	if comments, _ := tracker.StoryComments(context.Background(), "mw-gq6.2"); len(comments) != 0 {
		t.Errorf("expected no comment, got %+v", comments)
	}
}

func TestPathSetReleasesAModelStoryAtSeventyPercentWhenTheLineIs85(t *testing.T) {
	tracker, rules := aModelPassRig(t, 70)
	var out strings.Builder
	_, err := application.PathSet{Tracker: tracker, Rules: rules, Out: &out, After: []string{"mw-gq6.3"},
		Readings: application.UsageReadings{Notes: tracker}, UsageLine: 85, Now: func() time.Time { return pacedNow }}.
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil)
	if err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if strings.Contains(out.String(), "held: mw-gq6.2") {
		t.Errorf("expected the model story not held at 70%% with the line at 85, got:\n%s", out.String())
	}
}

// heldLineOf sets the path of mw-gq6.2 with --after on a releasing rig that has
// had comment written on it and says what mw path set printed.
func heldLineOf(t *testing.T, labels []string, comment string) string {
	t.Helper()
	tracker, rules := aReleasingRig(t)
	if len(labels) > 0 {
		if err := tracker.SetLabels("mw-gq6.2", labels...); err != nil {
			t.Fatal(err)
		}
	}
	if comment != "" {
		if err := tracker.CommentOnStory(context.Background(), "mw-gq6.2", comment); err != nil {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Out: &out, After: []string{"mw-gq6.3"}}).
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	return heldOf(out.String())
}

// heldOf is the held: line among what mw path set printed.
func heldOf(printed string) string {
	for _, line := range strings.Split(printed, "\n") {
		if strings.HasPrefix(line, "held: ") {
			return line
		}
	}
	return ""
}

// mw-gq6.1552: a held: line names the way out, so nobody reaches for a raw bd
// update to release the story.
func TestPathSetHeldLineForAMayorsHoldNamesMwRelease(t *testing.T) {
	printed := heldLineOf(t, nil, "HELD: waits for the new schema")
	if !strings.HasPrefix(printed, "held: mw-gq6.2: it has a HELD: comment: HELD: waits for the new schema") ||
		!strings.HasSuffix(printed, "release it with: mw release mw-gq6.2") {
		t.Errorf("expected the held: line to end with the way out, got:\n%s", printed)
	}
	if strings.Contains(printed, "--over-hold") {
		t.Errorf("a Mayor's hold needs no --over-hold, got:\n%s", printed)
	}
}

func TestPathSetHeldLineForTheGovernorsWordNamesOverHold(t *testing.T) {
	printed := heldLineOf(t, nil, "HELD: waits for the Governor's word on the colour")
	if !strings.HasPrefix(printed, "held: mw-gq6.2: it is held on the Governor's word") ||
		!strings.HasSuffix(printed, `release it with: mw release mw-gq6.2 --over-hold "<why>"`) {
		t.Errorf("expected the held: line to end with the --over-hold way out, got:\n%s", printed)
	}
}

func TestPathSetHeldLineForALabelNamesTheWayOut(t *testing.T) {
	for label, want := range map[string]string{
		application.LabelIcebox: "thaw it with: mw thaw mw-gq6.2",
		application.LabelHitl:   "release it with: mw release mw-gq6.2",
	} {
		printed := heldLineOf(t, []string{label}, "")
		if !strings.HasPrefix(printed, "held: mw-gq6.2: it has the label "+label) || !strings.HasSuffix(printed, want) {
			t.Errorf("label %s: expected the held: line to end %q, got:\n%s", label, want, printed)
		}
	}
}

func TestPathSetHoldLineNamesMwRelease(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	var out strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Out: &out, After: []string{"mw-gq6.3"}, Hold: "waits for the demo"}).
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if got := heldOf(out.String()); got != "held: mw-gq6.2: waits for the demo; release it with: mw release mw-gq6.2" {
		t.Errorf("expected the --hold line to name the way out, got:\n%s", got)
	}
}

func TestPathSetHeldLineForABugWithNoSweepNamesTheWayOut(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	if err := tracker.SetType("mw-gq6.2", "bug"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetAcceptance("mw-gq6.2", "1. it works"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetCreated("mw-gq6.2", domain.SweepRuleFrom.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Out: &out, After: []string{"mw-gq6.3"}}).
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	got := heldOf(out.String())
	if !strings.HasPrefix(got, "held: mw-gq6.2: no sweep: mw-gq6.2") || !strings.HasSuffix(got, "then run mw path set mw-gq6.2 again") {
		t.Errorf("expected the no-sweep held: line to name the way out, got %q", got)
	}
}

// [pace] max_at_once (mw-gq6.1695): PathSet.MaxAtOnce moves the count of model
// stories of an epic open or running at once; zero is ReleasePacedMax.
func TestPathSetReleasesAModelStoryWhenFourAreOpenAndMaxAtOnceIsFive(t *testing.T) {
	for _, tc := range []struct {
		max      int
		released bool
	}{{5, true}, {0, false}} {
		tracker, rules := aModelPassRig(t, 30)
		for i := 0; i < 4; i++ {
			id := fmt.Sprintf("mw-gq6.1%d", i)
			tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "pass " + id})
			if err := tracker.SetLabels(id, application.LabelModelStory); err != nil {
				t.Fatal(err)
			}
		}
		var out strings.Builder
		_, err := application.PathSet{Tracker: tracker, Rules: rules, Out: &out, After: []string{"mw-gq6.3"},
			Readings: application.UsageReadings{Notes: tracker}, MaxAtOnce: tc.max, Now: func() time.Time { return pacedNow }}.
			Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil)
		if err != nil {
			t.Fatalf("setting the path: %v", err)
		}
		held := statusOf(t, tracker, "mw-gq6.2") == application.StatusHeld
		if held == tc.released {
			t.Errorf("max_at_once %d: expected released=%v, got held=%v:\n%s", tc.max, tc.released, held, out.String())
		}
		if !tc.released && !strings.Contains(out.String(), "4 of its epic's model stories running or open, at most 3 at once") {
			t.Errorf("expected the held: line to name the count of 3, got:\n%s", out.String())
		}
	}
}
