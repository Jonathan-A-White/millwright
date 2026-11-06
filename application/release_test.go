package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// filedAndHeld files the two-story plan the way an unattended `mw file` does:
// everything in the tracker, everything held.
func filedAndHeld(t *testing.T) (*apptest.FakeTracker, application.FiledPlan) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	filed, err := application.File{Tracker: tracker}.Run(context.Background(), twoStoryPlan())
	if err != nil {
		t.Fatalf("filing the plan: %v", err)
	}
	if filed.Released {
		t.Fatal("expected an unattended filing to leave the plan held")
	}
	return tracker, filed
}

func TestReleasingAFiledPlanPrintsItsTreeAndSaysWhatIsReady(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	out := &strings.Builder{}

	found, err := application.Release{Tracker: tracker, Out: out}.Run(context.Background(), filed.EpicID)
	if err != nil {
		t.Fatalf("releasing %s: %v", filed.EpicID, err)
	}
	if !found.Released {
		t.Error("expected the release to report that it released the plan")
	}

	// The tree is the tree the filing printed: the epic, the default path, and
	// every story with its own path, its estimate and what it waits on.
	module, gateway := filed.Stories[0].ID, filed.Stories[1].ID
	for _, want := range []string{
		filed.EpicID + " · Walking skeleton",
		"default path millwright/main · claude/opus/high · tdd-feature · vps",
		module + " · Go module",
		"held · 60m · waits on nothing",
		gateway + " · Beads gateway",
		"held · waits on " + module,
		"Released the 2 held stories of " + filed.EpicID + ". Ready now: " + module + ".",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected the release to say %q, got:\n%s", want, out.String())
		}
	}

	// And exactly the story that waits on nothing is takeable now.
	ready, err := tracker.ReadyStories(context.Background(), filed.EpicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories: %v", err)
	}
	if got := apptest.IDs(ready); len(got) != 1 || got[0] != module {
		t.Fatalf("expected only %s to be ready, got %v", module, got)
	}
}

func TestReleasingTwiceChangesNothing(t *testing.T) {
	tracker, filed := filedAndHeld(t)

	first := &strings.Builder{}
	if _, err := (application.Release{Tracker: tracker, Out: first}).Run(context.Background(), filed.EpicID); err != nil {
		t.Fatalf("releasing %s: %v", filed.EpicID, err)
	}
	before, err := tracker.ReadyStories(context.Background(), filed.EpicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories: %v", err)
	}

	second := &strings.Builder{}
	if _, err := (application.Release{Tracker: tracker, Out: second}).Run(context.Background(), filed.EpicID); err != nil {
		t.Fatalf("releasing %s again: %v", filed.EpicID, err)
	}
	if !strings.Contains(second.String(), "none of the 2 stories of "+filed.EpicID+" is held") {
		t.Errorf("expected the second release to say there was nothing held, got:\n%s", second)
	}

	after, err := tracker.ReadyStories(context.Background(), filed.EpicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories after the second release: %v", err)
	}
	if strings.Join(apptest.IDs(before), ",") != strings.Join(apptest.IDs(after), ",") {
		t.Fatalf("expected releasing twice to change nothing, %v became %v",
			apptest.IDs(before), apptest.IDs(after))
	}
}

func TestReleasingAnEpicNobodyFiledWritesNothing(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	out := &strings.Builder{}

	_, err := application.Release{Tracker: tracker, Out: out}.Run(context.Background(), "f-nope")
	if err == nil {
		t.Fatal("expected releasing an epic nobody filed to be refused")
	}
	if !strings.Contains(err.Error(), "f-nope") {
		t.Errorf("expected the refusal to name the epic, got %q", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected nothing to be printed about an epic that is not there, got:\n%s", out)
	}
	for _, story := range filed.Stories {
		detail, err := tracker.ShowStory(context.Background(), story.ID)
		if err != nil {
			t.Fatalf("reading back %s: %v", story.ID, err)
		}
		if !detail.Held() {
			t.Errorf("expected %s to be held still, it is %s", story.ID, detail.Status)
		}
	}
}

// stuckTracker releases nothing: the way a tracker whose database is locked
// behaves once the reading has already succeeded.
type stuckTracker struct {
	*apptest.FakeTracker
}

func (s *stuckTracker) ReleaseStory(context.Context, string) error {
	return errors.New("the database is locked")
}

func TestAReleaseThatCannotWriteSaysWhichStory(t *testing.T) {
	tracker, filed := filedAndHeld(t)

	_, err := application.Release{Tracker: &stuckTracker{FakeTracker: tracker}}.
		Run(context.Background(), filed.EpicID)
	if err == nil {
		t.Fatal("expected a release the tracker refuses to fail")
	}
	for _, want := range []string{filed.Stories[0].ID, filed.EpicID, "the database is locked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the failure to say %q, got %q", want, err)
		}
	}
}

func TestReleasingNeedsAWorkTrackerAndAnEpic(t *testing.T) {
	if _, err := (application.Release{}).Run(context.Background(), "f-1"); err == nil {
		t.Error("expected releasing with no work tracker to be refused")
	}
	if _, err := (application.Release{Tracker: apptest.NewFakeTracker()}).Run(context.Background(), "  "); err == nil {
		t.Error("expected releasing with no epic to be refused")
	}
}

func TestReleaseStoryIdReleasesThatStoryAlone(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module, gateway := filed.Stories[0].ID, filed.Stories[1].ID
	out := &strings.Builder{}

	found, err := application.Release{Tracker: tracker, Out: out}.Run(context.Background(), module)
	if err != nil {
		t.Fatalf("releasing the story %s: %v", module, err)
	}
	if !found.Released {
		t.Error("expected the release to report that it released the story")
	}
	for _, want := range []string{module + " · Go module", "millwright/main", "released 1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected the release to say %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out.String(), gateway) {
		t.Errorf("expected the sibling %s not to be mentioned, got:\n%s", gateway, out)
	}

	released, err := tracker.ShowStory(context.Background(), module)
	if err != nil {
		t.Fatalf("reading back %s: %v", module, err)
	}
	if released.Held() {
		t.Errorf("expected %s to be released, it is %s", module, released.Status)
	}
	sibling, err := tracker.ShowStory(context.Background(), gateway)
	if err != nil {
		t.Fatalf("reading back %s: %v", gateway, err)
	}
	if !sibling.Held() {
		t.Errorf("expected %s to be held still, it is %s", gateway, sibling.Status)
	}
}

func TestReleaseStoryIdThatIsNotHeldLeavesItAsItIs(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module := filed.Stories[0].ID
	if err := tracker.ReleaseStory(context.Background(), module); err != nil {
		t.Fatalf("releasing %s first: %v", module, err)
	}
	out := &strings.Builder{}

	if _, err := (application.Release{Tracker: tracker, Out: out}).Run(context.Background(), module); err != nil {
		t.Fatalf("releasing the open story %s: %v", module, err)
	}
	if want := module + " is open, not held: nothing released"; !strings.Contains(out.String(), want) {
		t.Errorf("expected the release to say %q, got:\n%s", want, out)
	}
	if strings.Contains(out.String(), "released 1") {
		t.Errorf("expected no story to be released, got:\n%s", out)
	}
	detail, err := tracker.ShowStory(context.Background(), module)
	if err != nil {
		t.Fatalf("reading back %s: %v", module, err)
	}
	if detail.Status != apptest.StatusOpen {
		t.Errorf("expected %s to stay open, it is %s", module, detail.Status)
	}
}

func TestReleaseStoryIdWithNoRigIsRefusedAndWritesNothing(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("e-bare", domain.Path{})
	tracker.AddStory("e-bare", domain.Story{ID: "s-bare", Title: "No path"})
	if err := tracker.SetStatus("s-bare", apptest.StatusDeferred); err != nil {
		t.Fatalf("holding the story: %v", err)
	}
	out := &strings.Builder{}

	_, err := application.Release{Tracker: tracker, Out: out}.Run(context.Background(), "s-bare")
	if err == nil {
		t.Fatal("expected a story with no rig to be refused")
	}
	for _, want := range []string{"s-bare", "no path"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to say %q, got %q", want, err)
		}
	}
	detail, err := tracker.ShowStory(context.Background(), "s-bare")
	if err != nil {
		t.Fatalf("reading back s-bare: %v", err)
	}
	if !detail.Held() {
		t.Errorf("expected s-bare to be held still, it is %s", detail.Status)
	}
}

// expectsFiles says every rig expects a files key.
type expectsFiles struct{}

func (expectsFiles) EpicRequirements(context.Context, string) (domain.EpicRequirements, error) {
	return domain.EpicRequirements{FilesExpected: true}, nil
}

func TestReleaseStoryIdWarnsOfAMissingFilesKeyAndReleasesAllTheSame(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module := filed.Stories[0].ID
	out := &strings.Builder{}

	if _, err := (application.Release{Tracker: tracker, Rules: expectsFiles{}, Out: out}).Run(context.Background(), module); err != nil {
		t.Fatalf("releasing the story %s: %v", module, err)
	}
	if !strings.Contains(out.String(), module) || !strings.Contains(out.String(), "files") {
		t.Errorf("expected a warning that %s has no files key, got:\n%s", module, out)
	}
	if !strings.Contains(out.String(), "released 1") {
		t.Errorf("expected the story to be released all the same, got:\n%s", out)
	}
}

// The 2026-10-08 comment on the epic of mw-5r3p30 held its stories .1, .4 and .9
// on the Governor's word; on 2026-10-17 it refused the release of the new .573
// (mw-gq6.566).
const lampasHold = "CHANGED on his words (mw-fnvjof.1 ...) Stories .1, .4, .9 and this description edited before release (all held) on the Governor's word"

func TestAHoldOnTheEpicNamingOtherStoriesDoesNotHoldANewSibling(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module, gateway := filed.Stories[0].ID, filed.Stories[1].ID
	if err := tracker.CommentOnStory(context.Background(), filed.EpicID, lampasHold+" ("+gateway+")"); err != nil {
		t.Fatal(err)
	}

	if _, err := (application.Release{Tracker: tracker}).Run(context.Background(), module); err != nil {
		t.Fatalf("expected %s to release, the epic's comment names %s: %v", module, gateway, err)
	}
	released, err := tracker.ShowStory(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	if released.Held() {
		t.Errorf("expected %s to be released, it is %s", module, released.Status)
	}
}

func TestAHoldOnTheEpicNamingTheStoryStillHoldsItAndSaysWhereTheCommentSits(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module := filed.Stories[0].ID
	if err := tracker.CommentOnStory(context.Background(), filed.EpicID, "HELD: on the Governor's word: "+module+" waits for him"); err != nil {
		t.Fatal(err)
	}

	_, err := application.Release{Tracker: tracker}.Run(context.Background(), module)
	if err == nil {
		t.Fatal("expected the release to be refused: the epic's comment names the story")
	}
	for _, want := range []string{"held on the Governor's word", "on " + filed.EpicID, "waits for him"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to say %q, got: %v", want, err)
		}
	}
}

func TestAHoldOnTheStoryItselfNeedsNoIdAndNamesItsBead(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module := filed.Stories[0].ID
	if err := tracker.CommentOnStory(context.Background(), module, "HELD: his word, do not release yet"); err != nil {
		t.Fatal(err)
	}

	_, err := application.Release{Tracker: tracker}.Run(context.Background(), module)
	if err == nil || !strings.Contains(err.Error(), "on "+module) {
		t.Fatalf("expected a refusal naming the bead %s, got: %v", module, err)
	}
}

func TestAStoryIdInACommentIsNotTakenFromALongerId(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module := filed.Stories[0].ID
	if err := tracker.CommentOnStory(context.Background(), filed.EpicID, "HELD: on the Governor's word: "+module+"7 waits for him"); err != nil {
		t.Fatal(err)
	}

	if _, err := (application.Release{Tracker: tracker}).Run(context.Background(), module); err != nil {
		t.Fatalf("expected %s to release, the comment names %s7: %v", module, module, err)
	}
}

// A real hold opens its comment with HELD: (application/stuck.go); the words held
// and Governor anywhere else in a comment's prose are not one (mw-gq6.720).
func TestACommentOpeningHELDAndNamingTheGovernorHoldsTheStory(t *testing.T) {
	for _, text := range []string{
		"HELD: by the Governor via postern, txid direct:abc123",
		"HELD: the Governor's word, wait for him",
		"held: his word, do not release yet",
		"  HELD: the Governor wants to see it first",
	} {
		tracker, filed := filedAndHeld(t)
		module := filed.Stories[0].ID
		if err := tracker.CommentOnStory(context.Background(), module, text); err != nil {
			t.Fatal(err)
		}

		_, err := application.Release{Tracker: tracker}.Run(context.Background(), module)
		if err == nil {
			t.Errorf("comment %q: expected the release to be refused", text)
			continue
		}
		if want := "held on the Governor's word"; !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), strings.TrimSpace(text)) {
			t.Errorf("comment %q: expected a refusal naming the comment, got: %v", text, err)
		}
	}
}

func TestReleasedOverHoldStillLiftsAHELDComment(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module := filed.Stories[0].ID
	if err := tracker.CommentOnStory(context.Background(), module, "HELD: the Governor's word, wait for him"); err != nil {
		t.Fatal(err)
	}

	if _, err := (application.Release{Tracker: tracker, OverHold: "he asked for it in chat"}).Run(context.Background(), module); err != nil {
		t.Fatalf("expected --over-hold to release %s: %v", module, err)
	}
	if _, err := (application.Release{Tracker: tracker}).Run(context.Background(), module); err != nil {
		t.Fatalf("expected the release marker to lift the hold: %v", err)
	}
}

func TestProseThatOnlyMentionsHeldAndTheGovernorDoesNotHoldTheStory(t *testing.T) {
	for _, text := range []string{
		"Filed HELD: mw-bsf54t.13 ... Choices PROVISIONAL, the Governor to confirm ... HIS WORDS ...",
		"Choices PROVISIONAL, the Governor to confirm",
		`He said "the old one stays held until I say" and the Governor will say it`,
		"Parked on the Governor's word: waits for him",
		"Estimate revised; the Governor approved the plan, nothing is held",
	} {
		for _, onEpic := range []bool{false, true} {
			tracker, filed := filedAndHeld(t)
			module := filed.Stories[0].ID
			bead, prose := module, text
			if onEpic {
				bead, prose = filed.EpicID, text+" ("+module+")"
			}
			if err := tracker.CommentOnStory(context.Background(), bead, prose); err != nil {
				t.Fatal(err)
			}

			if _, err := (application.Release{Tracker: tracker}).Run(context.Background(), module); err != nil {
				t.Errorf("comment %q on %s: expected %s to release: %v", prose, bead, module, err)
			}
		}
	}
}

// pacedNow is the moment the paced-release tests read the usage at.
var pacedNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// pacedList is a list of stories p-1, p-2, ... in the statuses given, in order,
// and the usage a reading of percent was kept at ten minutes before pacedNow
// with the week resetting three days after it.
func pacedList(t *testing.T, percent float64, statuses ...string) (*apptest.FakeTracker, []string) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("p-epic", domain.Path{Rig: "septuagint", Branch: "main"})
	var ids []string
	for i, status := range statuses {
		id := fmt.Sprintf("p-%d", i+1)
		ids = append(ids, id)
		if status == "missing" {
			continue
		}
		tracker.AddStory("p-epic", domain.Story{ID: id, Title: "Batch " + id})
		if status == application.StatusInProgress {
			if err := tracker.ClaimAs(id, "mw@cloud3", pacedNow.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := tracker.SetStatus(id, status); err != nil {
			t.Fatal(err)
		}
	}
	if percent >= 0 {
		reading := application.UsageReading{Percent: percent, At: pacedNow.Add(-10 * time.Minute), Resets: pacedNow.Add(72 * time.Hour)}
		if err := (application.UsageReadings{Notes: tracker}).Add(context.Background(), reading); err != nil {
			t.Fatal(err)
		}
	}
	return tracker, ids
}

// pacedRelease runs the paced release over ids and returns what it released and
// what it printed.
func pacedRelease(t *testing.T, tracker *apptest.FakeTracker, ids []string, dryRun bool) ([]string, string) {
	t.Helper()
	var out strings.Builder
	released, err := application.ReleasePaced{
		Tracker:  tracker,
		Readings: application.UsageReadings{Notes: tracker},
		Now:      func() time.Time { return pacedNow },
		DryRun:   dryRun,
		Out:      &out,
	}.Run(context.Background(), ids)
	if err != nil {
		t.Fatalf("releasing the list: %v\n%s", err, out.String())
	}
	return released, out.String()
}

func statusOf(t *testing.T, tracker *apptest.FakeTracker, id string) string {
	t.Helper()
	detail, err := tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatalf("reading %s: %v", id, err)
	}
	return detail.Status
}

func TestThePaceIsThreeStoriesAndSixtyPercent(t *testing.T) {
	if application.ReleasePacedMax != 3 || application.ReleasePacedUsageLine != 60 {
		t.Errorf("the epic's pace is 3 at once and 60%% of the week, got %d and %v",
			application.ReleasePacedMax, application.ReleasePacedUsageLine)
	}
}

func TestPacedReleaseWithUsageOverTheLineReleasesNoneAndSaysSo(t *testing.T) {
	held := application.StatusHeld
	tracker, ids := pacedList(t, 74, held, held, held)

	released, out := pacedRelease(t, tracker, ids, false)

	if len(released) != 0 {
		t.Errorf("expected none released with the usage at 74%%, got %v", released)
	}
	for _, want := range []string{"74.0%", "over 60%", "nothing released"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the output to say %q, got:\n%s", want, out)
		}
	}
	for _, id := range ids {
		if got := statusOf(t, tracker, id); got != held {
			t.Errorf("expected %s to stay held, it is %s", id, got)
		}
		if comments := tracker.Comments(id); len(comments) != 0 {
			t.Errorf("expected no comment on %s, got %v", id, comments)
		}
	}
}

func TestPacedReleaseAtExactlyTheLineReleases(t *testing.T) {
	held := application.StatusHeld
	tracker, ids := pacedList(t, 60, held, held)

	released, _ := pacedRelease(t, tracker, ids, false)

	if strings.Join(released, ",") != "p-1,p-2" {
		t.Errorf("expected 60%% to be under the line (only over 60%% stops it), got %v", released)
	}
}

func TestPacedReleaseWithNoFreshReadingReleasesNone(t *testing.T) {
	held := application.StatusHeld
	stale := application.UsageReading{Percent: 10, At: pacedNow.Add(-5 * time.Hour), Resets: pacedNow.Add(72 * time.Hour)}
	week := application.UsageReading{Percent: 10, At: pacedNow.Add(-5 * 24 * time.Hour), Resets: pacedNow.Add(-time.Hour)}
	for name, c := range map[string]struct {
		reading *application.UsageReading
		want    string
	}{
		"no reading":                 {nil, "no usage was reported"},
		"reading hours old":          {&stale, "STALE"},
		"reading of the week before": {&week, "STALE"},
	} {
		tracker, ids := pacedList(t, -1, held)
		if c.reading != nil {
			if err := (application.UsageReadings{Notes: tracker}).Add(context.Background(), *c.reading); err != nil {
				t.Fatal(err)
			}
		}

		released, out := pacedRelease(t, tracker, ids, false)

		if len(released) != 0 || statusOf(t, tracker, "p-1") != held {
			t.Errorf("%s: expected nothing released, got %v", name, released)
		}
		if !strings.Contains(out, c.want) || !strings.Contains(out, "nothing released") {
			t.Errorf("%s: expected the output to say %q and that nothing was released, got:\n%s", name, c.want, out)
		}
	}
}

func TestPacedReleaseWithThreeOfTheListRunningReleasesNone(t *testing.T) {
	held, running := application.StatusHeld, application.StatusInProgress
	tracker, ids := pacedList(t, 30, running, running, running, held, held)

	released, out := pacedRelease(t, tracker, ids, false)

	if len(released) != 0 {
		t.Errorf("expected none released with 3 running, got %v", released)
	}
	for _, want := range []string{"3 of the list", "at most 3", "nothing released"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the output to say %q, got:\n%s", want, out)
		}
	}
	if got := statusOf(t, tracker, "p-4"); got != held {
		t.Errorf("expected p-4 to stay held, it is %s", got)
	}
}

func TestPacedReleaseWithOneRunningReleasesTheNextTwoInOrder(t *testing.T) {
	held, running := application.StatusHeld, application.StatusInProgress
	tracker, ids := pacedList(t, 52.5, held, running, held, held, held)

	released, out := pacedRelease(t, tracker, ids, false)

	if strings.Join(released, ",") != "p-1,p-3" {
		t.Fatalf("expected the next two in the list, p-1 and p-3, got %v\n%s", released, out)
	}
	for id, want := range map[string]string{"p-1": application.StatusOpen, "p-3": application.StatusOpen, "p-4": application.StatusHeld, "p-5": application.StatusHeld} {
		if got := statusOf(t, tracker, id); got != want {
			t.Errorf("expected %s to be %s, it is %s", id, want, got)
		}
	}
	// Each bead released says what it acted on.
	for _, id := range released {
		comments := tracker.Comments(id)
		if len(comments) != 1 {
			t.Fatalf("expected one comment on %s, got %v", id, comments)
		}
		for _, want := range []string{"RELEASED", "52.5%", "1 of the list running or open", "at most 3"} {
			if !strings.Contains(comments[0], want) {
				t.Errorf("expected the comment on %s to quote %q, got %q", id, want, comments[0])
			}
		}
	}
	if comments := tracker.Comments("p-4"); len(comments) != 0 {
		t.Errorf("expected no comment on p-4, got %v", comments)
	}
	if !strings.Contains(out, "released 2: p-1, p-3") {
		t.Errorf("expected the output to name what it released, got:\n%s", out)
	}
}

func TestPacedReleaseCountsAnOpenStoryOfTheListAgainstThePace(t *testing.T) {
	held, open := application.StatusHeld, application.StatusOpen
	tracker, ids := pacedList(t, 30, open, open, held, held)

	released, _ := pacedRelease(t, tracker, ids, false)

	if strings.Join(released, ",") != "p-3" {
		t.Errorf("expected two open to leave room for one, got %v", released)
	}
}

func TestPacedReleaseSkipsAndNamesAClosedAMissingAndAnIcedStory(t *testing.T) {
	held, closed := application.StatusHeld, application.StatusClosed
	tracker, ids := pacedList(t, 30, closed, "missing", held, held, held, held)
	if err := tracker.AddLabel(context.Background(), "p-3", application.LabelIcebox); err != nil {
		t.Fatal(err)
	}

	released, out := pacedRelease(t, tracker, ids, false)

	if strings.Join(released, ",") != "p-4,p-5,p-6" {
		t.Errorf("expected the held, un-iced stories after the skipped ones, got %v", released)
	}
	for _, want := range []string{"closed, skipped: p-1", "not in the tracker, skipped: p-2", "iced, skipped (mw thaw first): p-3"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the output to say %q, got:\n%s", want, out)
		}
	}
}

func TestPacedReleaseLeavesAStoryHeldOnTheGovernorsWord(t *testing.T) {
	held := application.StatusHeld
	tracker, ids := pacedList(t, 30, held, held)
	if err := tracker.CommentOnStory(context.Background(), "p-1", "HELD: waits for the Governor's word"); err != nil {
		t.Fatal(err)
	}

	released, out := pacedRelease(t, tracker, ids, false)

	if strings.Join(released, ",") != "p-2" {
		t.Errorf("expected only p-2 released, got %v", released)
	}
	if !strings.Contains(out, "p-1 is held on the Governor's word") {
		t.Errorf("expected the output to name p-1, got:\n%s", out)
	}
}

func TestPacedReleaseDryRunReleasesNothingAndPrintsWhatItWould(t *testing.T) {
	held := application.StatusHeld
	tracker, ids := pacedList(t, 30, held, held, held, held)

	released, out := pacedRelease(t, tracker, ids, true)

	if len(released) != 0 {
		t.Errorf("expected a dry run to release none, got %v", released)
	}
	if !strings.Contains(out, "would release 3: p-1, p-2, p-3") {
		t.Errorf("expected the output to say what it would release, got:\n%s", out)
	}
	for _, id := range ids {
		if got := statusOf(t, tracker, id); got != held {
			t.Errorf("expected %s to stay held, it is %s", id, got)
		}
		if comments := tracker.Comments(id); len(comments) != 0 {
			t.Errorf("expected no comment on %s, got %v", id, comments)
		}
	}
}
