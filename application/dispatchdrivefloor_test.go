package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1428 AC 1: a dispatch pass on a WSL host starts no Builder while the
// Windows drive's free space is under the floor (15 GB unless the config says
// another), and starts one at or over it; a report that is not current or not
// there (DriveKnown false, as hostload leaves it) does not block.
func TestDispatchStartsNoBuilderOnAWSLHostWhoseWindowsDriveIsUnderTheFloor(t *testing.T) {
	notRead := application.LoadReading{Load: 1, Cores: 8, MemAvailableMB: 8000, MemKnown: true}
	for _, tt := range []struct {
		name    string
		reading application.LoadReading
		limits  application.RoomLimits
		starts  bool
	}{
		{"10 GB free under the default floor", aDriveWith(10 * gb), application.RoomLimits{}, false},
		{"10 GB free under a floor of 15", aDriveWith(10 * gb), application.RoomLimits{MinDriveFreeGB: 15}, false},
		{"20 GB free over a floor of 15", aDriveWith(20 * gb), application.RoomLimits{MinDriveFreeGB: 15}, true},
		{"20 GB free over the default floor", aDriveWith(20 * gb), application.RoomLimits{}, true},
		{"a report older than a day or none", notRead, application.RoomLimits{MinDriveFreeGB: 15}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report := driveDispatch(t, tt.reading, tt.limits)
			if got := len(startedIDs(report)) > 0; got != tt.starts {
				t.Errorf("started a Builder = %v, want %v (passed %+v)", got, tt.starts, report.Passed)
			}
		})
	}
}

// mw-gq6.1428 AC 2: mw status's DAMPERS names the host, the free space and the
// floor while dispatch is refusing for the drive, and nothing when it is not.
func TestStatusDampersShowsTheDriveFloorRefusal(t *testing.T) {
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

	under := statusOf(aDriveWith(10 * gb))
	dampers := under[strings.Index(under, "DAMPERS"):]
	if end := strings.Index(dampers, "\n\n"); end >= 0 {
		dampers = dampers[:end]
	}
	for _, want := range []string{"laptop", "D:", "10.0 GB", "15 GB"} {
		if !strings.Contains(dampers, want) {
			t.Errorf("DAMPERS lacks %q: %q", want, dampers)
		}
	}

	if over := statusOf(aDriveWith(20 * gb)); strings.Contains(over, "DAMPERS") {
		t.Errorf("DAMPERS is shown with 20 GB free: %s", over)
	}
	if none := statusOf(application.LoadReading{Load: 1, Cores: 8}); strings.Contains(none, "DAMPERS") {
		t.Errorf("DAMPERS is shown with no drive report: %s", none)
	}
}
