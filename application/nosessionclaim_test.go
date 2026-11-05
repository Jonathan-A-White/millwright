package application_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.877: the lease check. An in-progress claim of a dispatch whose lease
// ran out and whose Path names no host (so no session) is stuck; a live lease
// or a named host is not.
func TestNoSessionClaim(t *testing.T) {
	now := time.Date(2026, 11, 1, 11, 30, 0, 0, time.UTC)
	claimed := func(edit func(*application.StoryDetail)) application.StoryDetail {
		detail := application.StoryDetail{
			Story:        domain.Story{ID: "mw-gq6.9"},
			Status:       application.StatusInProgress,
			Assignee:     "mw@desktop",
			LeaseExpires: now.Add(-7 * time.Hour),
		}
		detail.Story.Overrides.Host = domain.HostAuto
		edit(&detail)
		return detail
	}
	cases := []struct {
		name string
		edit func(*application.StoryDetail)
		want bool
	}{
		{"expired lease, host auto, in progress", func(*application.StoryDetail) {}, true},
		{"expired lease, no host in the path at all", func(d *application.StoryDetail) { d.Story.Overrides.Host = "" }, true},
		{"a live lease", func(d *application.StoryDetail) { d.LeaseExpires = now.Add(3 * time.Minute) }, false},
		{"a lease that ran out a moment ago", func(d *application.StoryDetail) { d.LeaseExpires = now.Add(-time.Minute) }, false},
		{"no lease shown", func(d *application.StoryDetail) { d.LeaseExpires = time.Time{} }, false},
		{"a named host", func(d *application.StoryDetail) { d.Story.Overrides.Host = "desktop" }, false},
		{"open, not in progress", func(d *application.StoryDetail) { d.Status = application.StatusOpen }, false},
		{"held by someone who is not a dispatch", func(d *application.StoryDetail) { d.Assignee = "mayor" }, false},
		{"queued for its batch", func(d *application.StoryDetail) { d.Labels = []string{"run:queued"} }, false},
		{"handed back for its close-out alone", func(d *application.StoryDetail) { d.Labels = []string{"run:close-out-again"} }, false},
		{"the Governor's own", func(d *application.StoryDetail) { d.Labels = []string{application.LabelHitl} }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := application.NoSessionClaim(claimed(c.edit), now); got != c.want {
				t.Fatalf("NoSessionClaim = %v, want %v", got, c.want)
			}
		})
	}
}

// mw-gq6.998: the grace is for a claim another host's dispatch may be starting
// right now. A claim held by this very host is not being started by anyone: its
// own dispatch holds the host's lock, so the claim's lease having run out is
// all that says nothing works it.
func TestNoSessionClaimOfHost(t *testing.T) {
	now := time.Date(2026, 11, 5, 20, 15, 0, 0, time.UTC)
	claimed := func(holder string, ago time.Duration) application.StoryDetail {
		detail := application.StoryDetail{
			Story:        domain.Story{ID: "mw-hmnm2xi.2"},
			Status:       application.StatusInProgress,
			Assignee:     holder,
			LeaseExpires: now.Add(-ago),
		}
		detail.Story.Overrides.Host = domain.HostAuto
		return detail
	}
	cases := []struct {
		name   string
		detail application.StoryDetail
		want   bool
	}{
		{"this host's claim, lease ran out a minute ago", claimed("mw@cloud2", time.Minute), true},
		{"this host's claim, lease still holds", claimed("mw@cloud2", -time.Minute), false},
		{"another host's claim, lease ran out a minute ago", claimed("mw@cloud1", time.Minute), false},
		{"another host's claim, lease ran out long ago", claimed("mw@cloud1", time.Hour), true},
		{"the Mayor's claim, lease ran out long ago", claimed("mayor", time.Hour), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := application.NoSessionClaimOf(c.detail, now, "cloud2"); got != c.want {
				t.Fatalf("NoSessionClaimOf = %v, want %v", got, c.want)
			}
		})
	}
}
