package beads_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// mw-gq6.612: after mw-gq6.608 the desktop's mw doctor still started about 4 bd
// a minute with nothing wrong: the work in hand (three listings) and every bead
// (one more) for the DAMPERS line and the starved-rig check, on every run. With
// the connection kept open the doctor reads them over it, and the one bd left
// on a run is beads-batch's `bd config get dolt.auto-commit`, which asks bd
// itself how it commits and is no row of the server's.
func TestADoctorRunWithNothingWrongStartsAtMostOneBd(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".beads", "config.yaml"), []byte("dolt:\n  auto-commit: batch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := beads.New(vault, beads.WithProgram(program), beads.WithHeadDB(server.open(t)),
		beads.WithActor("mw@desktop"), beads.WithKeptSaid(io.Discard))

	now := time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC)
	survey := application.DamperSurvey{Tracker: g, Notes: g, Graph: g}
	store := doctor.New(t.TempDir())
	doc := application.Doctor{
		Checks: application.DoctorChecks{
			doctor.NewBeadsBatch(vault, g.AutoCommit),
			doctor.NewBeadsChurn(g.ServerConnections, store),
			&application.StarvedRigCheck{
				Host:   "desktop",
				Alarms: func(ctx context.Context) ([]application.DamperAlarm, error) { return survey.Starved(ctx, now) },
				Mail:   func(context.Context, application.NewMessage) error { return nil },
			},
			&apptest.FakeDoctorCheck{CheckName: "quiet", Verdict: application.DoctorOK},
		},
		State:   store,
		Log:     store,
		Notes:   g,
		Dampers: survey,
		Host:    "desktop",
		Now:     func() time.Time { return now },
		Out:     io.Discard,
	}

	if _, err := doc.Run(ctx, "", false); err != nil {
		t.Fatalf("the first run: %v", err)
	}
	from := len(bdCallsSince(t, asked, 0))
	now = now.Add(5 * time.Minute)
	report, err := doc.Run(ctx, "", false)
	if err != nil {
		t.Fatalf("the second run: %v", err)
	}
	for _, result := range report.Results {
		if result.Verdict != "ok" {
			t.Errorf("expected every check ok, got %s %s: %s", result.Check, result.Verdict, result.Reason)
		}
	}
	calls := bdCallsSince(t, asked, from)
	if len(calls) > 1 {
		t.Errorf("expected at most one bd started by a doctor run with nothing wrong, got %d:\n%s", len(calls), strings.Join(calls, "\n"))
	}
	if len(calls) == 1 && !strings.Contains(calls[0], "config get dolt.auto-commit") {
		t.Errorf("expected the one bd to be beads-batch's, got %s", calls[0])
	}
	if len(server.unknown) > 0 {
		t.Errorf("the stand-in server was asked what it does not know:\n%s", strings.Join(server.unknown, "\n"))
	}
}
