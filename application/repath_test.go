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

var repathNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// aRePathFactory is a dispatch on the laptop that starts nothing (cap 0), so a
// story's host is judged by the re-path alone, with desktop and vps retired.
func aRePathFactory(t *testing.T) (application.Dispatch, *apptest.FakeTracker) {
	t.Helper()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Host = "laptop"
	dispatch.Cap = 0
	dispatch.Retired = []string{"desktop", "vps"}
	dispatch.Now = func() time.Time { return repathNow }
	return dispatch, tracker
}

func pinnedTo(tracker *apptest.FakeTracker, id, host string, extra ...string) {
	path := domain.Path{Host: host}
	for i := 0; i+1 < len(extra); i += 2 {
		_ = path.Set(extra[i], extra[i+1])
	}
	tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "pinned to " + host, Overrides: path})
}

func storyHost(t *testing.T, tracker *apptest.FakeTracker, id string) string {
	t.Helper()
	detail, err := tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return detail.Merged().Host
}

func rePathedComments(tracker *apptest.FakeTracker, id string) []string {
	var found []string
	for _, c := range tracker.Comments(id) {
		if strings.HasPrefix(c, application.RePathedMark) {
			found = append(found, c)
		}
	}
	return found
}

// AC 1: an open story whose host is retired goes to host=auto, with a
// RE-PATHED: comment naming the old host and why.
func TestADispatchRePathsAnOpenStoryPinnedToARetiredHost(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	pinnedTo(tracker, "mw-gq6.1", "desktop")

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := storyHost(t, tracker, "mw-gq6.1"); got != domain.HostAuto {
		t.Errorf("host = %q, want auto", got)
	}
	comments := rePathedComments(tracker, "mw-gq6.1")
	if len(comments) != 1 {
		t.Fatalf("want one RE-PATHED: comment, got %q", tracker.Comments("mw-gq6.1"))
	}
	for _, want := range []string{"desktop", "retired", application.RetiredHostsKey} {
		if !strings.Contains(comments[0], want) {
			t.Errorf("the comment lacks %q: %q", want, comments[0])
		}
	}
	if len(report.Repathed) != 1 || report.Repathed[0].StoryID != "mw-gq6.1" || report.Repathed[0].From != "desktop" {
		t.Errorf("the report should name the re-path, got %+v", report.Repathed)
	}
	if !strings.Contains(report.String(), "re-pathed mw-gq6.1 from desktop") {
		t.Errorf("the report should say it, got:\n%s", report.String())
	}
}

// AC 1: a cloud box no longer in the cloud book is gone.
func TestADispatchRePathsAnOpenStoryPinnedToACloudBoxThatIsGone(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	dispatch.CloudBook = &apptest.FakeCloudBook{State: application.CloudState{Boxes: []application.CloudBox{{Name: "cloud2", Up: repathNow.Add(-time.Hour)}}}}
	pinnedTo(tracker, "mw-gq6.1", "cloud1")
	pinnedTo(tracker, "mw-gq6.2", "cloud2")
	pinnedTo(tracker, "mw-gq6.3", "laptop")

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := storyHost(t, tracker, "mw-gq6.1"); got != domain.HostAuto {
		t.Errorf("the story on the gone box: host = %q, want auto", got)
	}
	comments := rePathedComments(tracker, "mw-gq6.1")
	if len(comments) != 1 || !strings.Contains(comments[0], "cloud1") || !strings.Contains(comments[0], "cloud book") {
		t.Errorf("want a RE-PATHED: comment naming cloud1 and the book, got %q", comments)
	}
	for id, want := range map[string]string{"mw-gq6.2": "cloud2", "mw-gq6.3": "laptop"} {
		if got := storyHost(t, tracker, id); got != want {
			t.Errorf("%s: host = %q, want %q left alone", id, got, want)
		}
	}
}

// A cloud book that cannot be read says nothing is gone.
func TestADispatchWithNoCloudBookLeavesABoxPinAlone(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	pinnedTo(tracker, "mw-gq6.1", "cloud1")
	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := storyHost(t, tracker, "mw-gq6.1"); got != "cloud1" {
		t.Errorf("host = %q, want cloud1 left alone", got)
	}
}

// AC 4: held and in-progress stories are never touched, and nothing is filed.
func TestADispatchRePathsNeitherAHeldNorAClaimedStory(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	pinnedTo(tracker, "mw-gq6.1", "desktop")
	pinnedTo(tracker, "mw-gq6.2", "desktop")
	if err := tracker.HoldStory(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ClaimAs("mw-gq6.2", "mw@desktop", repathNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"mw-gq6.1", "mw-gq6.2"} {
		if got := storyHost(t, tracker, id); got != "desktop" {
			t.Errorf("%s: host = %q, want desktop untouched", id, got)
		}
		if c := rePathedComments(tracker, id); len(c) != 0 {
			t.Errorf("%s: a RE-PATHED: comment on a story the Mayor holds or a session works: %q", id, c)
		}
	}
}

// AC 3: a pin that says why it must stay is never moved.
func TestADispatchNeverMovesAPinThatSaysWhyItMustStay(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	pinnedTo(tracker, "mw-gq6.1", "desktop", domain.HostWhyField, "the GPU is on the desktop")

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := storyHost(t, tracker, "mw-gq6.1"); got != "desktop" {
		t.Errorf("host = %q, want desktop kept by its why", got)
	}
	if c := rePathedComments(tracker, "mw-gq6.1"); len(c) != 0 {
		t.Errorf("a pin that stays is not commented on: %q", c)
	}
}

// AC 3: host=laptop on a rig whose file says it needs the laptop stays.
func TestADispatchNeverMovesALaptopPinOnARigThatNeedsTheLaptop(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	dispatch.Retired = []string{"laptop"}
	dispatch.Host = "cloud1"
	rules := apptest.NewFakeEpicRules()
	rules.Require("millwright", domain.EpicRequirements{NeedsLaptop: true})
	dispatch.Rules = rules
	pinnedTo(tracker, "mw-gq6.1", "laptop")

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := storyHost(t, tracker, "mw-gq6.1"); got != "laptop" {
		t.Errorf("host = %q, want laptop kept: the rig needs it", got)
	}
}

func noRoomFor(host string, since time.Time) (string, string) {
	return application.HostNoRoomKey(host), application.FormatHostNoRoom(application.HostNoRoom{Since: since, Why: "load 25 on 8 cores"})
}

// AC 2: a story whose pinned host has had no room for over 60 minutes moves,
// when this host passes its gates and has room now.
func TestADispatchRePathsAStoryWhosePinnedHostHasHadNoRoomOverAnHour(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	dispatch.Retired = nil
	dispatch.Memory = tracker
	pinnedTo(tracker, "mw-gq6.1", "desktop")
	pinnedTo(tracker, "mw-gq6.2", "cloud1")
	key, value := noRoomFor("desktop", repathNow.Add(-61*time.Minute))
	_ = tracker.SetNote(context.Background(), key, value)
	key, value = noRoomFor("cloud1", repathNow.Add(-59*time.Minute))
	_ = tracker.SetNote(context.Background(), key, value)

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := storyHost(t, tracker, "mw-gq6.1"); got != domain.HostAuto {
		t.Errorf("full for 61 minutes: host = %q, want auto", got)
	}
	comments := rePathedComments(tracker, "mw-gq6.1")
	if len(comments) != 1 || !strings.Contains(comments[0], "desktop") || !strings.Contains(comments[0], "no room") {
		t.Errorf("want a RE-PATHED: comment naming desktop and no room, got %q", comments)
	}
	if got := storyHost(t, tracker, "mw-gq6.2"); got != "cloud1" {
		t.Errorf("full for 59 minutes: host = %q, want cloud1 left alone", got)
	}
}

func TestTheNoRoomLimitIsAKnob(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	dispatch.Retired = nil
	dispatch.Memory = tracker
	dispatch.RepathNoRoom = 10 * time.Minute
	pinnedTo(tracker, "mw-gq6.1", "desktop")
	key, value := noRoomFor("desktop", repathNow.Add(-11*time.Minute))
	_ = tracker.SetNote(context.Background(), key, value)
	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := storyHost(t, tracker, "mw-gq6.1"); got != domain.HostAuto {
		t.Errorf("host = %q, want auto after 11 minutes of a 10 minute limit", got)
	}
}

// AC 2: only if another host that passes its gates has room now.
func TestADispatchLeavesAFullHostsStoryWhereNoOtherHostHasRoom(t *testing.T) {
	key, value := noRoomFor("desktop", repathNow.Add(-3*time.Hour))
	for name, arrange := range map[string]func(*application.Dispatch){
		"this host has no room": func(d *application.Dispatch) {
			d.Load = &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 40, Cores: 8}}
		},
		"this host has not the rig": func(d *application.Dispatch) { d.Rigs = map[string]string{} },
		"this host is the full one": func(d *application.Dispatch) { d.Host = "desktop" },
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker := aRePathFactory(t)
			dispatch.Retired = nil
			dispatch.Memory = tracker
			arrange(&dispatch)
			pinnedTo(tracker, "mw-gq6.1", "desktop")
			_ = tracker.SetNote(context.Background(), key, value)
			if _, err := dispatch.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := storyHost(t, tracker, "mw-gq6.1"); got != "desktop" {
				t.Errorf("host = %q, want desktop: nothing else can take it", got)
			}
		})
	}
}

// A dry run writes nothing.
func TestADryRunRePathsNothing(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	dispatch.DryRun = true
	pinnedTo(tracker, "mw-gq6.1", "desktop")
	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := storyHost(t, tracker, "mw-gq6.1"); got != "desktop" {
		t.Errorf("host = %q, a dry run writes nothing", got)
	}
}

// A host with no room keeps the note of since when, once, and drops it when it
// has room again.
func TestADispatchKeepsWhenItLostItsRoomAndDropsItWhenItIsBack(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	dispatch.Retired = nil
	dispatch.Memory = tracker
	dispatch.Cap = 1
	busy := &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 40, Cores: 8}}
	dispatch.Load = busy
	ctx := context.Background()

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatal(err)
	}
	was, _ := tracker.Note(ctx, application.HostNoRoomKey("laptop"))
	if got := application.ParseHostNoRoom(was); !got.Since.Equal(repathNow) || got.Why == "" {
		t.Fatalf("the note should say since %s and why, got %q", repathNow, was)
	}
	dispatch.Now = func() time.Time { return repathNow.Add(20 * time.Minute) }
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if again, _ := tracker.Note(ctx, application.HostNoRoomKey("laptop")); application.ParseHostNoRoom(again).Since != application.ParseHostNoRoom(was).Since {
		t.Errorf("the time it lost its room moved: %q then %q", was, again)
	}
	busy.Reading = application.LoadReading{Load: 0.5, Cores: 8}
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if left, _ := tracker.Note(ctx, application.HostNoRoomKey("laptop")); left != "" {
		t.Errorf("the host has room: the note should be gone, got %q", left)
	}
}

// AC 3: the pin that must stay, and why, as mw status names it.
func TestPinWhyIsTheReasonAPinMustStay(t *testing.T) {
	for name, tc := range map[string]struct {
		path  domain.Path
		rules domain.EpicRequirements
		want  string
	}{
		"a host_why":                 {domain.Path{Host: "desktop", HostWhy: "the GPU"}, domain.EpicRequirements{}, "the GPU"},
		"the laptop and a rig on it": {domain.Path{Host: "laptop", Rig: "lampas"}, domain.EpicRequirements{NeedsLaptop: true}, "the rig lampas needs the laptop"},
		"the laptop and a free rig":  {domain.Path{Host: "laptop", Rig: "lampas"}, domain.EpicRequirements{}, ""},
		"another host, a laptop rig": {domain.Path{Host: "desktop", Rig: "lampas"}, domain.EpicRequirements{NeedsLaptop: true}, ""},
	} {
		if got := application.PinWhy(tc.path, tc.rules); got != tc.want {
			t.Errorf("%s: PinWhy = %q, want %q", name, got, tc.want)
		}
	}
}

// AC 5: mw path set never writes a retired host or a gone box.
func TestPathSetCopiedFromAStoryPinnedToARetiredHostWritesAuto(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.3", Title: "on the desktop",
		Overrides: domain.Path{Rig: "millwright", Host: "desktop"}})
	var out strings.Builder
	path, err := application.PathSet{Tracker: tracker, Retired: []string{"desktop"}, Out: &out}.
		Run(context.Background(), "mw-gq6.2", "mw-gq6.3", nil)
	if err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if path.Host != domain.HostAuto {
		t.Errorf("host = %q, want auto", path.Host)
	}
	if got := storyHost(t, tracker, "mw-gq6.2"); got != domain.HostAuto {
		t.Errorf("the story's host = %q, want auto", got)
	}
	for _, want := range []string{"desktop", "retired", "host=auto"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the output should say %q:\n%s", want, out.String())
		}
	}
}

func TestPathSetNamingARetiredHostOrAGoneBoxWritesAutoAndSaysSo(t *testing.T) {
	book := &apptest.FakeCloudBook{State: application.CloudState{Boxes: []application.CloudBox{{Name: "cloud2"}}}}
	for host, kept := range map[string]bool{"desktop": false, "vps": false, "cloud1": false, "cloud2": true, "laptop": true} {
		tracker := aVaultWithTwoFormulas()
		var out strings.Builder
		_, err := application.PathSet{Tracker: tracker, Retired: []string{"desktop", "vps"}, CloudBook: book, Out: &out}.
			Run(context.Background(), "mw-gq6.2", "", []string{"host=" + host})
		if err != nil {
			t.Fatalf("host=%s: %v", host, err)
		}
		got := storyHost(t, tracker, "mw-gq6.2")
		if kept && got != host {
			t.Errorf("host=%s: wrote %q, want it kept", host, got)
		}
		if !kept {
			if got != domain.HostAuto {
				t.Errorf("host=%s: wrote %q, want auto", host, got)
			}
			if !strings.Contains(out.String(), host) || !strings.Contains(out.String(), "host=auto") {
				t.Errorf("host=%s: the output should name the host and host=auto:\n%s", host, out.String())
			}
		}
	}
}

// AC 3: mw status names a pin that stays, on a host that cannot take it.
func TestStatusNamesAPinThatStaysOnARetiredHostAsHeldByItsPin(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.60", "Needs the GPU", "desktop")
	storyOn(t, tracker, "mw-gq6.61", "Free to move", "desktop")
	if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.60", map[string]string{domain.HostWhyField: "the GPU is on the desktop"}); err != nil {
		t.Fatal(err)
	}
	report := retiredStatus(t, tracker, "desktop")
	var pins map[string]string
	for _, w := range report.Others {
		if w.Host == "desktop" {
			pins = w.Pins
		}
	}
	if got := pins["mw-gq6.60"]; got != "held by its pin: the GPU is on the desktop" {
		t.Errorf("Pins = %v", pins)
	}
	if got := pins["mw-gq6.61"]; got != "" {
		t.Errorf("a pin that can move is not held by its pin, got %q", got)
	}
	if !strings.Contains(report.String(), "held by its pin: the GPU is on the desktop") {
		t.Errorf("the report should print it:\n%s", report.String())
	}
}

// AC 5: a plan filed with a retired host or a gone box is filed with host=auto
// in its place, and the filing says so; the plan given is not changed.
func TestFilingAPlanPinnedToARetiredHostOrAGoneBoxWritesAuto(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	plan := twoStoryPlan() // its epic defaults say host vps
	plan.Stories[0].Overrides.Host = "cloud9"
	var warned strings.Builder

	filed, err := application.File{
		Tracker:   tracker,
		Retired:   []string{"vps"},
		CloudBook: &apptest.FakeCloudBook{},
		Err:       &warned,
	}.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("filing the plan: %v", err)
	}
	for _, story := range filed.Stories {
		if story.Path.Host != domain.HostAuto {
			t.Errorf("%s was filed with host=%s, want auto", story.Key, story.Path.Host)
		}
	}
	for _, want := range []string{"host=vps", "host=cloud9", "host=auto was written instead"} {
		if !strings.Contains(warned.String(), want) {
			t.Errorf("the warning should say %q:\n%s", want, warned.String())
		}
	}
	if plan.Epic.Defaults.Host != "vps" || plan.Stories[0].Overrides.Host != "cloud9" {
		t.Errorf("the plan given was changed: %+v", plan)
	}
}

// A deferred story pinned to a box the book lacks keeps its pin while held;
// once the Mayor releases it, a dispatch tick re-paths it with a RE-PATHED:
// comment, so nothing waits for ever.
func TestADispatchRePathsAnUnHeldStoryPinnedToAGoneBox(t *testing.T) {
	dispatch, tracker := aRePathFactory(t)
	dispatch.CloudBook = &apptest.FakeCloudBook{State: application.CloudState{Boxes: []application.CloudBox{{Name: "cloud2", Up: repathNow.Add(-time.Hour)}}}}
	pinnedTo(tracker, "mw-gq6.1", "cloud1")
	if err := tracker.HoldStory(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := storyHost(t, tracker, "mw-gq6.1"); got != "cloud1" {
		t.Fatalf("held: host = %q, want cloud1 kept", got)
	}
	if err := tracker.ReleaseStory(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := storyHost(t, tracker, "mw-gq6.1"); got != domain.HostAuto {
		t.Errorf("released: host = %q, want auto", got)
	}
	if c := rePathedComments(tracker, "mw-gq6.1"); len(c) != 1 || !strings.Contains(c[0], "cloud1") {
		t.Errorf("want one RE-PATHED: comment naming cloud1, got %q", c)
	}
}
