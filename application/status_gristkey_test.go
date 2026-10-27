package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A ready story that mw dispatch passes by on this host for want of a grist test
// key is shown with the reason and the host that takes it (mw-1knt69p).
func TestStatusSaysWhyAReadyStoryNeedingTheGristKeyIsPassedByOnAHostWithNone(t *testing.T) {
	for name, tc := range map[string]struct {
		files   string
		hasKey  func() bool
		wantWhy bool
	}{
		"a files key under grinds/, no key":    {"grinds/examples/word-list/x.json", func() bool { return false }, true},
		"a files key under grinds/, key held":  {"grinds/examples/word-list/x.json", func() bool { return true }, false},
		"no grinds/ file, no key":              {"application/x.go", func() bool { return false }, false},
		"a files key under grinds/, not asked": {"grinds/examples/word-list/x.json", nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			tracker := aTrackerPathedToVPS(t)
			story := domain.Story{ID: "mw-gq6.30", Title: "Add an example"}
			for field, value := range map[string]string{"host": domain.HostAuto, "files": tc.files} {
				if err := story.Overrides.Set(field, value); err != nil {
					t.Fatal(err)
				}
			}
			tracker.AddStory("mw-gq6", story)
			report, err := application.Status{
				Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
				HasGristTestKey: tc.hasKey,
				Now:             func() time.Time { return statusNow },
			}.Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			printed := report.String()
			if got := strings.Contains(printed, "passed by vps: no grist test key"); got != tc.wantWhy {
				t.Fatalf("want the reason = %v, got:\n%s", tc.wantWhy, printed)
			}
			if tc.wantWhy && !strings.Contains(printed, "a host that has one takes it") {
				t.Errorf("expected the line to say which host takes it, got:\n%s", printed)
			}
		})
	}
}
