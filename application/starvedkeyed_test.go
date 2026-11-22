package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1659: a story that needs a grist key is passed over by the host with no
// key for that, and by the host that has it for its own room. The alarm named
// the keyless host's reason, which reads as if no host could take the story.
func twoHostsOneKeylessOneFull(t *testing.T, laptop application.StarvedRig) *apptest.FakeTracker {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.1", "One", "auto")
	notes := map[string]application.StarvedRig{
		"cloud3": {Rig: "millwright", Since: statusNow.Add(-4 * time.Hour), Hold: application.StarvedHolds["no-grist-key"], Reason: "its checks run mw grist eval, and cloud3 has no grist test key (grist_test_key_file in the config file): it waits for a host that has one"},
		"laptop": laptop,
	}
	for host, note := range notes {
		if err := tracker.SetNote(context.Background(), application.StarvedKey(host), application.FormatStarved([]application.StarvedRig{note})); err != nil {
			t.Fatal(err)
		}
	}
	return tracker
}

func laptopHolding(hold, reason string) application.StarvedRig {
	return application.StarvedRig{Rig: "millwright", Since: statusNow.Add(-3 * time.Hour), Hold: application.StarvedHolds[hold], Reason: reason}
}

func TestAStarvedAlarmNamesTheKeyedHostsNoRoomNotTheKeylessHostsMissingKey(t *testing.T) {
	alarms := starvedAlarms(t, twoHostsOneKeylessOneFull(t, laptopHolding("no-room", "laptop has no room: load 20.4 of 20 cores")))
	if len(alarms) != 1 {
		t.Fatalf("expected one alarm, got %+v", alarms)
	}
	for _, want := range []string{"waits for laptop: no room", "load 20.4 of 20"} {
		if !strings.Contains(alarms[0].Text, want) {
			t.Errorf("expected %q in the alarm, got %q", want, alarms[0].Text)
		}
	}
	if strings.Contains(alarms[0].Text, "grist") || strings.Contains(alarms[0].Text, "cloud3") {
		t.Errorf("expected the keyless host's reason left out, got %q", alarms[0].Text)
	}
	if !alarms[0].Wait {
		t.Errorf("expected a wait for the keyed host's room, not a fault: %+v", alarms[0])
	}
}

func TestAStarvedAlarmNamesTheKeyedHostsCapNotTheKeylessHostsMissingKey(t *testing.T) {
	alarms := starvedAlarms(t, twoHostsOneKeylessOneFull(t, laptopHolding("cap", "laptop has taken 4 of the 4 sessions it may run at once")))
	if len(alarms) != 1 || !strings.Contains(alarms[0].Text, "waits for laptop: cap") || strings.Contains(alarms[0].Text, "grist") || !alarms[0].Wait {
		t.Errorf("expected the laptop's cap as a wait, got %+v", alarms)
	}
}

func TestAStarvedAlarmStillNamesBothHostsWhenTheKeyedHostIsHeldByAFault(t *testing.T) {
	alarms := starvedAlarms(t, twoHostsOneKeylessOneFull(t, laptopHolding("smoke-hold", "the grist smoke of cairn failed")))
	if len(alarms) != 1 {
		t.Fatalf("expected one alarm, got %+v", alarms)
	}
	for _, want := range []string{"cloud3: no grist key", "laptop: smoke hold"} {
		if !strings.Contains(alarms[0].Text, want) {
			t.Errorf("expected %q in the alarm, got %q", want, alarms[0].Text)
		}
	}
	if strings.Contains(alarms[0].Text, "waits for laptop") || alarms[0].Wait {
		t.Errorf("expected a fault with no waits-for, got %+v", alarms[0])
	}
}

// A story pinned to the laptop is passed over by every other host as "another
// host", and by the laptop for its room: the room is the block.
func TestAStarvedAlarmNamesThePinnedHostsNoRoomNotAnotherHostsPassingItOver(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.1", "One", "laptop")
	notes := map[string]application.StarvedRig{
		"cloud3": {Rig: "millwright", Since: statusNow.Add(-4 * time.Hour), Hold: application.StarvedHolds["other-host"], Reason: "it is worked on laptop"},
		"laptop": laptopHolding("no-room", "laptop has no room: load 20.4 of 20 cores"),
	}
	for host, note := range notes {
		if err := tracker.SetNote(context.Background(), application.StarvedKey(host), application.FormatStarved([]application.StarvedRig{note})); err != nil {
			t.Fatal(err)
		}
	}
	alarms := starvedAlarms(t, tracker)
	if len(alarms) != 1 || !strings.Contains(alarms[0].Text, "waits for laptop: no room") || strings.Contains(alarms[0].Text, "cloud3") || !alarms[0].Wait {
		t.Errorf("expected the pinned host's no room as a wait, got %+v", alarms)
	}
}
