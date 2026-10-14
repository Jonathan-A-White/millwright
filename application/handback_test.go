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

var handedBackOn = time.Date(2026, 10, 10, 9, 30, 0, 0, time.UTC)

const handBackText = "HANDED BACK: no commits. The fix needs bsv-kit and the mill first."

func TestHandedBackIsTheLatestHandBackNobodyHasRePathed(t *testing.T) {
	say := func(text string, day int) application.Comment {
		return application.Comment{Text: text, Created: handedBackOn.AddDate(0, 0, day)}
	}
	cases := []struct {
		name     string
		comments []application.Comment
		want     bool
		at       time.Time
	}{
		{"no comments", nil, false, time.Time{}},
		{"other words only", []application.Comment{say("mw dispatch on vps started attempt 1", 0)}, false, time.Time{}},
		{"a hand-back", []application.Comment{say(handBackText, 0)}, true, handedBackOn},
		{"a hand-back, then mw's own words", []application.Comment{say(handBackText, 0), say("mw retry on vps gave back the claim", 1)}, true, handedBackOn},
		{"re-pathed after it", []application.Comment{say(handBackText, 0), say("RE-PATHED: bsv-kit landed", 1)}, false, time.Time{}},
		{"handed back again after a re-path", []application.Comment{say(handBackText, 0), say("RE-PATHED: x", 1), say(handBackText, 2)}, true, handedBackOn.AddDate(0, 0, 2)},
		{"a re-path before any hand-back", []application.Comment{say("RE-PATHED: x", 0)}, false, time.Time{}},
		{"the mark must open the comment", []application.Comment{say("I have not HANDED BACK anything", 0)}, false, time.Time{}},
		{"leading space is allowed", []application.Comment{say("  \nHANDED BACK\nno commits", 0)}, true, handedBackOn},
	}
	for _, c := range cases {
		at, got := application.HandedBack(c.comments)
		if got != c.want || !at.Equal(c.at) {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", c.name, at, got, c.at, c.want)
		}
	}
}

// aHandedBackStory leaves a ready story the Builder handed back on
// handedBackOn, the way mw-31qug1.6 stood on 2026-10-14.
func aHandedBackStory(t *testing.T, tracker *apptest.FakeTracker, id string) {
	t.Helper()
	tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "The story " + id})
	if err := tracker.CommentOnStoryAt(id, handBackText, handedBackOn); err != nil {
		t.Fatal(err)
	}
}

func TestADispatchDoesNotStartAStoryTheBuilderHandedBack(t *testing.T) {
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	aHandedBackStory(t, tracker, "mw-gq6.1")

	for tick := 1; tick <= 2; tick++ {
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if len(report.Started) != 0 {
			t.Fatalf("tick %d: started %v, want nothing", tick, startedIDs(report))
		}
		if len(report.Passed) != 1 || !strings.Contains(report.Passed[0].Why, "handed it back") || !strings.Contains(report.Passed[0].Why, "--repathed") {
			t.Fatalf("tick %d: the pass should say it was handed back and the way on, got %+v", tick, report.Passed)
		}
	}
	if d, _ := tracker.ShowStory(context.Background(), "mw-gq6.1"); d.Assignee != "" || d.Status != "open" {
		t.Errorf("the story should be left open and unclaimed, got %q %q", d.Status, d.Assignee)
	}
	if len(worktrees.added) != 0 || len(runner.Started()) != 0 {
		t.Errorf("a worktree or session was made: %v %v", worktrees.added, runner.Started())
	}
	if got := tracker.Comments("mw-gq6.1"); len(got) != 1 {
		t.Errorf("dispatch should write nothing on it, got %q", got)
	}
}

func TestADispatchStartsAHandedBackStoryOnceItIsRePathed(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aHandedBackStory(t, tracker, "mw-gq6.1")
	if err := tracker.CommentOnStoryAt("mw-gq6.1", "RE-PATHED: bsv-kit and the mill landed", handedBackOn.AddDate(0, 0, 4)); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
		t.Errorf("started %v, want the re-pathed story", ids)
	}
}

func TestADispatchLeavesAHandedBackStoryAloneInADryRunToo(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.DryRun = true
	aHandedBackStory(t, tracker, "mw-gq6.1")
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 0 || len(report.Passed) != 1 {
		t.Errorf("a dry run should pass it over, got started %v passed %+v", startedIDs(report), report.Passed)
	}
}

func TestRetryRefusesAStoryTheBuilderHandedBackAndChangesNothing(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	if err := tracker.CommentOnStoryAt("mw-gq6.1", handBackText, handedBackOn); err != nil {
		t.Fatal(err)
	}
	landing := &fakeRetryLanding{AheadCount: 1, BundleSHA: "deadbeef"}
	files := &apptest.FakeVaultFiles{}
	var out strings.Builder

	report, err := aRetry(tracker, landing, files, &out).Run(context.Background(), "mw-gq6.1")
	if err == nil || !report.Refused {
		t.Fatalf("expected a refusal, got %+v, %v", report, err)
	}
	for _, want := range []string{"handed it back", "2026-10-10", "--repathed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal should say %q, got %v", want, err)
		}
	}
	if landing.bundled || landing.removed || landing.branchDeleted || len(files.Commits()) != 0 {
		t.Errorf("a refusal changes nothing, got %+v", landing)
	}
	detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if detail.Assignee == "" {
		t.Errorf("the claim should be kept")
	}
	if got := tracker.Comments("mw-gq6.1"); len(got) != 1 {
		t.Errorf("a refusal writes no comment, got %q", got)
	}
}

func TestRetryRepathedWritesTheCommentAndGoesOn(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	if err := tracker.CommentOnStoryAt("mw-gq6.1", handBackText, handedBackOn); err != nil {
		t.Fatal(err)
	}
	landing := &fakeRetryLanding{AheadCount: 0}
	var out strings.Builder
	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
	retry.RePathed = "bsv-kit and the mill landed on main"

	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil || !report.ClaimReleased {
		t.Fatalf("expected the retry to go on, got %+v, %v\n%s", report, err, out.String())
	}
	comments := tracker.Comments("mw-gq6.1")
	if len(comments) < 2 || comments[1] != "RE-PATHED: bsv-kit and the mill landed on main" {
		t.Fatalf("the RE-PATHED: comment should follow the hand-back first, got %q", comments)
	}
	said := make([]application.Comment, len(comments))
	for i, text := range comments {
		said[i] = application.Comment{Text: text}
	}
	if _, handedBack := application.HandedBack(said); handedBack {
		t.Errorf("the story should no longer read as handed back: %q", comments)
	}
}

func TestRetryRepathedWritesNothingWhenTheRetryIsRefusedForAnotherReason(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	_ = tracker.CommentOnStoryAt("mw-gq6.1", handBackText, handedBackOn)
	runner := apptest.NewFakeRunner()
	if err := runner.Start(context.Background(), application.SessionSpec{Name: application.SessionName("mw-gq6.1"), Command: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	retry := aRetry(tracker, &fakeRetryLanding{}, &apptest.FakeVaultFiles{}, &strings.Builder{})
	retry.Runner = runner
	retry.RePathed = "it landed"
	if _, err := retry.Run(context.Background(), "mw-gq6.1"); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("expected the live session to refuse the retry, got %v", err)
	}
	if got := tracker.Comments("mw-gq6.1"); len(got) != 1 {
		t.Errorf("a refused retry writes no comment, got %q", got)
	}
}

func TestStatusListsAHandedBackStoryUnderItsHeadingWithTheDate(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.50", "Handed back", "vps")
	_ = tracker.CommentOnStoryAt("mw-gq6.50", handBackText, handedBackOn)
	storyOn(t, tracker, "mw-gq6.51", "Handed back, then re-pathed", "vps")
	_ = tracker.CommentOnStoryAt("mw-gq6.51", handBackText, handedBackOn)
	_ = tracker.CommentOnStoryAt("mw-gq6.51", "RE-PATHED: it landed", handedBackOn.AddDate(0, 0, 1))
	storyOn(t, tracker, "mw-gq6.52", "Never handed back", "vps")

	report := otherHostStatus(t, tracker)
	if len(report.HandedBack) != 1 || report.HandedBack[0].Detail.Story.ID != "mw-gq6.50" {
		t.Fatalf("handed back: %+v, want only mw-gq6.50", report.HandedBack)
	}
	text := report.String()
	for _, want := range []string{application.HandedBackHeading + " (1)", "mw-gq6.50", "2026-10-10", "mw retry mw-gq6.50 --repathed"} {
		if !strings.Contains(text, want) {
			t.Errorf("the report lacks %q:\n%s", want, text)
		}
	}
}

func TestStatusSaysNothingOfHandBacksWhenThereAreNone(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.52", "Never handed back", "vps")
	report := otherHostStatus(t, tracker)
	if len(report.HandedBack) != 0 || strings.Contains(report.String(), application.HandedBackHeading) {
		t.Errorf("zero should show nothing, got %+v\n%s", report.HandedBack, report.String())
	}
}

func TestRetryRepathedOfAnUnclaimedHandedBackStoryHasNoClaimToGiveBack(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-gq6", domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps",
	})
	aHandedBackStory(t, tracker, "mw-gq6.1")
	var out strings.Builder
	retry := aRetry(tracker, &fakeRetryLanding{NoWorktree: true}, &apptest.FakeVaultFiles{}, &out)

	if _, err := retry.Run(context.Background(), "mw-gq6.1"); err == nil || !strings.Contains(err.Error(), "--repathed") {
		t.Fatalf("expected the refusal naming --repathed, got %v", err)
	}
	retry.RePathed = "bsv-kit landed"
	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil || !report.ClaimReleased || !report.RePathed {
		t.Fatalf("expected the retry to go on, got %+v, %v\n%s", report, err, out.String())
	}
	if got := tracker.Comments("mw-gq6.1"); len(got) < 2 || got[1] != "RE-PATHED: bsv-kit landed" {
		t.Errorf("the RE-PATHED: comment should follow the hand-back, got %q", got)
	}
}
