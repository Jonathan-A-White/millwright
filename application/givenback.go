package application

import (
	"context"
	"fmt"
	"strings"
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
	return giveBackClaimTo(ctx, tracker, id, StatusOpen, release, wait, pause)
}

// giveBackClaimTo is giveBackClaim for a give-back that leaves the story in the
// status named: StatusHeld for one that keeps the Mayor's hold, StatusOpen
// otherwise. A write that bd reported done and that did not take is a failed
// give-back, and what is said of the claim on the story waits for it to read
// as it should (mw-gq6.1493, as mw retry's giveBackTo, mw-gq6.1465).
func giveBackClaimTo(ctx context.Context, tracker WorkTracker, id, status string, release func(context.Context) error,
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
		case !strings.EqualFold(strings.TrimSpace(detail.Status), status) || detail.Assignee != "":
			last = fmt.Errorf("the give-back was accepted, but %s still reads %s, held by %q", id, detail.Status, detail.Assignee)
		default:
			return nil
		}
	}
	return fmt.Errorf("%w (after %d tries)", last, GiveBackTries)
}

// givenBackAs is the outcome of one give-back write, err its error, judged by
// the story: nil when the story reads status and unclaimed, whatever err said (a
// try that failed and left it so did land, as a write on a connection that
// dropped can); a passing fault of the beads server is err as it was, to be
// tried again; and a write that returned nil with the story unchanged is an
// error, since only the story's own state says a claim was given back
// (mw-gq6.1493).
func givenBackAs(ctx context.Context, tracker WorkTracker, id, status string, err error) error {
	if _, passing := Faulted(err); passing {
		return err
	}
	detail, readErr := tracker.ShowStory(ctx, id)
	switch {
	case readErr == nil && strings.EqualFold(strings.TrimSpace(detail.Status), status) && detail.Assignee == "":
		return nil
	case err != nil:
		return err
	case readErr != nil:
		return fmt.Errorf("the give-back was accepted, but reading %s back failed: %w", id, readErr)
	default:
		return fmt.Errorf("the give-back was accepted, but %s still reads %s, held by %q", id, detail.Status, detail.Assignee)
	}
}
