package application

import (
	"encoding/json"
	"fmt"
	"time"
)

// NeedsYouSplit is the number Postern's Needs you badge shows, split by kind,
// as the view's counts file holds it (mw-gq6.1643). It is printed beside his
// review queue and judges nothing: the damper's limit of
// DefaultDamperReviewQueue is still judged on the review queue alone.
type NeedsYouSplit struct {
	// Total is the badge's number, the needs waiting on him and not folded.
	Total int
	// Cards are the question, decision and review needs; Demos the demo needs;
	// Hands the hands steps; Landings the verify needs, the landings waiting for
	// his look; Other the rest (release, stale, alarm).
	Cards, Demos, Hands, Landings, Other int
	// Todo are the open items on his To do lists, which stay on their own tab's
	// count and are not in Total.
	Todo int
	// Why is set instead of the numbers when the counts file is missing,
	// stale or unreadable.
	Why string
}

// NeedsYouSplitOf reads the counts file's bytes at now. A missing file, a stale
// one or one that cannot be read gives a split with Why and no numbers.
func NeedsYouSplitOf(raw []byte, now time.Time) NeedsYouSplit {
	if raw == nil {
		return NeedsYouSplit{Why: "no count (the view has not written needs-counts.json here)"}
	}
	var counts PosternNeedsCounts
	err := json.Unmarshal(raw, &counts)
	var written time.Time
	if err == nil {
		written, err = time.Parse(time.RFC3339, counts.WrittenAt)
	}
	if err != nil {
		return NeedsYouSplit{Why: "no count (the counts file cannot be read)"}
	}
	if now.Sub(written) > PosternNeedsCountsStale {
		return NeedsYouSplit{Why: fmt.Sprintf("no count (stale: written %s)", viewClock(written))}
	}
	you := counts.You
	split := NeedsYouSplit{
		Total: you.Total, Todo: counts.Todo,
		Cards: you.Kinds[PosternNeedQuestion] + you.Kinds[PosternNeedDecision] + you.Kinds[PosternNeedReview],
		Demos: you.Kinds[PosternNeedDemo], Hands: you.Kinds[PosternNeedHands], Landings: you.Kinds[PosternNeedVerify],
	}
	split.Other = you.Total - split.Cards - split.Demos - split.Hands - split.Landings
	return split
}

// Line is the split on one line: the badge's number, its kinds, and the To do
// items beside it.
func (s NeedsYouSplit) Line() string {
	if s.Why != "" {
		return "his badge: " + s.Why
	}
	return fmt.Sprintf("his badge %d: %d cards, %d demos, %d hands steps, %d landings waiting for his look, %d other; To do items %d (own tab, not in the badge's count)",
		s.Total, s.Cards, s.Demos, s.Hands, s.Landings, s.Other, s.Todo)
}
