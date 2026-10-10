package application

import (
	"context"
	"fmt"
)

// readNeeded reads, in one ShowBeads, every bead that stories wait on and
// lookup does not hold yet, and puts them in lookup. Over a link with a long
// round trip every read of the tracker is seconds, so a loop that read what a
// story waits on one need at a time cost a row of them (mw-gq6.358). A need
// the tracker has no record of is an error naming the story that waits on it.
func readNeeded(ctx context.Context, tracker WorkTracker, stories []StoryDetail, lookup map[string]StoryDetail) error {
	var wanted []string
	waiter := map[string]string{}
	for _, story := range stories {
		for _, need := range story.Needs {
			if _, known := lookup[need]; known {
				continue
			}
			if _, asked := waiter[need]; !asked {
				waiter[need] = story.Story.ID
				wanted = append(wanted, need)
			}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	read, err := tracker.ShowBeads(ctx, wanted)
	if err != nil {
		return fmt.Errorf("reading what the stories wait on: %w", err)
	}
	for _, blocker := range read {
		lookup[blocker.Story.ID] = blocker
	}
	for _, need := range wanted {
		if _, found := lookup[need]; !found {
			return fmt.Errorf("reading %s, which %s waits on: no such bead", need, waiter[need])
		}
	}
	return nil
}
