package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1487: a story handed back for its close-out alone runs the rig's tests
// in a session that dispatch starts, so it waits for the room a new Builder
// waits for: a host with no room (load, memory or drive) starts no close-out,
// leaves the story handed back, and starts it once there is room.
func TestCloseOutAgainWaitsForRoom(t *testing.T) {
	for _, tt := range []struct {
		name    string
		reading application.LoadReading
		starts  bool
	}{
		{"a drive under its floor", aDriveWith(gb / 100), false},
		{"a drive with room", aDriveWith(20 * gb), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			dispatch, tracker, _, _, _ := aFactory(t)
			id := "mw-gq6.1"
			tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "A story"})
			if err := tracker.ClaimAs(id, "fake", time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if err := tracker.SetStoryState(ctx, id, application.RunState, application.RunCloseOutAgain, "merge slot"); err != nil {
				t.Fatal(err)
			}
			dispatch.Boot.After = []string{"mw next"}
			dispatch.Load = &apptest.FakeHostLoad{Reading: tt.reading}
			dispatch.Room = application.RoomLimits{}

			report, err := dispatch.Run(ctx)
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if got := len(report.CloseOutsAgain) > 0; got != tt.starts {
				t.Errorf("ran the close-out again = %v, want %v (notes %v, passed %+v)", got, tt.starts, report.Notes, report.Passed)
			}
			if got := tracker.State(id, application.RunState); !tt.starts && got != application.RunCloseOutAgain {
				t.Errorf("run state = %q, want it left %q", got, application.RunCloseOutAgain)
			}
			var waited bool
			for _, p := range report.Passed {
				waited = waited || (p.StoryID == id && p.Gate == "no-room")
			}
			if waited == tt.starts {
				t.Errorf("passed on the no-room gate = %v, want %v (passed %+v)", waited, !tt.starts, report.Passed)
			}
		})
	}
}
