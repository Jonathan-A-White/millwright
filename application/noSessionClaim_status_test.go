package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
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

// mw-gq6.998: mw-hmnm2xi.2 was claimed by mw@cloud2 and never launched; the
// doctor on cloud2 said cannot-tell for twenty minutes, though the claim was
// its own host's and nothing was running it. A claim this host holds with no
// session and a lapsed lease it gives back itself; another host's is still
// only named.
func ownClaimsCheck(tracker *apptest.FakeTracker, runner *apptest.FakeRunner) *application.NoSessionClaimsCheck {
	tracker.Clock = func() time.Time { return statusNow }
	return &application.NoSessionClaimsCheck{
		Claims:   tracker.StaleClaims,
		Now:      func() time.Time { return statusNow },
		Host:     "desktop",
		Sessions: runner,
		Tracker:  tracker,
	}
}

func TestDoctorGivesBackAClaimItsOwnHostHoldsWithNoSessionAndALapsedLease(t *testing.T) {
	ctx := context.Background()
	tracker := aTrackerPathedToVPS(t)
	anUnhostedClaim(t, tracker, "mw-gq6.50", "mw@desktop", statusNow, 2*time.Minute)
	anUnhostedClaim(t, tracker, "mw-gq6.51", "mw@vps", statusNow, 7*time.Hour)
	check := ownClaimsCheck(tracker, apptest.NewFakeRunner())

	verdict, why := check.Probe(ctx)
	if verdict != application.DoctorFaulty || !strings.Contains(why, "mw-gq6.50") {
		t.Fatalf("expected this host's own claim faulty, got %v %q", verdict, why)
	}
	if err := check.Cure(ctx); err != nil {
		t.Fatalf("expected the cure to give the claim back, got %v", err)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.50")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != application.StatusOpen || detail.Assignee != "" {
		t.Fatalf("expected the claim given back, got status %q assignee %q", detail.Status, detail.Assignee)
	}
	if comments := tracker.Comments("mw-gq6.50"); len(comments) == 0 || !strings.Contains(comments[len(comments)-1], "desktop") {
		t.Fatalf("expected a comment saying this host gave the claim back, got %q", comments)
	}
	if way := check.WayBack(); !strings.Contains(way, "mw-gq6.50") || strings.Contains(way, "mw-gq6.51") {
		t.Fatalf("expected the way back to name what was given back, got %q", way)
	}
	other, err := tracker.ShowStory(ctx, "mw-gq6.51")
	if err != nil || other.Status != application.StatusInProgress {
		t.Fatalf("expected another host's claim left in progress, got %+v %v", other, err)
	}
	if verdict, why := check.Probe(ctx); verdict != application.DoctorCannotTell || strings.Contains(why, "mw-gq6.50") || !strings.Contains(why, "mw-gq6.51") {
		t.Fatalf("expected only the other host's claim still named, got %v %q", verdict, why)
	}
}

func TestDoctorKeepsAClaimItsOwnHostHoldsWhileASessionRunsOrItsCloseOutRefusedIt(t *testing.T) {
	ctx := context.Background()
	tracker := aTrackerPathedToVPS(t)
	runner := apptest.NewFakeRunner()
	anUnhostedClaim(t, tracker, "mw-gq6.50", "mw@desktop", statusNow, 2*time.Minute)
	anUnhostedClaim(t, tracker, "mw-gq6.51", "mw@desktop", statusNow, 2*time.Minute)
	startOldSession(t, runner, "mw-gq6.50")
	if err := tracker.SetStoryState(ctx, "mw-gq6.51", application.RunState, application.RunBlocked, "refused"); err != nil {
		t.Fatalf("recording the refusal: %v", err)
	}
	check := ownClaimsCheck(tracker, runner)

	if verdict, why := check.Probe(ctx); verdict == application.DoctorFaulty {
		t.Fatalf("expected nothing to cure, got %v %q", verdict, why)
	}
	if err := check.Cure(ctx); err != nil {
		t.Fatalf("expected a cure with nothing to do to succeed, got %v", err)
	}
	for _, id := range []string{"mw-gq6.50", "mw-gq6.51"} {
		if detail, err := tracker.ShowStory(ctx, id); err != nil || detail.Status != application.StatusInProgress {
			t.Fatalf("expected %s kept, got %+v %v", id, detail, err)
		}
	}
}
