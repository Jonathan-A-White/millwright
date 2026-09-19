package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// fakeSeatFiles is a seat's files held in memory.
type fakeSeatFiles struct {
	start application.SeatStart
	// asked records the seat and host it was asked about.
	asked [2]string
}

func (f *fakeSeatFiles) SeatStart(_ context.Context, seat, host string) (application.SeatStart, error) {
	f.asked = [2]string{seat, host}
	start := f.start
	start.Seat = seat
	return start, nil
}

func (f *fakeSeatFiles) HostFile(context.Context, string, string, string) (string, error) {
	return "", nil
}

// aSeatStart is a seat with a charter and two handoffs, the newest written an
// hour before noon, with whatever a test changes applied.
func aSeatStart(change func(*application.SeatStart)) application.SeatStart {
	noon := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	start := application.SeatStart{
		Dir:        "/vault",
		Charter:    "/vault/seats/mayor/charter.md",
		HandoffDir: "seats/mayor/handoffs",
		Handoffs: []application.Handoff{
			{Name: "2026-09-19-11", Path: "seats/mayor/handoffs/2026-09-19-11.md", Number: 11, Written: noon.Add(-2 * time.Hour)},
			{Name: "2026-09-19-12", Path: "seats/mayor/handoffs/2026-09-19-12.md", Number: 12, Written: noon.Add(-time.Hour)},
		},
	}
	if change != nil {
		change(&start)
	}
	return start
}

// aSeatUp starts the mayor at noon on the files and terminal given.
func aSeatUp(files application.SeatFiles, windows application.Windows) application.SeatUp {
	return application.SeatUp{
		Seats:   files,
		Windows: windows,
		Harness: &fakeSeatHarness{},
		Seat:    "mayor",
		Host:    "laptop",
		Now:     func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) },
	}
}

// fakeSeatHarness turns a launch into a window without a harness anywhere.
type fakeSeatHarness struct{}

func (h *fakeSeatHarness) SeatSession(l application.SeatLaunch) (application.WindowSpec, error) {
	return application.WindowSpec{Name: l.Name, Dir: l.Dir, Command: []string{"claude", l.Kickoff}}, nil
}

// Window names end in a number, so one seat's names can be prefixes of each
// other. The window that holds the seat is the whole name the acting file
// names, never a shorter one that happens to be inside it.
func TestTheActingWindowIsTheLongestNameInTheActingFile(t *testing.T) {
	files := &fakeSeatFiles{start: aSeatStart(func(s *application.SeatStart) {
		s.Acting = "Mayor after handoff 12 (tmux window 4 'mayor-2026-09-19-13'), since 2026-09-19T11:30:00Z"
	})}
	windows := apptest.NewFakeWindows()
	// The old window, whose name is inside the new one's, was opened before the
	// newest handoff; the window the acting file actually names was opened
	// after it.
	windows.Holds("mayor-2026-09-19-1", time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC))
	windows.Holds("mayor-2026-09-19-13", time.Date(2026, 9, 19, 11, 30, 0, 0, time.UTC))

	_, err := aSeatUp(files, windows).Run(context.Background())
	if err == nil {
		t.Fatalf("expected the seat to be refused as already acting, %d windows were opened", len(windows.Opened))
	}
	if !strings.Contains(err.Error(), "mayor-2026-09-19-13") {
		t.Errorf("expected the refusal to name the window the acting file names, got: %v", err)
	}
	if len(windows.Opened) != 0 {
		t.Errorf("expected a refused seat up to have opened nothing, it opened %+v", windows.Opened)
	}
}

// A window nothing can date is not evidence that the seat is held: the refusal
// takes a fact, and an undated window leaves the seat startable.
func TestASeatHeldByAWindowNothingCanDateIsStarted(t *testing.T) {
	files := &fakeSeatFiles{start: aSeatStart(func(s *application.SeatStart) {
		s.Acting = "Mayor in window 'mayor-2026-09-19-12'"
	})}
	windows := apptest.NewFakeWindows()
	windows.Holds("mayor-2026-09-19-12", time.Time{})

	report, err := aSeatUp(files, windows).Run(context.Background())
	if err != nil {
		t.Fatalf("starting the seat: %v", err)
	}
	if report.Window != "mayor-2026-09-19-13" {
		t.Errorf("expected the window to be numbered past the handoffs and the windows, got %q", report.Window)
	}
}

// The seat is asked for on the host it is started on: a seat that keeps its
// handoffs per host must be read for this one.
func TestTheSeatIsReadForTheHostItIsStartedOn(t *testing.T) {
	files := &fakeSeatFiles{start: aSeatStart(nil)}
	if _, err := aSeatUp(files, apptest.NewFakeWindows()).Run(context.Background()); err != nil {
		t.Fatalf("starting the seat: %v", err)
	}
	if files.asked != [2]string{"mayor", "laptop"} {
		t.Errorf("expected the mayor's files to be read for laptop, got %v", files.asked)
	}
}
