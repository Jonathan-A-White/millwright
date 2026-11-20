package application_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// laneStory is a ready story of a rig, below P1: a feature, or a bug (a fix).
func laneStory(id, rig string, priority int, labels ...string) application.StoryDetail {
	d := damperStory(id, priority, time.Hour, labels...)
	d.Defaults = domain.Path{Rig: rig}
	return d
}

func laneFacts(rig string, features, fixes int) application.DamperFacts {
	facts := aCleanDamperFixture()
	for n := 0; n < features; n++ {
		facts.Work.Ready = append(facts.Work.Ready, laneStory(fmt.Sprintf("%s-f.%d", rig, n), rig, 2))
	}
	for n := 0; n < fixes; n++ {
		facts.Work.Ready = append(facts.Work.Ready, laneStory(fmt.Sprintf("%s-b.%d", rig, n), rig, 3, application.LabelBug))
	}
	return facts
}

func floorLines(report application.DampersReport) []string {
	var lines []string
	for _, a := range report.Alarms {
		if a.Principle == application.DamperFeatureFloor {
			lines = append(lines, a.Line())
		}
	}
	return lines
}

func TestDampersAlarmARigUnderItsFeatureFloor(t *testing.T) {
	facts := laneFacts("lampas", 4, 0)
	facts.FeatureFloor = map[string]int{"lampas": 5}
	lines := floorLines(application.ReadDampers(facts, application.DamperLimits{}))
	if len(lines) != 1 {
		t.Fatalf("want one feature floor alarm, got %q", lines)
	}
	want := "feature floor: lampas: 4 features ready, floor 5: carve or release before a fix"
	if lines[0] != want {
		t.Errorf("the alarm reads %q, want %q", lines[0], want)
	}
}

func TestDampersAreQuietOnceTheRigHasItsFloorOfFeatures(t *testing.T) {
	facts := laneFacts("lampas", 6, 0)
	facts.FeatureFloor = map[string]int{"lampas": 5}
	if lines := floorLines(application.ReadDampers(facts, application.DamperLimits{})); len(lines) != 0 {
		t.Errorf("a rig with its floor alarmed: %q", lines)
	}
}

func TestDampersAlarmARigWithNoReadyFeatureAtAll(t *testing.T) {
	facts := aCleanDamperFixture()
	facts.FeatureFloor = map[string]int{"postern": 2, "trade-tracker": 2}
	lines := floorLines(application.ReadDampers(facts, application.DamperLimits{}))
	if len(lines) != 2 || !strings.Contains(lines[0], "postern: 0 features ready, floor 2") {
		t.Errorf("want an alarm for each rig, in name order, got %q", lines)
	}
}

func TestDampersCountOnlyReadyFeaturesBelowP1AndWhatAFeatureWaitsOn(t *testing.T) {
	facts := laneFacts("lampas", 1, 0)
	facts.Work.Ready = append(facts.Work.Ready,
		laneStory("lampas-p1", "lampas", 1),
		laneStory("lampas-held", "lampas", 2, domain.HeldForPrefix+"event"),
		laneStory("lampas-hitl", "lampas", 2, application.LabelHitl),
		laneStory("lampas-other", "otherrig", 2),
		laneStory("lampas-need", "lampas", 3, application.LabelBug),
	)
	// a feature that is not ready waits on the bug: the bug is in the feature lane.
	facts.Graph = append(facts.Graph,
		graphBead("lampas-wait", 2, "open", nil, "lampas-need"))
	facts.FeatureFloor = map[string]int{"lampas": 5}
	lines := floorLines(application.ReadDampers(facts, application.DamperLimits{}))
	if len(lines) != 1 || !strings.Contains(lines[0], "lampas: 2 features ready, floor 5") {
		t.Errorf("want 2 features (one plain, one a feature waits on), got %q", lines)
	}
}

func fixLines(report application.DampersReport) []string {
	var lines []string
	for _, a := range report.Alarms {
		if a.Principle == application.DamperFixQueue {
			lines = append(lines, a.Line())
		}
	}
	return lines
}

func TestDampersAlarmARigWithMoreReadyFixesThanTheLimit(t *testing.T) {
	facts := laneFacts("lampas", 0, 11)
	lines := fixLines(application.ReadDampers(facts, application.DamperLimits{}))
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "fix queue: lampas: 11 fixes ready, limit 10: release none; fold or ice") {
		t.Fatalf("want one fix queue alarm, got %q", lines)
	}
	if !strings.Contains(lines[0], "+6 (all: mw stuck --dampers)") {
		t.Errorf("the line names five fixes and counts the rest: %q", lines[0])
	}
}

func TestDampersAreQuietAtTheFixLimitAndTheLimitIsAKnob(t *testing.T) {
	facts := laneFacts("lampas", 0, 10)
	if lines := fixLines(application.ReadDampers(facts, application.DamperLimits{})); len(lines) != 0 {
		t.Errorf("a rig at its limit alarmed: %q", lines)
	}
	if lines := fixLines(application.ReadDampers(facts, application.DamperLimits{ReadyFixesPerRig: 9})); len(lines) != 1 {
		t.Errorf("a limit of 9 should alarm a rig with 10 fixes: %q", lines)
	}
}
