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
