package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// mw-6ww.175.24: mw report with no --rig took over 5 minutes at load 15, most
// of it in `bd list --all`, which it ran afresh for each read (its stories
// since, then each of the damper counts' reads): it kept no run cache. The
// tracker it reads from asks bd for the list of every bead once, however many
// of those reads it makes.
func TestReportTrackerReadsEveryBeadOnce(t *testing.T) {
	asked := aBdThatRecords(t)
	var said bytes.Buffer

	tracker, stop := reportTracker(t.TempDir(), "laptop", &said)
	defer stop()
	ctx := context.Background()
	if _, err := tracker.StoriesSince(ctx, time.Time{}); err != nil {
		t.Fatal(err)
	}
	survey, err := damperSurvey(tracker, t.TempDir(), "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := survey.DamperCounts(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.StoriesSince(ctx, time.Time{}); err != nil {
		t.Fatal(err)
	}
	var lists int
	for _, line := range strings.Split(strings.TrimSpace(readAsked(t, asked)), "\n") {
		if strings.Contains(line, " list --all") {
			lists++
		}
	}
	if lists != 1 {
		t.Errorf("bd ran list --all %d times for one report, want once:\n%s", lists, readAsked(t, asked))
	}
}
