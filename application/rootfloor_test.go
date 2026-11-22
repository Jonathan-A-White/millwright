package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// aRootWith is a reading of a host with room in everything but, perhaps, its root
// filesystem, whose free space is the argument.
func aRootWith(free int64) application.LoadReading {
	return application.LoadReading{Load: 1, Cores: 8, MemAvailableMB: 8000, MemKnown: true,
		RootFreeBytes: free, RootKnown: true}
}

// mw-6ww.185.6: a host whose own root filesystem has under min_root_free_gb free
// has no room, on any host, WSL or not.
func TestNoRoomSaysWhyAHostWhoseRootFilesystemIsNearlyFull(t *testing.T) {
	tests := []struct {
		name    string
		reading application.LoadReading
		limits  application.RoomLimits
		want    string
	}{
		{"root under the default 15 GB", aRootWith(gb / 2), application.RoomLimits{}, "root has 0.5 GB free, under 15 GB"},
		{"root just under the default 15 GB", aRootWith(14 * gb), application.RoomLimits{}, "root has 14.0 GB free, under 15 GB"},
		{"root at the default 15 GB", aRootWith(15 * gb), application.RoomLimits{}, ""},
		{"root well over", aRootWith(900 * gb), application.RoomLimits{}, ""},
		{"a higher minimum", aRootWith(16 * gb), application.RoomLimits{MinRootFreeGB: 20}, "root has 16.0 GB free, under 20 GB"},
		{"the guard off", aRootWith(0), application.RoomLimits{NoRootGuard: true}, ""},
		{"root not read", application.LoadReading{Load: 1, Cores: 8, MemAvailableMB: 8000, MemKnown: true}, application.RoomLimits{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.reading.NoRoom(tt.limits); got != tt.want {
				t.Errorf("NoRoom = %q, want %q", got, tt.want)
			}
		})
	}
}

// The root floor holds back dispatch on a host that is not WSL as on one that is.
func TestNoRoomForTheRootFilesystemStartsNoBuilder(t *testing.T) {
	for _, tt := range []struct {
		name    string
		reading application.LoadReading
		limits  application.RoomLimits
		starts  bool
	}{
		{"root under the floor", aRootWith(10 * gb), application.RoomLimits{}, false},
		{"root over the floor", aRootWith(20 * gb), application.RoomLimits{}, true},
		{"root unread", application.LoadReading{Load: 1, Cores: 8, MemAvailableMB: 8000, MemKnown: true}, application.RoomLimits{}, true},
		{"root under the floor, the guard off", aRootWith(10 * gb), application.RoomLimits{NoRootGuard: true}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report := driveDispatch(t, tt.reading, tt.limits)
			if got := len(startedIDs(report)) > 0; got != tt.starts {
				t.Errorf("started a Builder = %v, want %v (passed %+v)", got, tt.starts, report.Passed)
			}
		})
	}
}

// mw-6ww.185.6: mw status DAMPERS names the refusal while dispatch refuses for
// it, with the free space and the floor, as it names the Windows drive's.
func TestRootFloorAlarmNamesTheFreeSpaceAndTheFloor(t *testing.T) {
	alarm, found := aRootWith(10*gb).RootFloorAlarm("laptop", application.RoomLimits{})
	if !found {
		t.Fatal("no DAMPERS line for a root filesystem under the floor")
	}
	for _, want := range []string{"laptop", "root has 10.0 GB free", "under the 15 GB floor", "dispatch starts no new Builder on laptop"} {
		if !strings.Contains(alarm.Text, want) {
			t.Errorf("alarm lacks %q: %q", want, alarm.Text)
		}
	}
	if alarm.Principle != application.DamperRootFloor || alarm.Identity != "root-floor:laptop" {
		t.Errorf("alarm = %+v, want principle %q and identity root-floor:laptop", alarm, application.DamperRootFloor)
	}
	for name, tt := range map[string]struct {
		reading application.LoadReading
		limits  application.RoomLimits
	}{
		"at the floor":  {aRootWith(15 * gb), application.RoomLimits{}},
		"over":          {aRootWith(100 * gb), application.RoomLimits{}},
		"not read":      {application.LoadReading{Load: 1, Cores: 8}, application.RoomLimits{}},
		"the guard off": {aRootWith(gb), application.RoomLimits{NoRootGuard: true}},
	} {
		if _, found := tt.reading.RootFloorAlarm("laptop", tt.limits); found {
			t.Errorf("%s: DAMPERS line shown, dispatch is not refusing", name)
		}
	}
	// The alarm is there exactly while NoRoom refuses for the root.
	for _, free := range []int64{gb, 14 * gb, 15 * gb, 16 * gb} {
		reading := aRootWith(free)
		_, found := reading.RootFloorAlarm("laptop", application.RoomLimits{})
		if refuses := strings.Contains(reading.NoRoom(application.RoomLimits{}), "root has"); refuses != found {
			t.Errorf("%d GB: NoRoom refuses for the root = %v, alarm shown = %v", free/gb, refuses, found)
		}
	}
}

// mw status prints the refusal under DAMPERS while dispatch refuses for the root.
func TestStatusDampersShowsTheRootFloorRefusal(t *testing.T) {
	statusOf := func(reading application.LoadReading) string {
		tracker := apptest.NewFakeTracker()
		report, err := application.Status{
			Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder",
			Load: &apptest.FakeHostLoad{Reading: reading}, Room: application.RoomLimits{},
			Cap: 2,
		}.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return report.String()
	}
	under := statusOf(aRootWith(10 * gb))
	if !strings.Contains(under, "DAMPERS") {
		t.Fatalf("no DAMPERS with the root at 10 GB: %s", under)
	}
	dampers := under[strings.Index(under, "DAMPERS"):]
	for _, want := range []string{"laptop", "root has 10.0 GB free", "15 GB"} {
		if !strings.Contains(dampers, want) {
			t.Errorf("DAMPERS lacks %q: %q", want, dampers)
		}
	}
	if over := statusOf(aRootWith(20 * gb)); strings.Contains(over, "DAMPERS") {
		t.Errorf("DAMPERS is shown with 20 GB free: %s", over)
	}
}
