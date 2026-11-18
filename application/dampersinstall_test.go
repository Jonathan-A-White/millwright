package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// installAlarmsOf is the DAMPERS alarms an mw status of the VPS reads about
// host, whose doctor left verdict ago ago (mw-gq6.1429).
func installAlarmsOf(t *testing.T, host string, verdict application.DoctorVerdict) []application.DamperAlarm {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.9", "A story", host)
	syncedAt(t, tracker, host, time.Minute)
	if err := tracker.SetNote(context.Background(), application.DoctorVerdictKey(host), verdict.Note()); err != nil {
		t.Fatalf("leaving the verdict of %s: %v", host, err)
	}
	var found []application.DamperAlarm
	for _, alarm := range otherHostStatus(t, tracker).Dampers.Alarms {
		if strings.Contains(alarm.Text, host+":") {
			found = append(found, alarm)
		}
	}
	return found
}

// mw-gq6.1429: the desktop's doctor report was a week old when its fstrim
// drop-ins and Windows drive task were still not installed, and a stale report
// is no DAMPERS alarm, so nobody heard. A WSL host whose last report said an
// install was missing keeps that alarm, by host and by install, until a report
// says it is installed.
func TestDampersNamesAWSLHostMissingAnInstall(t *testing.T) {
	trim := application.DoctorFinding{Check: "wsl-trim", Reason: "fstrim.timer has never run on this WSL host; cure: mw host trim-setup"}
	drive := application.DoctorFinding{Check: "wsl-drive-free", Reason: "no report of the Windows drive's free space; `mw host wsl-flag --install-line` prints the line"}

	t.Run("a fresh report names the host and the missing install", func(t *testing.T) {
		alarms := installAlarmsOf(t, "desktop", application.DoctorVerdict{At: statusNow.Add(-time.Hour), Faulty: []application.DoctorFinding{trim, drive}})
		if len(alarms) != 2 {
			t.Fatalf("expected one alarm for each missing install, got %+v", alarms)
		}
		for i, want := range []string{"wsl-trim", "wsl-drive-free"} {
			if !strings.Contains(alarms[i].Text, want) || !strings.Contains(alarms[i].Line(), "desktop") {
				t.Errorf("expected alarm %d to name desktop and %s, got %q", i, want, alarms[i].Line())
			}
		}
	})

	t.Run("a report a week old still names them, and says how old it is", func(t *testing.T) {
		alarms := installAlarmsOf(t, "desktop", application.DoctorVerdict{At: statusNow.Add(-7 * 24 * time.Hour), Faulty: []application.DoctorFinding{trim, drive}})
		if len(alarms) != 2 {
			t.Fatalf("expected both installs still named from a stale report, got %+v", alarms)
		}
		for _, alarm := range alarms {
			if !strings.Contains(alarm.Text, "7d") || !strings.Contains(alarm.Text, "stale") {
				t.Errorf("expected the alarm to say the report is 7d old and stale, got %q", alarm.Text)
			}
		}
	})

	t.Run("a stale report of some other fault is still no alarm", func(t *testing.T) {
		other := application.DoctorFinding{Check: "beads-size", Reason: "too big"}
		if alarms := installAlarmsOf(t, "desktop", application.DoctorVerdict{At: statusNow.Add(-7 * 24 * time.Hour), Faulty: []application.DoctorFinding{other}}); len(alarms) != 0 {
			t.Fatalf("expected no alarm from a stale report of another check, got %+v", alarms)
		}
	})

	t.Run("a report that says nothing is faulty names nothing", func(t *testing.T) {
		for _, ago := range []time.Duration{time.Hour, 7 * 24 * time.Hour} {
			if alarms := installAlarmsOf(t, "desktop", application.DoctorVerdict{At: statusNow.Add(-ago)}); len(alarms) != 0 {
				t.Fatalf("expected nothing once installed (report %s old), got %+v", ago, alarms)
			}
		}
	})
}
