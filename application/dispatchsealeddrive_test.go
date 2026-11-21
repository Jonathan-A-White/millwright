package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1527: the desktop's WSL is sealed from Windows and the Windows task
// that reports its D: was never installed there, so its reading had no drive
// (DriveKnown false) and no floor applied: dispatch went on starting Builders
// until D: had 3 MB free and WSL would not start (Wsl/Service/E_UNEXPECTED). A
// host known to be WSL whose Windows drive no current report shows has no room;
// the guard turned off (min_drive_free_gb = 0) and a host not known to be WSL
// are held to nothing, as before.
func TestDispatchStartsNoBuilderOnAWSLHostThatCannotSeeItsWindowsDrive(t *testing.T) {
	blind := application.LoadReading{Load: 1, Cores: 16, MemAvailableMB: 8000, MemKnown: true, WSL: true}
	notWSL := application.LoadReading{Load: 1, Cores: 16, MemAvailableMB: 8000, MemKnown: true}
	seen := aDriveWith(200 * gb)
	seen.WSL = true
	for _, tt := range []struct {
		name    string
		reading application.LoadReading
		limits  application.RoomLimits
		starts  bool
	}{
		{"a WSL host with no current drive report", blind, application.RoomLimits{}, false},
		{"the same with the drive guard off", blind, application.RoomLimits{NoDriveGuard: true}, true},
		{"a host not known to be WSL with no report", notWSL, application.RoomLimits{}, true},
		{"a WSL host whose report shows room", seen, application.RoomLimits{}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report := driveDispatch(t, tt.reading, tt.limits)
			if got := len(startedIDs(report)) > 0; got != tt.starts {
				t.Errorf("started a Builder = %v, want %v (passed %+v)", got, tt.starts, report.Passed)
			}
		})
	}

	why := blind.NoRoom(application.RoomLimits{})
	for _, want := range []string{"cannot see its Windows drive", "mw host wsl-flag --install-line"} {
		if !strings.Contains(why, want) {
			t.Errorf("NoRoom lacks %q: %q", want, why)
		}
	}
	alarm, found := blind.DriveFloorAlarm("desktop", application.RoomLimits{})
	if !found || !strings.Contains(alarm.Text, "desktop") || !strings.Contains(alarm.Text, "mw host wsl-flag --install-line") {
		t.Errorf("DAMPERS line for a blind WSL host = %+v, %v; want one naming the host and the install line", alarm, found)
	}
	if _, found := blind.DriveFloorAlarm("desktop", application.RoomLimits{NoDriveGuard: true}); found {
		t.Errorf("DAMPERS line shown with the drive guard off")
	}
	if _, found := notWSL.DriveFloorAlarm("laptop", application.RoomLimits{}); found {
		t.Errorf("DAMPERS line shown for a host not known to be WSL")
	}
}
