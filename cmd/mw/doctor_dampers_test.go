package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// aBdWithAStarvedRig is aBdThatRecords with a rig the laptop holds back for
// hours, so the survey reads the rig's closed stories for the landing that
// clears the note.
func aBdWithAStarvedRig(t *testing.T) (asked string) {
	t.Helper()
	bin := t.TempDir()
	asked = filepath.Join(t.TempDir(), "asked.log")
	since := time.Now().Add(-8 * time.Hour).UTC().Format(time.RFC3339)
	notes := `{"host.laptop.starved":"postern\t` + since + `\tdrained\tdesktop is drained"}`
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + asked + "\n" +
		"case \"$*\" in\n" +
		"*\" kv \"*) printf '%s\\n' '" + notes + "' ;;\n" +
		"*) echo '[]' ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, name := range []string{"BEADS_DIR", "BEADS_DB", "BEADS_DOLT_SERVER_HOST", "BEADS_DOLT_SERVER_MODE"} {
		t.Setenv(name, "")
	}
	return asked
}

// mw-gq6.1286: the dampers line read the closed stories of each starved rig a
// second time, after the starved-rig check had read them, and each read is a
// bd list of closed stories of 2 to 4 s on a loaded laptop. The survey's tracker
// keeps what it read of a rig's closed stories for the run, so the line asks
// again for none of them.
func TestDoctorDampersDoNotReadAStarvedRigsClosedStoriesAgain(t *testing.T) {
	asked := aBdWithAStarvedRig(t)
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
	if _, err := survey.DamperCounts(ctx); err != nil {
		t.Fatalf("DamperCounts: %v", err)
	}
	reads := 0
	for _, line := range strings.Split(strings.TrimSpace(readAsked(t, asked)), "\n") {
		if strings.Contains(line, "--metadata-field rig=postern") {
			reads++
		}
	}
	if reads != 1 {
		t.Errorf("the rig's closed stories were read %d times in one doctor run, want once:\n%s", reads, readAsked(t, asked))
	}
}
