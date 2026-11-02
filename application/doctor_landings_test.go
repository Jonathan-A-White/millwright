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

// fakeLandingTrees answers for each directory from a table: the directories in
// spare are ones nothing is lost by taking away, every other is refused with
// the reason in why. Remove deletes the directory, as the real adapter does.
type fakeLandingTrees struct {
	spare   map[string]bool
	why     string
	removed []string
}

func (f *fakeLandingTrees) Spare(_ context.Context, dir string) (bool, string) {
	if f.spare[dir] {
		return true, ""
	}
	return false, f.why
}

func (f *fakeLandingTrees) Remove(_ context.Context, dir string) error {
	f.removed = append(f.removed, dir)
	return os.RemoveAll(dir)
}

func TestMwDoctorRemovesAnOldLandingDirectoryThatLosesNothingAndThenSaysOk(t *testing.T) {
	home := t.TempDir()
	now := doctorNow
	spare := aLandingDir(t, home, "2950915166", now, 25*time.Hour)
	trees := &fakeLandingTrees{spare: map[string]bool{spare: true}}
	check := &application.StaleLandingsCheck{Dirs: []string{home}, Trees: trees, Now: func() time.Time { return now }}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, which the cure puts right, got %v: %s", verdict, reason)
	}
	if !strings.Contains(reason, spare) {
		t.Errorf("expected the finding to name %s, got %q", spare, reason)
	}
	if _, err := os.Stat(spare); err != nil {
		t.Fatalf("a probe changes nothing, but %s: %v", spare, err)
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("cure: %v", err)
	}
	if _, err := os.Stat(spare); !os.IsNotExist(err) {
		t.Errorf("expected the cure to remove %s, got %v", spare, err)
	}
	if !strings.Contains(check.WayBack(), spare) {
		t.Errorf("expected the way back to name what was removed (%s), got %q", spare, check.WayBack())
	}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || reason != "" {
		t.Errorf("expected ok after the cure, got %v %q", verdict, reason)
	}
}

func TestMwDoctorLeavesAnOldLandingDirectoryThatMayHoldSomeonesWorkFlagged(t *testing.T) {
	for name, tc := range map[string]struct {
		age time.Duration
		why string
	}{
		"younger than a day":                   {age: 23 * time.Hour},
		"with uncommitted changes":             {age: 30 * time.Hour, why: "it has uncommitted changes"},
		"with a commit not on main":            {age: 30 * time.Hour, why: "its HEAD is not on main"},
		"with a landing process running in it": {age: 30 * time.Hour, why: "pid 4242 is running in it"},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			now := doctorNow
			dir := aLandingDir(t, home, "1", now, tc.age)
			trees := &fakeLandingTrees{spare: map[string]bool{}, why: tc.why}
			check := &application.StaleLandingsCheck{Dirs: []string{home}, Trees: trees, Now: func() time.Time { return now }}

			verdict, _ := check.Probe(context.Background())
			if tc.age > application.StaleLandingsAge && verdict != application.DoctorCannotTell {
				t.Errorf("expected cannot-tell, left for a person, got %v", verdict)
			}
			if tc.age <= application.StaleLandingsAge && verdict != application.DoctorOK {
				t.Errorf("expected ok for a landing still being made, got %v", verdict)
			}
			_ = check.Cure(context.Background())
			if _, err := os.Stat(dir); err != nil {
				t.Errorf("expected %s to be left in place, got %v", dir, err)
			}
			if len(trees.removed) != 0 {
				t.Errorf("expected nothing removed, got %v", trees.removed)
			}
		})
	}
}

func TestMwDoctorNamesWhyAnOldLandingDirectoryWasLeftInPlace(t *testing.T) {
	home := t.TempDir()
	now := doctorNow
	dir := aLandingDir(t, home, "7", now, 30*time.Hour)
	trees := &fakeLandingTrees{spare: map[string]bool{}, why: "it has uncommitted changes"}
	check := &application.StaleLandingsCheck{Dirs: []string{home}, Trees: trees, Now: func() time.Time { return now }}

	_, reason := check.Probe(context.Background())
	for _, want := range []string{dir, "it has uncommitted changes"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to say %q, got %q", want, reason)
		}
	}
}
