package application

import (
	"context"
	"fmt"
	"time"
)

// GiveBackTries is how many times a claim's give-back is tried before it is
// reported as still held, and GiveBackWait how long dispatch waits between the
// tries. A give-back that shares a connection with the failure that made it
// necessary can fail itself, or be reported done and not take (mw-gq6.372).
const (
	GiveBackTries = 3
	GiveBackWait  = 2 * time.Second
)

// giveBackClaim runs release, and reads the story back to see that it is open
// and unclaimed, until it is or the tries are used up. Only the story's own
// state says the claim was given back, never the release call returning: the
// error is what the last try found. wait is how the pause between tries is
// made, pause its length.
func giveBackClaim(ctx context.Context, tracker WorkTracker, id string, release func(context.Context) error,
	wait func(context.Context, time.Duration) error, pause time.Duration) error {
	var last error
	for try := 1; try <= GiveBackTries; try++ {
		if try > 1 {
			if err := wait(ctx, pause); err != nil {
				return fmt.Errorf("%w (and waiting %s to try giving it back again: %v)", last, pause, err)
			}
		}
		if err := release(ctx); err != nil {
			last = err
			continue
		}
		detail, err := tracker.ShowStory(ctx, id)
		switch {
		case err != nil:
			last = fmt.Errorf("the give-back was accepted, but reading %s back failed: %w", id, err)
		case detail.Status != StatusOpen || detail.Assignee != "":
			last = fmt.Errorf("the give-back was accepted, but %s still reads %s, held by %q", id, detail.Status, detail.Assignee)
		default:
			return nil
		}
	}
	return fmt.Errorf("%w (after %d tries)", last, GiveBackTries)
}
