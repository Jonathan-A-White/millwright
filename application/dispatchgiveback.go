package application

import (
	"context"
	"fmt"
)

// giveBackRead gives a claim back by write and reads the story back until it is
// in status and unclaimed (giveBackClaimTo), pausing as the dispatch pauses
// between give-back tries: by its Wait, or really when it has none. The comment
// that says a claim was given back is written only once this returns nil
// (mw-gq6.1493).
func (d Dispatch) giveBackRead(ctx context.Context, id, status string, write func(context.Context) error) error {
	wait := d.Wait
	if wait == nil {
		wait = waitFor
	}
	return giveBackClaimTo(ctx, d.Tracker, id, status, write, wait, GiveBackWait)
}

// reclaimRead is Tracker.ReclaimStory for the dead-pane reclaim, with the
// give-back read back: a reclaim bd said was done and that left the story
// claimed is asked again (a lease that has run out stays run out), and one that
// never takes is an error and not a reclaim, so no comment says the claim was
// given back while the story still holds it (mw-gq6.1493). A lease that still
// holds is not reclaimed and nothing is read back.
func (d Dispatch) reclaimRead(ctx context.Context, id string) (bool, error) {
	reclaimed, err := d.Tracker.ReclaimStory(ctx, id)
	if err != nil || !reclaimed {
		return false, err
	}
	first := true
	err = d.giveBackRead(ctx, id, StatusOpen, func(ctx context.Context) error {
		if first {
			first = false
			return nil
		}
		_, err := d.Tracker.ReclaimStory(ctx, id)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("the claim on %s could not be given back: %w", id, err)
	}
	return true, nil
}
