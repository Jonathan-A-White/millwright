package doctor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

var baNow = time.Date(2026, 10, 7, 15, 7, 0, 0, time.UTC)

// baCheck is the check on the laptop, which the home file says is home, with
// the desktop's last sync noted at desktopSync ("" for no note) and the VPS's
// at vpsSync, and a clock fixed at baNow.
func baCheck(t *testing.T, desktopSync, vpsSync string) (*doctor.BoostAsleep, *apptest.FakeDoctorNotes) {
	t.Helper()
	notes := apptest.NewFakeDoctorNotes()
	if desktopSync != "" {
		_ = notes.SetNote(context.Background(), application.LastSyncKey("desktop"), desktopSync)
	}
	if vpsSync != "" {
		_ = notes.SetNote(context.Background(), application.LastSyncKey("vps"), vpsSync)
	}
	check := doctor.NewBoostAsleep(pcThisHost, "laptop", notes)
	check.Limit = application.DefaultHostSilence
	check.Now = func() time.Time { return baNow }
	return check, notes
}

func baStamp(ago time.Duration) string {
	return baNow.Add(-ago).Format(application.LastSyncFormat)
}

func TestTheBoostAsleepProbeIsFaultyWhenTheBoostIsSilentPastTheThreshold(t *testing.T) {
	check, _ := baCheck(t, "2026-10-05T13:40:24Z", "")

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if want := "desktop silent 49h26m (threshold 2h)"; !strings.Contains(reason, want) {
		t.Fatalf("expected the reason to say %q, got %q", want, reason)
	}
}

func TestTheBoostAsleepProbeIsOkWhenTheBoostIsSilentWithinTheThreshold(t *testing.T) {
	check, _ := baCheck(t, baStamp(119*time.Minute), "")

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "desktop") {
		t.Fatalf("expected the reason to name the desktop, got %q", reason)
	}
}

func TestTheBoostAsleepProbeIsFaultyOnceTheThresholdIsPassedByAMinute(t *testing.T) {
	check, _ := baCheck(t, baStamp(121*time.Minute), "")

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
}

func TestTheBoostAsleepProbeUsesTheThresholdItIsGiven(t *testing.T) {
	check, _ := baCheck(t, baStamp(3*time.Hour), "")
	check.Limit = 6 * time.Hour

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok under a 6h threshold, got %s (%s)", verdict, reason)
	}
}

func TestTheBoostAsleepProbeReadsTheDefaultThresholdWhenNoneIsSet(t *testing.T) {
	check, _ := baCheck(t, baStamp(3*time.Hour), "")
	check.Limit = 0

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty under the default 2h threshold, got %s (%s)", verdict, reason)
	}
}

func TestTheBoostAsleepProbeCannotTellWithNoLastSyncNote(t *testing.T) {
	check, _ := baCheck(t, "", "")

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTheBoostAsleepProbeCannotTellWhenTheNoteIsNotATime(t *testing.T) {
	check, _ := baCheck(t, "yesterday-ish", "")

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTheBoostAsleepProbeCannotTellWhenTheNoteCannotBeRead(t *testing.T) {
	check, notes := baCheck(t, baStamp(time.Minute), "")
	notes.Err = errors.New("bd is down")

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell || !strings.Contains(reason, "bd is down") {
		t.Fatalf("expected cannot-tell naming the error, got %s (%s)", verdict, reason)
	}
}

func TestTheBoostAsleepProbeCannotTellWhenTheThresholdCannotBeRead(t *testing.T) {
	check, _ := baCheck(t, baStamp(time.Minute), "")
	check.LimitErr = errors.New("host_silent_hours is nonsense")

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell || !strings.Contains(reason, "nonsense") {
		t.Fatalf("expected cannot-tell naming the error, got %s (%s)", verdict, reason)
	}
}

func TestTheBoostAsleepProbeDoesNotLookAtTheVPS(t *testing.T) {
	check, _ := baCheck(t, baStamp(time.Minute), "2026-10-01T00:00:00Z")

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok with only the VPS silent, got %s (%s)", verdict, reason)
	}
	if strings.Contains(reason, "vps") {
		t.Fatalf("expected the reason not to name the VPS, got %q", reason)
	}
}

func TestTheBoostAsleepProbeDoesNotApplyOnAHostThatIsNotHome(t *testing.T) {
	check, _ := baCheck(t, "2026-10-05T13:40:24Z", "")
	check.Home = pcOtherHost

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK || !strings.Contains(reason, "n/a") {
		t.Fatalf("expected ok, n/a, got %s (%s)", verdict, reason)
	}
}

func TestTheBoostAsleepProbeLooksAtTheLaptopWhenTheDesktopIsHome(t *testing.T) {
	check, notes := baCheck(t, "", "")
	check.Home, check.Host = pcOtherHost, "desktop"
	_ = notes.SetNote(context.Background(), application.LastSyncKey("laptop"), baStamp(5*time.Hour))
	_ = notes.SetNote(context.Background(), application.LastSyncKey("desktop"), baStamp(time.Minute))

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "laptop silent 5h00m") {
		t.Fatalf("expected faulty naming the laptop, got %s (%s)", verdict, reason)
	}
}

func TestTheBoostAsleepProbeCannotTellWhenTheHomeCannotBeTold(t *testing.T) {
	check, _ := baCheck(t, baStamp(5*time.Hour), "")
	check.Home = &apptest.FakeHomeFile{Missing: true}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTheBoostAsleepCureChangesNothing(t *testing.T) {
	check, notes := baCheck(t, "2026-10-05T13:40:24Z", "")
	check.Probe(context.Background())

	if err := check.Cure(context.Background()); err == nil {
		t.Fatalf("expected the cure to say there is none")
	}
	if got, _ := notes.Note(context.Background(), application.LastSyncKey("desktop")); got != "2026-10-05T13:40:24Z" {
		t.Fatalf("the cure changed the desktop's note: %q", got)
	}
}
