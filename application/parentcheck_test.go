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

// A listing tells a parent from a story by one shared check, StoryDetail.
// IsParent: a bead with children, or one typed epic. A task-typed bead with
// children read as a ready story in each listing that tested the type alone
// (mw-gq6.1092 to .1095, after mw-gq6.1086, .1088 and .1089).

func TestIsParentIsAnEpicOrAnythingWithChildren(t *testing.T) {
	for name, tc := range map[string]struct {
		d    application.StoryDetail
		want bool
	}{
		"a plain task":                {application.StoryDetail{Type: "task"}, false},
		"a task with children":        {application.StoryDetail{Type: "task", Children: 2}, true},
		"a feature with children":     {application.StoryDetail{Type: "feature", Children: 1}, true},
		"typed epic":                  {application.StoryDetail{Type: " Epic "}, true},
		"flagged epic":                {application.StoryDetail{IsEpic: true}, true},
		"an untyped bead no children": {application.StoryDetail{}, false},
	} {
		if got := tc.d.IsParent(); got != tc.want {
			t.Errorf("%s: IsParent = %v, want %v", name, got, tc.want)
		}
	}
}

// parentOf is a ready task-typed bead of rig millwright on vps with children
// open under it.
func parentOf(id string, children int) application.StoryDetail {
	d := damperStory(id, 2, 3*time.Hour)
	d.Type = "task"
	d.Children = children
	return d
}

func TestStatusReadyNeitherListsNorCountsABeadWithChildren(t *testing.T) {
	ctx := context.Background()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story", "vps")
	storyOn(t, tracker, "mw-gq6.31", "A parent", "vps")
	tracker.AddStory("mw-gq6.31", domain.Story{ID: "mw-gq6.31.1", Title: "Its child"})

	report, err := application.Status{Tracker: tracker, Host: "vps", Seat: "builder", Now: func() time.Time { return statusNow }}.Run(ctx)
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	for _, d := range report.Ready {
		if d.Story.ID == "mw-gq6.31" {
			t.Fatalf("the parent mw-gq6.31 is listed as a ready story: %+v", report.Ready)
		}
	}
	if len(report.Ready) == 0 {
		t.Fatalf("expected the plain story still ready, got none")
	}
}

func TestOpenRefusedStoriesSkipsAParentWithARefusedRunState(t *testing.T) {
	parent := parentOf("mw-q.9", 2)
	parent.Labels = []string{application.RunState + ":" + application.RunBlocked}
	parent.Updated = stuckNow.Add(-3 * time.Hour)
	story := refusedOpen("mw-q.3", application.RunBlocked, 3*time.Hour)
	for name, work := range map[string]application.WorkInHand{
		"ready":   {Ready: []application.StoryDetail{parent, story}},
		"claimed": {OpenClaimed: []application.StoryDetail{parent, story}},
	} {
		got := application.OpenRefusedStories(work, stuckNow)
		if len(got) != 1 || got[0].Story.Story.ID != "mw-q.3" {
			t.Errorf("%s: expected only mw-q.3, got %+v", name, got)
		}
	}
}

func TestTheStarvedAlarmDoesNotCountAParentAsReadyWork(t *testing.T) {
	rig := application.StarvedRig{Rig: "millwright", Since: statusNow.Add(-4 * time.Hour), Hold: "smoke hold", Reason: "x", Host: "vps"}
	parent := parentOf("mw-q.9", 2)
	story := damperStory("mw-q.3", 2, time.Hour)
	// Only the parent is ready: nothing a session can take, so no alarm.
	if alarms := application.StarvedAlarms([]application.StarvedRig{rig}, application.WorkInHand{Ready: []application.StoryDetail{parent}}, statusNow); len(alarms) != 0 {
		t.Errorf("expected no alarm with only a parent ready, got %+v", alarms)
	}
	// A parent running holds no session either, so it does not clear the alarm.
	running := parentOf("mw-q.8", 1)
	work := application.WorkInHand{Ready: []application.StoryDetail{parent, story}, OpenClaimed: []application.StoryDetail{running}}
	alarms := application.StarvedAlarms([]application.StarvedRig{rig}, work, statusNow)
	if len(alarms) != 1 || len(alarms[0].Beads) != 1 || alarms[0].Beads[0] != "mw-q.3" || !strings.Contains(alarms[0].Line(), "1 ready") {
		t.Errorf("expected one alarm counting only mw-q.3, got %+v", alarms)
	}
}

func TestTheMeteredPinnedAlarmDoesNotCountAParent(t *testing.T) {
	pinned := func(d application.StoryDetail) application.StoryDetail {
		d.Defaults = domain.Path{Rig: "millwright", Host: "laptop"}
		return d
	}
	heavy := map[string]bool{"millwright": true}
	parent := pinned(parentOf("mw-q.9", 2))
	story := pinned(damperStory("mw-q.3", 2, time.Hour))
	if _, found := application.MeteredPinned(application.WorkInHand{Ready: []application.StoryDetail{parent}}, "laptop", heavy); found {
		t.Errorf("expected no alarm with only a parent pinned")
	}
	alarm, found := application.MeteredPinned(application.WorkInHand{Ready: []application.StoryDetail{parent, story}}, "laptop", heavy)
	if !found || len(alarm.Beads) != 1 || alarm.Beads[0] != "mw-q.3" {
		t.Errorf("expected only mw-q.3, got %+v", alarm)
	}
}

func TestABriefShowsAChildThatIsItselfAParentAsAParentLine(t *testing.T) {
	fake := apptest.NewFakeTracker()
	fake.AddEpic("mw-big", domain.Path{})
	fake.AddStory("mw-big", domain.Story{ID: "mw-big.1", Title: "A plain story"})
	fake.AddStory("mw-big", domain.Story{ID: "mw-big.2", Title: "A task with children"})
	fake.AddStory("mw-big.2", domain.Story{ID: "mw-big.2.1", Title: "Grandchild one"})
	fake.AddStory("mw-big.2", domain.Story{ID: "mw-big.2.2", Title: "Grandchild two"})

	var out strings.Builder
	report, err := application.Brief{Tracker: fake, Out: &out}.Run(context.Background(), "mw-big")
	if err != nil {
		t.Fatalf("briefing: %v", err)
	}
	one := report.Beads[0]
	for _, line := range append(append([]application.BriefLine{}, one.Open...), one.Frontier()...) {
		if line.Story.Story.ID == "mw-big.2" {
			t.Fatalf("the parent mw-big.2 is among the open or frontier lines: %+v", one.Open)
		}
	}
	if len(one.Parents) != 1 || one.Parents[0].Story.Story.ID != "mw-big.2" {
		t.Fatalf("expected mw-big.2 as the one parent line, got %+v", one.Parents)
	}
	text := out.String()
	if !strings.Contains(text, "Parents (1)") || !strings.Contains(text, "parent of 2") {
		t.Errorf("expected a Parents group saying 'parent of 2', got:\n%s", text)
	}
	if !strings.Contains(text, "Open (1)") {
		t.Errorf("expected the plain story alone under Open (1), got:\n%s", text)
	}
}
