package application

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Show prints the tree of a plan that was filed earlier and stops. The Governor
// approves a plan from a phone, after the Mayor filed it, and has to see what he
// is approving first: this is the tree `mw release` prints, without the release.
//
// It is the reading half of Release and nothing more. Every call it makes is one
// ShowEpic, so nothing is written to the tracker, whatever state the stories are
// in.
type Show struct {
	Tracker WorkTracker

	// Out is where the tree is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run prints the tree of one already-filed epic and reports the epic as it was
// found. An id the tracker has no epic under, or that names a bead that is not
// an epic, is a refusal.
func (s Show) Run(ctx context.Context, epicID string) (FiledPlan, error) {
	if s.Tracker == nil {
		return FiledPlan{}, fmt.Errorf("showing a plan: there is no work tracker to read it from")
	}
	if strings.TrimSpace(epicID) == "" {
		return FiledPlan{}, fmt.Errorf("showing a plan: which epic?")
	}

	epic, err := s.Tracker.ShowEpic(ctx, epicID)
	if err != nil {
		return FiledPlan{}, fmt.Errorf("reading the epic %s: %w", epicID, err)
	}
	found := filedFrom(epic)
	if s.Out != nil {
		fmt.Fprint(s.Out, found.Tree())
	}
	return found, nil
}
