package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestShowRefusesWhatItCannotReadWithoutWriting(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("f-1", domain.Path{})
	tracker.AddStory("f-1", domain.Story{ID: "f-1.1", Title: "A story"})
	failing := apptest.NewFakeTracker()
	failing.Err = errors.New("bd is down")

	for name, tc := range map[string]struct {
		show application.Show
		id   string
		want string
	}{
		"no tracker":   {application.Show{}, "f-1", "no work tracker"},
		"no id":        {application.Show{Tracker: tracker}, "  ", "which epic"},
		"unknown epic": {application.Show{Tracker: tracker}, "f-2", "reading the epic f-2"},
		"a story":      {application.Show{Tracker: tracker}, "f-1.1", "f-1.1 is a story, not an epic"},
		"tracker down": {application.Show{Tracker: failing}, "f-1", "bd is down"},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			tc.show.Out = &out
			if _, err := tc.show.Run(context.Background(), tc.id); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected a refusal saying %q, got %v", tc.want, err)
			}
			if out.Len() != 0 {
				t.Errorf("expected nothing printed, got %q", out.String())
			}
		})
	}
	if got := tracker.Writes(); got != 0 {
		t.Errorf("expected no writes, got %d", got)
	}
}

func TestShowWithNowhereToPrintStillReadsTheEpic(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("f-1", domain.Path{})
	tracker.DescribeEpic("f-1", "An epic", apptest.StatusOpen, 1)

	found, err := application.Show{Tracker: tracker}.Run(context.Background(), "f-1")
	if err != nil || found.EpicID != "f-1" || found.Title != "An epic" {
		t.Errorf("expected the epic back, got %+v: %v", found, err)
	}
}
