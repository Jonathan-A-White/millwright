package application_test

import (
	"context"
	"github.com/Jonathan-A-White/millwright/application"
	"strings"
	"testing"
	"time"
)

// mw-gq6.877: a claim no host's Path names was in neither RUNNING nor OTHER
// HOSTS, so mw status said nothing of it for seven hours.

func TestStatusNamesAnUnhostedClaimWithALapsedLeaseAsStuck(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	anUnhostedClaim(t, tracker, "mw-gq6.50", "mw@desktop", statusNow, 7*time.Hour)
	anUnhostedClaim(t, tracker, "mw-gq6.51", "mw@desktop", statusNow, -2*time.Minute)

	report := otherHostStatus(t, tracker)
	printed := report.String()
	if strings.Count(printed, "STUCK:") != 1 {
		t.Fatalf("expected exactly the claim with the lapsed lease named STUCK, got:\n%s", printed)
	}
	var line string
	for _, l := range strings.Split(printed, "\n") {
		if strings.Contains(l, "STUCK:") {
			line = l
		}
	}
	if !strings.Contains(line, "mw@desktop") || !strings.Contains(printed, "mw retry mw-gq6.50") {
		t.Fatalf("expected the STUCK line to name the holder and the fix, got:\n%s", printed)
	}
	if !strings.Contains(printed, "mw-gq6.50") {
		t.Fatalf("expected the story listed, got:\n%s", printed)
	}
}

func TestStatusDoesNotNameAHostedClaimStuckAsAnUnhostedOne(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.52", "Never started", "vps")
	claim(t, tracker, "mw-gq6.52", 3*time.Hour)

	printed := otherHostStatus(t, tracker).String()
	if strings.Contains(printed, "STUCK") {
		t.Fatalf("expected a claim a host names left to STRANDED, got:\n%s", printed)
	}
}

func TestDoctorNamesAnUnhostedClaimWithALapsedLeaseAsStuck(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	anUnhostedClaim(t, tracker, "mw-gq6.50", "mw@desktop", statusNow, 7*time.Hour)
	anUnhostedClaim(t, tracker, "mw-gq6.51", "mw@desktop", statusNow, -2*time.Minute)
	check := &application.NoSessionClaimsCheck{Claims: tracker.StaleClaims, Now: func() time.Time { return statusNow }}

	verdict, why := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(why, "STUCK") || !strings.Contains(why, "mw-gq6.50") || strings.Contains(why, "mw-gq6.51") {
		t.Fatalf("expected only mw-gq6.50 named STUCK, got %v %q", verdict, why)
	}

	clean := &application.NoSessionClaimsCheck{Claims: aTrackerPathedToVPS(t).StaleClaims, Now: func() time.Time { return statusNow }}
	if verdict, why := clean.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok with nothing stuck, got %v %q", verdict, why)
	}
}
