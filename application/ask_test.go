package application_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

func at(day int, hour int) time.Time {
	return time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC)
}

func TestWorkingDaysSkipTheWeekend(t *testing.T) {
	// October 2026: the 5th is a Monday, the 9th a Friday, the 10th a Saturday.
	cases := []struct {
		from time.Time
		n    int
		want time.Time
	}{
		{at(5, 10), 3, at(8, 10)},   // Monday + 3 is Thursday
		{at(7, 10), 3, at(12, 10)},  // Wednesday + 3 is Monday
		{at(9, 10), 3, at(14, 10)},  // Friday + 3 is Wednesday
		{at(10, 10), 3, at(14, 10)}, // Saturday + 3 is Wednesday
		{at(11, 10), 3, at(14, 10)}, // Sunday + 3 is Wednesday
		{at(9, 10), 0, at(9, 10)},
	}
	for _, c := range cases {
		if got := application.AddWorkingDays(c.from, c.n); !got.Equal(c.want) {
			t.Errorf("%s + %d working days: want %s, got %s", c.from.Weekday(), c.n, c.want, got)
		}
	}
}

func TestAnAskIsReadBackFromTheLabels(t *testing.T) {
	d := application.StoryDetail{Story: domain.Story{ID: "mw-x.1"}, Labels: []string{
		"Asked-By:pat", "asked-of:sam", "ask:tl", "waiting:others", "hitl"}}
	if got := d.AskedBy(); got != "pat" {
		t.Errorf("asked by: want pat, got %q", got)
	}
	if got := d.AskedOf(); got != "sam" {
		t.Errorf("asked of: want sam, got %q", got)
	}
	if got := d.AskRole(); got != "tl" {
		t.Errorf("role: want tl, got %q", got)
	}
	if !d.WaitingOnOthers() {
		t.Error("expected the bead to be waiting on others")
	}
	bare := application.StoryDetail{Labels: []string{"asked-by-nobody", "asked:x"}}
	if bare.AskedBy() != "" || bare.AskedOf() != "" || bare.AskRole() != "" || bare.WaitingOnOthers() {
		t.Error("expected labels that only look alike to name no ask")
	}
}
