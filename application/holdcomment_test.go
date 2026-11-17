package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestIsHoldComment(t *testing.T) {
	for text, want := range map[string]bool{
		"HELD: x":                               true,
		"HELD by the Mayor 921: x":              true,
		"Held by Mayor 891 2026-11-13: x":       true,
		"held: x":                               true,
		"  HELD: leading space":                 true,
		"HELD by the Mayor 921":                 true,
		"HELD x":                                true,
		"HELDOVER x":                            false,
		"HELDER: x":                             false,
		"The story was HELD: x":                 false,
		"":                                      false,
		"   ":                                   false,
		"HELD":                                  false,
		"Not HELD by the Mayor 921: x":          false,
		"\nHELD: after a blank line, still one": true,
	} {
		if got := application.IsHoldComment(text); got != want {
			t.Errorf("IsHoldComment(%q) = %v, want %v", text, got, want)
		}
	}
}

// A hold written 'HELD by the Mayor 921:' is a written reason: mw stuck leaves
// the bead alone (mw-gq6.1379).
func TestStuckDoesNotListAHoldWrittenHeldByTheMayor(t *testing.T) {
	f := newStuckFixture()
	f.bead("mw-s.1", "deferred", nil)
	f.bead("mw-s.2", "open", nil, "mw-s.1")
	mustDo(t, f.tracker.CommentOnStory(context.Background(), "mw-s.1", "HELD by the Mayor 921: waits for the rig"))
	if got := rootIDs(f.find(t)); len(got) != 0 {
		t.Errorf("roots = %v, want none", got)
	}
}

// The sleep pass's old-holds line carries the first line of a 'HELD by' comment.
func TestOldHoldsReadsAHoldWrittenHeldByTheMayor(t *testing.T) {
	now := time.Date(2026, 11, 16, 7, 30, 0, 0, time.UTC)
	then := now.Add(-10 * 24 * time.Hour)
	h := &heldBeads{
		beads: []application.TriageBead{{ID: "h-by", Title: "By", Status: "deferred", Created: then, Updated: then}},
		comments: map[string][]application.Comment{"h-by": {
			{Created: then, Text: "HELD by the Mayor 921: waits for the rig\nsecond line"},
		}},
	}
	holds, err := application.OldHolds(context.Background(), h, now)
	mustDo(t, err)
	if len(holds) != 1 || holds[0].Reason != "HELD by the Mayor 921: waits for the rig" {
		t.Errorf("holds = %+v, want one with the comment's first line as its reason", holds)
	}
}

var _ = domain.Path{}
