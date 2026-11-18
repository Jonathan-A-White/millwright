package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

const gb = int64(1) << 30

func aDriveWith(free int64) application.LoadReading {
	return application.LoadReading{Load: 1, Cores: 8, MemAvailableMB: 8000, MemKnown: true,
		DriveName: "D:", DriveFreeBytes: free, DriveKnown: true}
}

// mw-gq6.1381: a WSL host whose Windows drive is nearly full has no room: the
// desktop's D: reached 0.01 GB free and WSL went read-only.
func TestNoRoomSaysWhyAWSLHostWhoseWindowsDriveIsNearlyFull(t *testing.T) {
	tests := []struct {
		name    string
		reading application.LoadReading
		limits  application.RoomLimits
		want    string
	}{
		{"drive under the default 15 GB", aDriveWith(gb / 2), application.RoomLimits{}, "D: has 0.5 GB free, under 15 GB: the WSL disk cannot grow"},
		{"drive just under the default 15 GB", aDriveWith(14 * gb), application.RoomLimits{}, "D: has 14.0 GB free, under 15 GB: the WSL disk cannot grow"},
		{"drive at the default 15 GB", aDriveWith(15 * gb), application.RoomLimits{}, ""},
		{"drive well over", aDriveWith(200 * gb), application.RoomLimits{}, ""},
		{"a higher minimum", aDriveWith(16 * gb), application.RoomLimits{MinDriveFreeGB: 20}, "D: has 16.0 GB free, under 20 GB: the WSL disk cannot grow"},
		{"the guard off", aDriveWith(0), application.RoomLimits{NoDriveGuard: true}, ""},
		{"drive not read", application.LoadReading{Load: 1, Cores: 8}, application.RoomLimits{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.reading.NoRoom(tt.limits); got != tt.want {
				t.Errorf("NoRoom = %q, want %q", got, tt.want)
			}
		})
	}
}

func driveDispatch(t *testing.T, reading application.LoadReading, limits application.RoomLimits) application.DispatchReport {
	t.Helper()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	dispatch.Load = &apptest.FakeHostLoad{Reading: reading}
	dispatch.Room = limits
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	return report
}

// AC 1: under the minimum dispatch claims nothing and says why in one line; at
// or over it, it goes on; with the guard off it goes on whatever the drive.
func TestDispatchClaimsNothingOnAWSLHostWhoseWindowsDriveIsUnderTheMinimum(t *testing.T) {
	report := driveDispatch(t, aDriveWith(gb/100), application.RoomLimits{})
	if ids := startedIDs(report); len(ids) != 0 {
		t.Fatalf("started %v on a host whose drive has 0.01 GB free", ids)
	}
	var why string
	for _, p := range report.Passed {
		if p.Gate == "no-room" {
			why = p.Why
		}
	}
	if !strings.Contains(why, "D: has 0.0 GB free, under 15 GB") || strings.Contains(why, "\n") {
		t.Errorf("want one no-room line naming the drive, got %q (passed %+v)", why, report.Passed)
	}
}

func TestDispatchGoesOnWhereTheWindowsDriveHasTheMinimumOrTheGuardIsOff(t *testing.T) {
	for name, arrange := range map[string]struct {
		reading application.LoadReading
		limits  application.RoomLimits
	}{
		"at the minimum": {aDriveWith(15 * gb), application.RoomLimits{}},
		"guard off":      {aDriveWith(gb / 100), application.RoomLimits{NoDriveGuard: true}},
		"not a WSL host": {application.LoadReading{Load: 1, Cores: 8}, application.RoomLimits{}},
	} {
		t.Run(name, func(t *testing.T) {
			report := driveDispatch(t, arrange.reading, arrange.limits)
			if ids := startedIDs(report); len(ids) == 0 {
				t.Errorf("nothing started, passed %+v", report.Passed)
			}
		})
	}
}
