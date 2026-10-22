package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// aLandingDir makes a landing directory under parent whose modification time
// is age before now.
func aLandingDir(t *testing.T, parent, id string, now time.Time, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(parent, application.LandingPrefix+id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making %s: %v", dir, err)
	}
	then := now.Add(-age)
	if err := os.Chtimes(dir, then, then); err != nil {
		t.Fatalf("ageing %s: %v", dir, err)
	}
	return dir
}

func TestMwDoctorNamesEveryLandingDirectoryOlderThanADayWithItsID(t *testing.T) {
	home, rigs := t.TempDir(), t.TempDir()
	now := doctorNow
	old := aLandingDir(t, home, "1040644850", now, 6*24*time.Hour)
	oldBeside := aLandingDir(t, rigs, "515436355", now, 25*time.Hour)
	aLandingDir(t, home, "803035949", now, 23*time.Hour)
	if err := os.MkdirAll(filepath.Join(home, "mw-landing-notadot"), 0o755); err != nil {
		t.Fatal(err)
	}
	check := &application.StaleLandingsCheck{Dirs: []string{home, rigs, home, filepath.Join(home, "missing")}, Now: func() time.Time { return now }}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected the verdict to leave it to a person (cannot-tell), got %v: %s", verdict, reason)
	}
	for _, want := range []string{"1040644850", old, "515436355", oldBeside} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to name %q, got %q", want, reason)
		}
	}
	if strings.Contains(reason, "803035949") {
		t.Errorf("a landing of 23 hours is still being made, not stale: %q", reason)
	}
	if strings.Count(reason, "1040644850") != 2 { // the id, and the path it ends in
		t.Errorf("expected a directory listed once even when its parent is given twice, got %q", reason)
	}
	for _, path := range []string{old, oldBeside} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("a probe changes nothing, but %s: %v", path, err)
		}
	}
}

func TestMwDoctorIsQuietWhenNoLandingDirectoryIsOlderThanADay(t *testing.T) {
	home := t.TempDir()
	now := doctorNow
	aLandingDir(t, home, "803035949", now, time.Hour)
	check := &application.StaleLandingsCheck{Dirs: []string{home}, Now: func() time.Time { return now }}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || reason != "" {
		t.Errorf("expected a plain ok, got %v %q", verdict, reason)
	}
}
