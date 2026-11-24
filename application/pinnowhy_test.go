package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1702: a host pin with no host_why is moved to host=auto after an hour
// of no room (repath), so mw path set and mw release say so when they write or
// release one, and mw status counts them.

const pinWarnWords = "host_why"

func TestPathSetWarnsOfAHostPinWithNoHostWhyAndWritesItAll(t *testing.T) {
	for _, host := range []string{"laptop", "desktop", "cloud2"} {
		tracker := aVaultWithTwoFormulas()
		var errOut strings.Builder
		_, err := application.PathSet{Tracker: tracker, Err: &errOut, RepathNoRoom: 90 * time.Minute}.
			Run(context.Background(), "mw-gq6.2", "", []string{"host=" + host})
		if err != nil {
			t.Fatalf("host=%s: nothing is refused, got %v", host, err)
		}
		if got := storyHost(t, tracker, "mw-gq6.2"); got != host {
			t.Errorf("host=%s was written as %q", host, got)
		}
		for _, want := range []string{"WARN", "mw-gq6.2", "host=" + host, "host=auto", "1h30m", "repath_no_room", `host_why="<why>"`} {
			if !strings.Contains(errOut.String(), want) {
				t.Errorf("host=%s: the warning lacks %q:\n%s", host, want, errOut.String())
			}
		}
	}
}

func TestPathSetSaysNothingOfAHostPinThatHasAWhyOrIsNotAPin(t *testing.T) {
	cases := map[string][]string{
		"a host_why in the same command": {"host=laptop", "host_why=only the Laptop has Docker"},
		"host=auto":                      {"host=auto"},
		"no host key at all":             {"model=sonnet"},
	}
	for name, pairs := range cases {
		tracker := aVaultWithTwoFormulas()
		var errOut strings.Builder
		if _, err := (application.PathSet{Tracker: tracker, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "", pairs); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(errOut.String(), pinWarnWords) {
			t.Errorf("%s: a story that is no unexplained pin was warned of:\n%s", name, errOut.String())
		}
	}
	// A host_why the story already carries, or its epic's defaults do.
	tracker := aVaultWithTwoFormulas()
	pinnedTo(tracker, "mw-gq6.3", "laptop", domain.HostWhyField, "the Morpheus cache is on the Laptop")
	var errOut strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Err: &errOut}).Run(context.Background(), "mw-gq6.3", "", []string{"host=laptop", "model=sonnet"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut.String(), pinWarnWords) {
		t.Errorf("a story with its own host_why was warned of:\n%s", errOut.String())
	}
}

func TestPathSetAHostPinOnARigThatNeedsTheLaptopIsNoUnexplainedPin(t *testing.T) {
	tracker, rules := aFilesExpectedRig(false)
	rules.Require("millwright", domain.EpicRequirements{NeedsLaptop: true})
	var errOut strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "", []string{"host=laptop"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut.String(), pinWarnWords) {
		t.Errorf("a pin to the laptop on a rig that needs it was warned of:\n%s", errOut.String())
	}
}

func TestReleaseWarnsOfAStoryPinnedWithNoHostWhyAndReleasesIt(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	if _, err := (application.PathSet{Tracker: tracker}).Run(context.Background(), "mw-gq6.2", "", []string{"host=desktop", "branch=main"}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.HoldStory(context.Background(), "mw-gq6.2"); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if _, err := (application.Release{Tracker: tracker, Out: &out}).Run(context.Background(), "mw-gq6.2"); err != nil {
		t.Fatalf("a warning only, got %v", err)
	}
	for _, want := range []string{"WARN", "mw-gq6.2", "host=desktop", "host=auto", pinWarnWords} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the release lacks %q:\n%s", want, out.String())
		}
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2"); detail.Held() {
		t.Error("the warned story was not released")
	}
}

func TestReleaseOfAnEpicWarnsOnlyOfTheUnexplainedPins(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	pinnedTo(tracker, "mw-gq6.3", "desktop", domain.HostWhyField, "the GPU is there")
	for _, id := range []string{"mw-gq6.1", "mw-gq6.2", "mw-gq6.3"} {
		if err := tracker.HoldStory(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	if _, err := (application.Release{Tracker: tracker, Out: &out}).Run(context.Background(), "mw-gq6"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "WARN: mw-gq6.3") {
		t.Errorf("a pin with a host_why was warned of:\n%s", out.String())
	}
	// mw-gq6.1 and mw-gq6.2 take the epic's host=laptop with no reason.
	for _, id := range []string{"mw-gq6.1", "mw-gq6.2"} {
		if !strings.Contains(out.String(), "WARN: "+id+": ") {
			t.Errorf("%s was not warned of:\n%s", id, out.String())
		}
	}
}

func TestPathSetNamesHostWhyInItsKeysList(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	_, err := application.PathSet{Tracker: tracker}.Run(context.Background(), "mw-gq6.2", "", nil)
	if err == nil || !strings.Contains(err.Error(), "host_why") {
		t.Errorf("the keys list of a bare call lacks host_why: %v", err)
	}
	_, err = application.PathSet{Tracker: tracker}.Run(context.Background(), "mw-gq6.2", "", []string{"nokey=1"})
	if err == nil || !strings.Contains(err.Error(), "host_why") {
		t.Errorf("the keys list of an unknown key lacks host_why: %v", err)
	}
	_, err = application.PathSet{Tracker: tracker}.Run(context.Background(), "mw-gq6.2", "", []string{"host="})
	if err == nil || !strings.Contains(err.Error(), "host_why") {
		t.Errorf("the keys list of an empty value lacks host_why: %v", err)
	}
}

func TestStatusCountsTheOpenStoriesPinnedWithNoHostWhyAsANote(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.70", "Pinned, no why", "laptop")
	storyOn(t, tracker, "mw-gq6.71", "Pinned, no why either", "desktop")
	storyOn(t, tracker, "mw-gq6.72", "Pinned with a why", "desktop")
	storyOn(t, tracker, "mw-gq6.73", "Free", domain.HostAuto)
	if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.72", map[string]string{domain.HostWhyField: "the GPU is on the desktop"}); err != nil {
		t.Fatal(err)
	}
	report := retiredStatus(t, tracker)
	if got := strings.Join(report.PinsNoWhy, ","); got != "mw-gq6.70,mw-gq6.71" {
		t.Errorf("PinsNoWhy = %q, want the two with no host_why", got)
	}
	text := strings.Join(strings.Fields(report.String()), " ")
	if !strings.Contains(text, "2 open stories are pinned to a host with no host_why") {
		t.Errorf("the report should count them:\n%s", text)
	}
	if strings.Contains(text, "warning:") {
		t.Errorf("a note, not a warning:\n%s", text)
	}
}

func TestStatusSaysNothingWhenEveryPinHasAWhy(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.72", "Pinned with a why", "desktop")
	storyOn(t, tracker, "mw-gq6.73", "Free", domain.HostAuto)
	if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.72", map[string]string{domain.HostWhyField: "the GPU is on the desktop"}); err != nil {
		t.Fatal(err)
	}
	report := retiredStatus(t, tracker)
	if len(report.PinsNoWhy) != 0 || strings.Contains(report.String(), "host_why") {
		t.Errorf("nothing to say, got %v:\n%s", report.PinsNoWhy, report.String())
	}
}
