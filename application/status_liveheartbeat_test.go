package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.878: the Mayor held a story just after dispatch claimed it and then
// released it, which wrote 'open' over a story whose session was running, and
// mw status, which lists RUNNING by status, lost it. A claim whose lease a
// heartbeat keeps live is running whatever its status says.
func TestStatusListsAStoryWithALiveHeartbeatUnderRunningWhateverItsStatus(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		lease        time.Duration // from statusNow
		running      bool
	}{
		{"set back to open, lease live", application.StatusOpen, time.Minute, true},
		{"held, lease live", application.StatusHeld, time.Minute, true},
		{"in progress, lease live", application.StatusInProgress, time.Minute, true},
		{"set back to open, lease run out", application.StatusOpen, -time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracker := aTrackerPathedToVPS(t)
			storyOn(t, tracker, "mw-gq6.30", "Runs", "vps")
			if err := tracker.ClaimAs("mw-gq6.30", "mw@vps", statusNow.Add(tc.lease)); err != nil {
				t.Fatal(err)
			}
			if err := tracker.SetStatus("mw-gq6.30", tc.status); err != nil {
				t.Fatal(err)
			}
			report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
				Now: func() time.Time { return statusNow }}.Run(context.Background())
			if err != nil {
				t.Fatalf("reading status: %v", err)
			}
			var listed bool
			for _, rs := range report.Running {
				listed = listed || rs.Detail.Story.ID == "mw-gq6.30"
			}
			if listed != tc.running {
				t.Fatalf("expected RUNNING to list mw-gq6.30: %v, got %v (running %+v)", tc.running, listed, report.Running)
			}
			if tc.running && strings.Contains(report.String(), application.OpenButClaimedHeading) {
				t.Errorf("a running story is not also listed under %s:\n%s", application.OpenButClaimedHeading, report.String())
			}
		})
	}
}
