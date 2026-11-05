package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.877: mw-5r3p30.940 was claimed by mw@desktop at 04:15Z, and the
// dispatch that claimed it died before it wrote the story's host over "auto".
// A story pathed to "auto" is on no host's list of what is running, so no
// host's dispatch looked at the claim and it sat in_progress for seven hours.

// anUnhostedClaim adds a story pathed to host "auto" and claims it as holder,
// with a lease that ran out ago before now: the claim a dispatch left when it
// died between claiming and recording the host.
func anUnhostedClaim(t *testing.T, tracker interface {
	AddStory(epic string, story domain.Story)
	ClaimAs(id, holder string, until time.Time) error
}, id, holder string, now time.Time, ago time.Duration) {
	t.Helper()
	story := domain.Story{ID: id, Title: "A story claimed by a dispatch that died"}
	if err := story.Overrides.Set("host", domain.HostAuto); err != nil {
		t.Fatalf("pathing %s to auto: %v", id, err)
	}
	tracker.AddStory("mw-gq6", story)
	if err := tracker.ClaimAs(id, holder, now.Add(-ago)); err != nil {
		t.Fatalf("claiming %s: %v", id, err)
	}
}

func TestDispatchGivesBackAnUnhostedClaimWhoseLeaseRanOutAndWhoseSessionNeverStarted(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	now := time.Date(2026, 11, 1, 11, 30, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	// Claimed by the desktop; this is the VPS's dispatch, which names no host for it.
	anUnhostedClaim(t, tracker, "mw-gq6.9", "mw@desktop", now, 7*time.Hour)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Reclaimed) != 1 || report.Reclaimed[0].StoryID != "mw-gq6.9" {
		t.Fatalf("expected the claim no host names given back, got %+v", report.Reclaimed)
	}
	comments := tracker.Comments("mw-gq6.9")
	if len(comments) == 0 || !strings.Contains(comments[0], "no host") {
		t.Fatalf("expected a comment saying no host named the claim, got %q", comments)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.9" {
		t.Fatalf("expected the story, open again, started on this host, got %+v", report.Started)
	}
}

func TestDispatchLeavesAnUnhostedClaimAloneWhileItsLeaseHoldsOrAHostNamesIt(t *testing.T) {
	cases := map[string]struct {
		ago    time.Duration
		holder string
		host   string
	}{
		"the lease still holds":               {ago: -3 * time.Minute, holder: "mw@desktop", host: domain.HostAuto},
		"the lease ran out a moment ago":      {ago: time.Minute, holder: "mw@desktop", host: domain.HostAuto},
		"the Mayor, not a dispatch, holds it": {ago: 7 * time.Hour, holder: "mayor", host: domain.HostAuto},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			dispatch, tracker, _, _, _ := aFactory(t)
			now := time.Date(2026, 11, 1, 11, 30, 0, 0, time.UTC)
			dispatch.Now = func() time.Time { return now }
			tracker.Clock = func() time.Time { return now }
			anUnhostedClaim(t, tracker, "mw-gq6.9", c.holder, now, c.ago)

			report, err := dispatch.Run(ctx)
			if err != nil {
				t.Fatalf("expected the dispatch to run cleanly, got %v", err)
			}
			if len(report.Reclaimed) != 0 {
				t.Fatalf("expected the claim left alone, got %+v", report.Reclaimed)
			}
			detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
			if err != nil {
				t.Fatalf("showing the story: %v", err)
			}
			if detail.Status != application.StatusInProgress || detail.Assignee != c.holder {
				t.Fatalf("expected the claim as it was, got status %q assignee %q", detail.Status, detail.Assignee)
			}
		})
	}
}

func TestDispatchLeavesAnUnhostedClaimAloneWhenASessionOfItsNameIsRunningHere(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	now := time.Date(2026, 11, 1, 11, 30, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	anUnhostedClaim(t, tracker, "mw-gq6.9", "mw@vps", now, 7*time.Hour)
	startOldSession(t, runner, "mw-gq6.9")

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Reclaimed) != 0 {
		t.Fatalf("expected the claim of a running session left alone, got %+v", report.Reclaimed)
	}
}

// mw-gq6.998: mw-hmnm2xi.2 was claimed by mw@cloud2 outside a dispatch tick (the
// close-out of its predecessor chains a dispatch) and never launched; it was
// pathed to "auto", so the claim was no host's to reclaim until the lease and
// NoSessionClaimGrace had both run out, and cloud2 sat idle for twenty minutes
// with the story its own claim held.
func TestDispatchLaunchesAStoryItsOwnHostClaimedWithNoSessionOnceTheLeaseRanOut(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	now := time.Date(2026, 11, 5, 20, 15, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	// The lease ran out four minutes ago: past the lease, well inside the grace.
	anUnhostedClaim(t, tracker, "mw-gq6.9", "mw@vps", now, 4*time.Minute)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Reclaimed) != 1 || report.Reclaimed[0].StoryID != "mw-gq6.9" {
		t.Fatalf("expected this host's own claim, with no session and a lapsed lease, given back, got %+v", report.Reclaimed)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.9" {
		t.Fatalf("expected the story launched in the same tick, got %+v", report.Started)
	}
	if printed := report.String(); !strings.Contains(printed, "reclaimed mw-gq6.9") || !strings.Contains(printed, "mw-gq6.9") {
		t.Fatalf("expected the tick's output to name the story, got:\n%s", printed)
	}
	comments := tracker.Comments("mw-gq6.9")
	if len(comments) == 0 || !strings.Contains(comments[0], "vps") {
		t.Fatalf("expected a comment saying this host gave its own claim back, got %q", comments)
	}
}

func TestDispatchLeavesItsOwnHostsClaimAloneWhileItsLeaseHoldsOrASessionRuns(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		ago     time.Duration
		running bool
	}{
		"the lease still holds":  {ago: -time.Minute},
		"a session of its name":  {ago: time.Hour, running: true},
		"the lease just ran out": {ago: -time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, runner, _ := aFactory(t)
			now := time.Date(2026, 11, 5, 20, 15, 0, 0, time.UTC)
			dispatch.Now = func() time.Time { return now }
			tracker.Clock = func() time.Time { return now }
			anUnhostedClaim(t, tracker, "mw-gq6.9", "mw@vps", now, c.ago)
			if c.running {
				startOldSession(t, runner, "mw-gq6.9")
			}
			report, err := dispatch.Run(ctx)
			if err != nil {
				t.Fatalf("expected the dispatch to run cleanly, got %v", err)
			}
			if len(report.Reclaimed) != 0 {
				t.Fatalf("expected the claim left alone, got %+v", report.Reclaimed)
			}
		})
	}
}
