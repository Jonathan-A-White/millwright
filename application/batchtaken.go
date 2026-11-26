package application

import (
	"fmt"
	"time"
)

// batchHolding is the mark of the running batch that has taken story detail, when
// there is one: the story is queued for batch landing and carries a closing-out
// mark whose lease still holds at now (holdQueue writes one on every story a
// batch takes, and renews it while the batch runs). Such a story is that
// batch's: setting it aside or giving it a new attempt would not take it out of
// the batch, and the batch would land it all the same (mw-gq6.1761).
func batchHolding(detail StoryDetail, now time.Time) (ClosingOut, bool) {
	if !QueuedForBatch(detail) || !detail.ClosingOut.Live(now) {
		return ClosingOut{}, false
	}
	return detail.ClosingOut, true
}

// words names the batch a mark is of, for a story of rig: "batch 730 of lampas,
// landing on laptop since 2026-11-26 19:57Z".
func (c ClosingOut) batchWords(rig string) string {
	which := "a batch of " + rig
	if c.Batch > 0 {
		which = BatchLabel(rig, c.Batch)
	}
	return fmt.Sprintf("%s, landing on %s since %s", which, c.By, c.Since.UTC().Format(QueuedClock))
}

// inBatchWords is the same for a sentence that says the story is in the batch:
// "batch 730 of lampas" or "a running batch of lampas".
func (c ClosingOut) inBatchWords(rig string) string {
	if c.Batch > 0 {
		return BatchLabel(rig, c.Batch)
	}
	return "a running batch of " + rig
}

// refusedForBatch is why a hand that changes a queued story's claim (mw land
// --set-aside, mw retry) is refused while a running batch holds the story.
func (c ClosingOut) refusedForBatch(rig, id string) string {
	return fmt.Sprintf("it has been taken by %s (its lease holds until %s): the batch lands it whatever is written on it now, so nothing was changed. "+
		"Do it once that batch has ended, or its mark on %s has lapsed",
		c.batchWords(rig), c.Until.UTC().Format("15:04Z"), id)
}
