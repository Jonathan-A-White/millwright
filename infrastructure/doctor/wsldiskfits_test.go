package doctor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

const (
	fitGiB = int64(1) << 30
	// the desktop's numbers: a 286 GB drive D: and a Linux disk that sees 1,007 GB
	fitDriveBytes = 286 * fitGiB
)

// wslDiskFitsCheck is the check over a windows-state file whose vhdx_drive line
// says the drive is 286 GB, and a root filesystem of rootGB; state "" writes
// no file at all.
func wslDiskFitsCheck(t *testing.T, virt string, state string, rootGB int64, home bool) *doctor.WSLDiskFits {
	t.Helper()
	path := filepath.Join(t.TempDir(), "windows-state")
	if state != "" {
		if err := os.WriteFile(path, []byte(state), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &doctor.WSLDiskFits{
		Path:       path,
		DetectVirt: detectVirt(virt),
		Home:       func(context.Context) (bool, error) { return home, nil },
		RootSize:   func() (int64, error) { return rootGB * fitGiB, nil },
	}
}

const desktopState = "time 2026-11-21T12:00:00Z\nvhdx_drive D 307090161664 10737418240\nc_free 1\n"

func TestWSLDiskFitsIsFaultyWhenTheLinuxDiskIsBiggerThanItsDrive(t *testing.T) {
	// the desktop: 1,007 GB of Linux disk in a file on a 286 GB drive
	c := wslDiskFitsCheck(t, "wsl", desktopState, 1007, false)
	verdict, reason := c.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %v %q", verdict, reason)
	}
	for _, want := range []string{"1007", "286", "mw host wsl-flag shrink"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected %q in the reason, got %q", want, reason)
		}
	}
}

func TestWSLDiskFitsIsOkWhenTheLinuxDiskFitsItsDrive(t *testing.T) {
	c := wslDiskFitsCheck(t, "wsl", desktopState, 220, false)
	verdict, reason := c.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %v %q", verdict, reason)
	}
	for _, want := range []string{"220", "286"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected %q in the ok line, got %q", want, reason)
		}
	}
}

func TestWSLDiskFitsLeavesTwentyGBBetweenTheDiskAndItsDrive(t *testing.T) {
	if v, r := wslDiskFitsCheck(t, "wsl", desktopState, 266, false).Probe(context.Background()); v != application.DoctorOK {
		t.Errorf("266 GB on 286 GB leaves 20 GB and is ok, got %v %q", v, r)
	}
	if v, r := wslDiskFitsCheck(t, "wsl", desktopState, 267, false).Probe(context.Background()); v != application.DoctorFaulty {
		t.Errorf("267 GB on 286 GB leaves under 20 GB and is faulty, got %v %q", v, r)
	}
}

func TestWSLDiskFitsCannotTellWithNoWindowsState(t *testing.T) {
	verdict, reason := wslDiskFitsCheck(t, "wsl", "", 1007, false).Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %v %q", verdict, reason)
	}
	if !strings.Contains(reason, "windows-state") {
		t.Errorf("expected the missing windows-state named, got %q", reason)
	}
}

func TestWSLDiskFitsCannotTellWhenTheStateHoldsNoDrive(t *testing.T) {
	verdict, reason := wslDiskFitsCheck(t, "wsl", "time 2026-11-21T12:00:00Z\nvhdx_drive unknown\n", 1007, false).Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %v %q", verdict, reason)
	}
}

func TestWSLDiskFitsIsOkOnAHostThatIsNotWSL(t *testing.T) {
	verdict, reason := wslDiskFitsCheck(t, "none", "", 1007, false).Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %v %q", verdict, reason)
	}
	if !strings.Contains(reason, "not WSL") {
		t.Errorf("expected 'not WSL', got %q", reason)
	}
}

// The home's disk is not shrunk without the Governor's word: it is never
// faulty, and its ok line says the two numbers and who watches the drive.
func TestWSLDiskFitsIsNeverFaultyOnTheHome(t *testing.T) {
	c := wslDiskFitsCheck(t, "wsl", desktopState, 1007, true)
	verdict, reason := c.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok on the home, got %v %q", verdict, reason)
	}
	for _, want := range []string{"1007", "286", "watched by wsl-drive-free"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected %q in the ok line, got %q", want, reason)
		}
	}
	// and with no windows-state it is still ok, not cannot-tell
	if v, r := wslDiskFitsCheck(t, "wsl", "", 1007, true).Probe(context.Background()); v != application.DoctorOK || !strings.Contains(r, "watched by wsl-drive-free") {
		t.Errorf("expected ok naming wsl-drive-free with no state on the home, got %v %q", v, r)
	}
}

func TestWSLDiskFitsCannotTellWhenTheHomeCannotBeTold(t *testing.T) {
	c := wslDiskFitsCheck(t, "wsl", desktopState, 1007, false)
	c.Home = func(context.Context) (bool, error) { return false, errors.New("no vault") }
	if v, r := c.Probe(context.Background()); v != application.DoctorCannotTell {
		t.Errorf("expected cannot-tell, got %v %q", v, r)
	}
}

func TestWSLDiskFitsCureIsOneNoteNamingTheShrink(t *testing.T) {
	c := wslDiskFitsCheck(t, "wsl", desktopState, 1007, false)
	var notes []string
	c.Note = func(_ context.Context, text string) error { notes = append(notes, text); return nil }
	c.Probe(context.Background())
	if err := c.Cure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "mw host wsl-flag shrink") {
		t.Errorf("expected one note naming the shrink, got %q", notes)
	}
	if !c.CureLeavesFault() {
		t.Errorf("the cure is a note: the fault stands after it")
	}
	if _, cap := c.Damper(); cap != 0 {
		t.Errorf("a note has no cure to cap, got %d", cap)
	}
}
