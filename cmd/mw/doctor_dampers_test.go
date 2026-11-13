package main

import (
	"context"
	"testing"
	"time"
)

// mw-6ww.175.41: mw doctor built its damper survey on an uncached gateway and
// asked it for the counts, the starved rigs and the refused stories, each of
// which reads the list of every bead (120 MB on the live vault), so one run
// ran bd list --all three times. The survey is read from a tracker that keeps
// the list for the run, so it is read once.
func TestDoctorDampersTrackerReadsEveryBeadOnce(t *testing.T) {
	asked := aBdThatRecords(t)
	dir := t.TempDir()
	t.Setenv("MW_VAULT", dir)
	t.Setenv("MW_HOST", "laptop")

	survey, err := doctorDamperSurvey(dir, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := survey.Starved(ctx, time.Now()); err != nil {
		t.Fatalf("Starved: %v", err)
	}
	if _, err := survey.Refused(ctx, time.Now()); err != nil {
		t.Fatalf("Refused: %v", err)
	}
	if _, err := survey.DamperCounts(ctx); err != nil {
		t.Fatalf("DamperCounts: %v", err)
	}
	if lists := listsAll(t, asked); lists != 1 {
		t.Errorf("doctor's damper survey ran bd list --all %d times, want once:\n%s", lists, readAsked(t, asked))
	}
}
