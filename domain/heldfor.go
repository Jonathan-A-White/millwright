package domain

import (
	"sort"
	"strings"
)

// HeldForPrefix starts the label that says a bead is held behind an event: a
// bead labelled held-for-<event> waits for that event to end, and whoever ends
// it is to release the bead. <event> is the id of the epic or demo whose close
// ends the event.
const HeldForPrefix = "held-for-"

// HeldFor is the beads still open behind one held-for label.
type HeldFor struct {
	// Label is the whole label, as the beads carry it.
	Label string
	// Beads are the ids of the beads carrying it that are not closed, in id order.
	Beads []string
}

// HeldForLabels are the held-for labels that some bead not yet closed carries,
// each with those beads, in label order. A label whose beads are all closed is
// not listed: nothing is held behind it.
func HeldForLabels(graph []GraphBead) []HeldFor {
	byLabel := map[string][]string{}
	for _, b := range graph {
		if strings.EqualFold(b.Status, "closed") {
			continue
		}
		seen := map[string]bool{}
		for _, label := range b.Labels {
			label = strings.ToLower(label)
			if !strings.HasPrefix(label, HeldForPrefix) || label == HeldForPrefix || seen[label] {
				continue
			}
			seen[label] = true
			byLabel[label] = append(byLabel[label], b.ID)
		}
	}
	out := make([]HeldFor, 0, len(byLabel))
	for label, ids := range byLabel {
		sort.Strings(ids)
		out = append(out, HeldFor{Label: label, Beads: ids})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Label < out[b].Label })
	return out
}

// HeldForEvent are the ids of the beads not yet closed that are held for the
// event: those labelled held-for-<event>, in id order. The event is the id of
// the epic or demo that ends it.
func HeldForEvent(graph []GraphBead, event string) []string {
	want := HeldForPrefix + strings.ToLower(strings.TrimSpace(event))
	for _, held := range HeldForLabels(graph) {
		if held.Label == want {
			return held.Beads
		}
	}
	return nil
}

// HeldForLabel is the first held-for-<event> label among labels, lower-cased, or
// "" when none is there: the label that says a bead waits for an event to end
// and is not to be worked until whoever ends it releases the bead.
func HeldForLabel(labels []string) string {
	for _, label := range labels {
		label = strings.ToLower(strings.TrimSpace(label))
		if strings.HasPrefix(label, HeldForPrefix) && label != HeldForPrefix {
			return label
		}
	}
	return ""
}
