package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// mw-gq6.1429: the fstrim drop-ins (mw host trim-setup) and the Windows drive
// report (mw host wsl-flag) landed and were never installed on the desktop,
// and no alarm was heard. A WSL host missing either is faulty, never
// cannot-tell, with the command that installs it in the line, and the Mayor
// hears once for each wait of the check's damper.

// noDropIns are drop-in paths that are not there.
func noDropIns(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	return []string{filepath.Join(dir, "timer.conf"), filepath.Join(dir, "service.conf")}
}

func TestAWSLHostMissingEitherInstallIsAStandingAlarm(t *testing.T) {
	t.Run("no fstrim drop-ins and a timer that never ran", func(t *testing.T) {
		check := trimCheck(t, "wsl", "loaded", "")
		check.DropIns = noDropIns(t)
		verdict, reason := check.Probe(context.Background())
		if verdict != application.DoctorFaulty {
			t.Fatalf("expected faulty, got %v %q", verdict, reason)
		}
		for _, want := range []string{"drop-in", "not installed", doctor.WSLTrimCureCommand} {
			if !strings.Contains(reason, want) {
				t.Errorf("expected the line to hold %q, got %q", want, reason)
			}
		}
	})

	t.Run("a trim that ran lately is no missing install, whatever the drop-in files", func(t *testing.T) {
		check := trimCheck(t, "wsl", "loaded", unixAt(2*24*time.Hour))
		check.DropIns = noDropIns(t)
		if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
			t.Fatalf("expected ok for a timer that ran 2 days ago, got %v %q", verdict, reason)
		}
	})

	t.Run("no windows-drive-free report", func(t *testing.T) {
		verdict, reason := driveFreeCheck(t, "wsl", "", true).Probe(context.Background())
		if verdict != application.DoctorFaulty || !strings.Contains(reason, "mw host wsl-flag --install-line") {
			t.Fatalf("expected faulty naming the install line, got %v %q", verdict, reason)
		}
	})

	t.Run("a windows-drive-free report over a day old: the task is not running", func(t *testing.T) {
		for _, ago := range []time.Duration{24*time.Hour + time.Minute, 3 * 24 * time.Hour} {
			verdict, reason := driveFreeCheck(t, "wsl", report("D:", 100, ago), false).Probe(context.Background())
			if verdict != application.DoctorFaulty {
				t.Fatalf("a report %s old: expected faulty, not cannot-tell, got %v %q", ago, verdict, reason)
			}
			if !strings.Contains(reason, "mw host wsl-flag --install-line") || !strings.Contains(reason, "stopped") {
				t.Errorf("a report %s old: expected the line to say the task stopped and name the install line, got %q", ago, reason)
			}
		}
	})

	t.Run("a report under a day old is current", func(t *testing.T) {
		if verdict, reason := driveFreeCheck(t, "wsl", report("D:", 100, 23*time.Hour), false).Probe(context.Background()); verdict != application.DoctorOK {
			t.Fatalf("expected ok, got %v %q", verdict, reason)
		}
	})

	t.Run("neither is asked of a host that is not WSL", func(t *testing.T) {
		trim := trimCheck(t, "none", "loaded", "")
		trim.DropIns = noDropIns(t)
		if verdict, _ := trim.Probe(context.Background()); verdict != application.DoctorOK {
			t.Errorf("wsl-trim: expected ok off WSL, got %v", verdict)
		}
		if verdict, _ := driveFreeCheck(t, "none", "", true).Probe(context.Background()); verdict != application.DoctorOK {
			t.Errorf("wsl-drive-free: expected ok off WSL, got %v", verdict)
		}
	})
}

// A host missing both installs, doctored every 5 minutes, leaves the Mayor one
// note of each a damper wait (a day for the trim, 6 hours for the drive), and
// every run says faulty meanwhile: the fault is never damped into silence.
func TestAWSLHostMissingBothInstallsWritesTheMayorOneNoteEachDamperWait(t *testing.T) {
	trim := trimCheck(t, "wsl", "loaded", "")
	trim.DropIns = noDropIns(t)
	drive := driveFreeCheck(t, "wsl", "", true)
	notes := map[string]int{}
	trim.Note = func(_ context.Context, text string) error { notes[doctor.WSLTrimName]++; return nil }
	drive.Note = func(_ context.Context, text string) error { notes[doctor.WSLDriveFreeName]++; return nil }

	now := trimNow
	verdicts := apptest.NewFakeTracker()
	runner := application.Doctor{
		Checks: application.DoctorChecks{trim, drive},
		State:  apptest.NewFakeDoctorState(), Log: &apptest.FakeDoctorLog{}, Verdicts: verdicts,
		Host: "desktop", Now: func() time.Time { return now },
	}
	run := func(at time.Duration) {
		t.Helper()
		now = trimNow.Add(at)
		report, _ := runner.Run(context.Background(), "", false)
		for _, result := range report.Results {
			if result.Verdict != "faulty" && result.Verdict != "damped" {
				t.Errorf("at +%s: %s is %q, want faulty (the note) or damped, never cured, ok or cannot-tell", at, result.Check, result.Verdict)
			}
		}
		// What the other hosts read: both installs still missing, on the tick
		// that wrote the note as on every other.
		said, _ := verdicts.Note(context.Background(), application.DoctorVerdictKey("desktop"))
		verdict, ok := application.ParseDoctorVerdict(said)
		if !ok || len(verdict.Faulty) != 2 || verdict.Faulty[0].Check != "wsl-trim" || verdict.Faulty[1].Check != "wsl-drive-free" {
			t.Errorf("at +%s: expected the verdict to hold both installs faulty, got %q", at, said)
		}
	}

	run(0)
	run(5 * time.Minute)
	run(time.Hour)
	if notes["wsl-trim"] != 1 || notes["wsl-drive-free"] != 1 {
		t.Fatalf("expected one note each in the first hour, got %v", notes)
	}
	run(7 * time.Hour)
	if notes["wsl-trim"] != 1 || notes["wsl-drive-free"] != 2 {
		t.Fatalf("expected the drive's second note after its 6 hours and the trim's not yet, got %v", notes)
	}
	run(25 * time.Hour)
	if notes["wsl-trim"] != 2 {
		t.Fatalf("expected the trim's second note after its day, got %v", notes)
	}
}

// The DAMPERS alarm of a stale report names a check by the same name the
// doctor logs it under.
func TestTheInstallChecksAreTheOnesDampersKeepsAlarmingFrom(t *testing.T) {
	want := []string{doctor.WSLTrimName, doctor.WSLDriveFreeName}
	got := application.DoctorInstallChecks
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for _, name := range want {
		found := false
		for _, g := range got {
			found = found || g == name
		}
		if !found {
			t.Errorf("expected %s among the install checks, got %v", name, got)
		}
	}
	_ = os.Getenv
}
