package application_test

import (
	"strings"
	"testing"
	"time"
)

// mw-gq6.576: a claim whose session never started sat in_progress for 34
// hours with nothing in mw status to say so. A claim whose lease ran out with
// no heartbeat since is listed as stranded, on this host and on every other.

func TestStatusListsAClaimWithALapsedLeaseAsStrandedOnThisHost(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.40", "Never started", "vps")
	storyOn(t, tracker, "mw-gq6.41", "Working", "vps")
	claim(t, tracker, "mw-gq6.40", 3*time.Hour)
	claim(t, tracker, "mw-gq6.41", 5*time.Minute)

	report := otherHostStatus(t, tracker)
	printed := report.String()
	if strings.Count(printed, "STRANDED:") != 1 {
		t.Fatalf("expected exactly the claim with the lapsed lease listed as stranded, got:\n%s", printed)
	}
	for _, running := range report.Running {
		if got := !running.Stranded.IsZero(); got != (running.Detail.Story.ID == "mw-gq6.40") {
			t.Errorf("%s: stranded is %v", running.Detail.Story.ID, got)
		}
	}
}

func TestStatusListsAClaimWithALapsedLeaseAsStrandedOnAnotherHost(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.42", "Never started", "laptop")
	claim(t, tracker, "mw-gq6.42", 3*time.Hour)
	syncedAt(t, tracker, "laptop", 5*time.Minute)

	block := section(otherHostStatus(t, tracker))
	if !strings.Contains(block, "STRANDED: lease ran out") {
		t.Fatalf("expected the other host's lapsed claim listed as stranded, got:\n%s", block)
	}
}
