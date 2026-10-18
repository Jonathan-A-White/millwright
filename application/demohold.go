package application

import (
	"context"
	"fmt"
	"strings"
)

// DemoHeldLine is the line a landed demo's comment ends with: a demo is closed
// by the Governor's "Looks good" and by nothing else, so landing leaves it open.
const DemoHeldLine = "A demo: left open for the Governor's 'Looks good'."

// DemoLeftOpen is what a landing's report says in place of "the story is
// closed" for a demo.
const DemoLeftOpen = "left open (a demo), waiting for the Governor's 'Looks good'"

// demoWaits reports whether a landed demo is as landing leaves it: open,
// labelled hitl so that no dispatcher takes it and his review queue lists it,
// and unclaimed so that his word can close it.
func demoWaits(detail StoryDetail) bool {
	return !detail.Closed() && detail.Assignee == "" && hasLabel(detail.Labels, LabelHitl)
}

// holdLandedDemo leaves a landed demo open and waiting for the Governor instead
// of closing it: the landing comment (outcome, then DemoHeldLine, naming the
// doc the demo's files key names) is written, the label hitl added so that no
// dispatcher takes it again and it is listed as waiting for him, and the claim
// given back so that nobody holds the bead his word is to close. holder is who
// the claim is released from, empty for whoever holds it now. A comment that
// cannot be written is a note, and the rest is still done; the label and the
// release are the error, and they are safe to try again whichever of them went
// through (the comment is written only while the label is not yet there).
func holdLandedDemo(ctx context.Context, tracker WorkTracker, id, outcome, holder string) (notes []string, err error) {
	detail, err := tracker.ShowStory(ctx, id)
	if err != nil {
		return nil, err
	}
	if !hasLabel(detail.Labels, LabelHitl) {
		said := outcome + "\n\n" + DemoHeldLine
		if files := strings.TrimSpace(detail.Story.Overrides.Files); files != "" {
			said += " The landed doc: " + files + "."
		}
		if err := tracker.CommentOnStory(ctx, id, said); err != nil {
			notes = append(notes, fmt.Sprintf("%s: the landing comment could not be written on the demo: %v", id, err))
		}
		if err := tracker.AddLabel(ctx, id, LabelHitl); err != nil {
			return notes, fmt.Errorf("labelling the demo %s %s: %w", id, LabelHitl, err)
		}
	}
	if detail.Assignee != "" {
		if holder == "" {
			holder = detail.Assignee
		}
		if err := tracker.ReleaseClaimHeldBy(ctx, id, holder); err != nil {
			return notes, fmt.Errorf("giving back the claim on the demo %s: %w", id, err)
		}
	}
	return notes, nil
}
