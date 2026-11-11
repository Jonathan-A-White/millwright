package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
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

// mw-6ww.163.1: a host the Governor retired is said once and then left alone.

func retiredStatus(t *testing.T, tracker *apptest.FakeTracker, retired ...string) application.StatusReport {
	t.Helper()
	report, err := application.Status{
		Tracker:     tracker,
		Notes:       tracker,
		Host:        "laptop",
		Seat:        "builder",
		HostSilence: 2 * time.Hour,
		Retired:     retired,
		Now:         func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}

func TestStatusSaysARetiredHostInOneLineWithNoSleepStrandedOrDoctorWords(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	// Last synced three days ago, with a doctor verdict that is long stale.
	syncedAt(t, tracker, "desktop", 72*time.Hour)
	if err := tracker.SetNote(context.Background(), application.DoctorVerdictKey("desktop"), "2026-01-01T00:00:00Z faulty:wifi=down"); err != nil {
		t.Fatal(err)
	}

	block := section(retiredStatus(t, tracker, "desktop", "vps"))
	for _, want := range []string{"OTHER HOSTS (2)", "  desktop · retired", "  vps · retired"} {
		if !strings.Contains(block, want) {
			t.Errorf("expected %q, got:\n%s", want, block)
		}
	}
	for _, not := range []string{"ASLEEP", "stranded", "re-path", "doctor", "last sync", "synced"} {
		if strings.Contains(block, not) {
			t.Errorf("expected nothing of %q under a retired host, got:\n%s", not, block)
		}
	}
	if lines := strings.Count(block, "\n") + 1; lines != 3 {
		t.Errorf("expected the heading and one line a retired host, got %d lines:\n%s", lines, block)
	}
}

func TestStatusStillShowsWorkPathedToARetiredHostOnceUnderALineThatReroutesIt(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.50", "Left on the desktop", "desktop")
	storyOn(t, tracker, "mw-gq6.51", "Claimed there", "desktop")
	claim(t, tracker, "mw-gq6.51", 3*time.Hour)

	printed := retiredStatus(t, tracker, "desktop").String()
	block := section(retiredStatus(t, tracker, "desktop"))
	if !strings.Contains(block, "desktop · retired") {
		t.Fatalf("expected the retired line, got:\n%s", block)
	}
	if n := strings.Count(block, "re-path: "+application.RepathHint+"laptop"); n != 1 {
		t.Errorf("expected one line saying how to re-path, got %d:\n%s", n, block)
	}
	for _, id := range []string{"mw-gq6.50", "mw-gq6.51"} {
		if n := strings.Count(printed, id+" · "); n != 1 {
			t.Errorf("expected %s shown once, got %d:\n%s", id, n, printed)
		}
	}
	for _, not := range []string{"ASLEEP", "stranded", "STRANDED"} {
		if strings.Contains(block, not) {
			t.Errorf("expected nothing of %q for work on a retired host, got:\n%s", not, block)
		}
	}
}

func TestStatusWithNoRetiredHostsIsAsItWas(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	report := retiredStatus(t, tracker)
	if len(report.Others) != 0 || strings.Contains(report.String(), "retired") {
		t.Fatalf("expected no retired host in the report, got:\n%s", report.String())
	}
}
