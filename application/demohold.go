package application

import (
	"context"
	"fmt"
)

// DemoHeldLine is the line a landed demo's comment ends with: a demo is closed
// by the Governor's "Looks good" and by nothing else, so landing leaves it open.
const DemoHeldLine = "A demo: left open and held for the Governor's 'Looks good'."

// DemoLeftOpen is what a landing's report says in place of "the story is
// closed" for a demo.
const DemoLeftOpen = "left open (a demo), held for the Governor's 'Looks good'"

// holdLandedDemo leaves a landed demo open and held instead of closing it: the
// landing comment (outcome, then DemoHeldLine) is written, the claim given
// back so that nobody holds the bead the Governor's word is to close, and the
// story held (StatusHeld) so no dispatcher takes it again. holder is who the
// claim is released from, empty for the tracker's own actor. A comment that
// cannot be written is a note, and the hold is still done; the hold and the
// release are the error, and they are safe to try again whichever of them
// went through.
func holdLandedDemo(ctx context.Context, tracker WorkTracker, id, outcome, holder string) (notes []string, err error) {
	detail, err := tracker.ShowStory(ctx, id)
	if err != nil {
		return nil, err
	}
	if !detail.Held() {
		said := outcome + "\n\n" + DemoHeldLine
		if err := tracker.CommentOnStory(ctx, id, said); err != nil {
			notes = append(notes, fmt.Sprintf("%s: the landing comment could not be written on the demo: %v", id, err))
		}
	}
	if detail.Assignee != "" {
		release := tracker.ReleaseClaim
		if holder != "" {
			release = func(ctx context.Context, id string) error { return tracker.ReleaseClaimHeldBy(ctx, id, holder) }
		}
		if err := release(ctx, id); err != nil {
			return notes, fmt.Errorf("giving back the claim on the demo %s: %w", id, err)
		}
	}
	if err := tracker.HoldStory(ctx, id); err != nil {
		return notes, fmt.Errorf("holding the demo %s: %w", id, err)
	}
	return notes, nil
}
