package application

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// shareEpsilon keeps a share the arithmetic lands on exactly (0.6 of 5) from
// being read as a hair under it.
const shareEpsilon = 1e-9

// shareEligible says a story is one the share counts and orders: below P1, and
// one a dispatch could start. A P0 or P1 story is the express lane, always
// first and never counted; a parent, a story the Governor must be present for
// and one parked behind an event are passed over by dispatch, so they neither
// take a share slot nor are moved.
func shareEligible(d StoryDetail) bool {
	return d.Priority >= 2 && !d.IsParent() && !d.Hitl() && domain.HeldForLabel(d.Labels) == ""
}

// inFeatureLane says a story is in the feature lane: a feature by StoryKind, or
// a story a feature waits on (waited, from featureWaits).
func inFeatureLane(d StoryDetail, waited map[string]bool) bool {
	return IsFeatureKind(StoryKind(d)) || waited[d.Story.ID]
}

// featureWaits is the ids of the stories a feature waits on, directly or through
// other stories it waits on: the stories among blocked (open stories waiting on
// something unfinished, with their Needs narrowed to what they still wait for)
// that are features seed it, and each need of a story in it joins it. These are
// the same links BeadGraph carries, read from the listing the caller already has.
func featureWaits(blocked []StoryDetail) map[string]bool {
	waited := map[string]bool{}
	byID := make(map[string]StoryDetail, len(blocked))
	var pending []StoryDetail
	for _, d := range blocked {
		byID[d.Story.ID] = d
		if IsFeatureKind(StoryKind(d)) {
			pending = append(pending, d)
		}
	}
	for len(pending) > 0 {
		d := pending[0]
		pending = pending[1:]
		for _, need := range d.Needs {
			if waited[need] {
				continue
			}
			waited[need] = true
			if next, isBlocked := byID[need]; isBlocked {
				pending = append(pending, next)
			}
		}
	}
	return waited
}

// shareWords is the reason a story was moved ahead of a fix: the state of its
// rig's lane at the moment it was.
func shareWords(features, running int, share float64) string {
	return fmt.Sprintf("feature share: %d of %d running, target %d%%", features, running, int(math.Round(share*100)))
}

// inShareOrder is sorted (the order inStartOrder gave) with each rig's share
// kept, and why each story it moved sits where it does, by id. P0 and P1 stay
// where they are. For each rig whose file has a feature_share, it counts the
// feature-lane stories among the rig's running stories below P1 on every host;
// while that count is under share x (running + 1) and a feature-lane story is
// ready, the first one is placed before the next fix, as if each story placed
// were started. A feature is never moved behind a fix, so with the share met the
// order is today's. The stories of a rig keep to the places they held, so the
// rig's order against other rigs and against P0 and P1 is as it was.
//
// It is an order, not a gate: nothing is held back, and a feature that dispatch
// passes over for a file another story has is simply not started, the loop going
// on to the fix after it. A rig file without the key, no rules, or rules that
// cannot be read give today's order; a rig whose file cannot be read is named in
// the notes, one line a rig, so the share is not turned off without a word.
// waited is read at most once, and only when some rig has a share.
func inShareOrder(ctx context.Context, sorted, running []StoryDetail, rules EpicRules, waited func() map[string]bool) ([]StoryDetail, map[string]string, []string) {
	if rules == nil {
		return sorted, nil, nil
	}
	places := map[string][]int{}
	var rigs []string
	for i, d := range sorted {
		if !shareEligible(d) {
			continue
		}
		rig := d.Merged().Rig
		if _, seen := places[rig]; !seen {
			rigs = append(rigs, rig)
		}
		places[rig] = append(places[rig], i)
	}
	var lane map[string]bool
	var ordered []StoryDetail
	var why map[string]string
	var notes []string
	for _, rig := range rigs {
		required, err := rules.EpicRequirements(ctx, rig)
		if err != nil {
			notes = append(notes, fmt.Sprintf("the rig %s's feature share could not be read, so its stories keep today's order: %v", rig, err))
			continue
		}
		if required.FeatureShare <= 0 {
			continue
		}
		if ordered == nil {
			ordered = append([]StoryDetail(nil), sorted...)
			why = map[string]string{}
			if waited != nil {
				lane = waited()
			}
		}
		features, live := 0, 0
		seen := map[string]bool{}
		for _, r := range running {
			if r.Merged().Rig != rig || r.Priority < 2 || r.Hitl() || seen[r.Story.ID] {
				continue
			}
			seen[r.Story.ID] = true
			live++
			if inFeatureLane(r, lane) {
				features++
			}
		}
		slots := places[rig]
		queue := make([]StoryDetail, len(slots))
		for k, at := range slots {
			queue[k] = sorted[at]
		}
		used := make([]bool, len(queue))
		head := 0
		for k := range slots {
			for used[head] {
				head++
			}
			pick := head
			if !inFeatureLane(queue[head], lane) && float64(features)+shareEpsilon < required.FeatureShare*float64(live+1) {
				for j := head + 1; j < len(queue); j++ {
					if !used[j] && inFeatureLane(queue[j], lane) {
						pick = j
						why[queue[j].Story.ID] = shareWords(features, live, required.FeatureShare)
						break
					}
				}
			}
			used[pick] = true
			if inFeatureLane(queue[pick], lane) {
				features++
			}
			live++
			ordered[slots[k]] = queue[pick]
		}
	}
	if ordered == nil {
		return sorted, nil, notes
	}
	return ordered, why, notes
}

// shareOrder is the order a dispatch on this host starts ready in: the order
// inShareOrder keeps, given what is running on every host. What a feature waits
// on is read from the tracker's blocked stories, once, and only when a rig has
// a share; a tracker that cannot say leaves it to the features themselves.
func (d Dispatch) shareOrder(ctx context.Context, ready, running []StoryDetail) ([]StoryDetail, []string) {
	ordered, _, notes := inShareOrder(ctx, ready, running, d.Rules, func() map[string]bool {
		blocked, err := d.Tracker.BlockedForHost(ctx, d.Host)
		if err != nil {
			return nil
		}
		return featureWaits(blocked)
	})
	return ordered, notes
}

// inShareOrderOfView is ordered with each rig's share kept, from the beads the
// Postern view holds: the stories claimed and not finished are what is running
// on every host, and the beads that wait on something unfinished say what a
// feature waits on.
func inShareOrderOfView(ctx context.Context, b *viewBuild, rules EpicRules, ordered []StoryDetail) []StoryDetail {
	var running []StoryDetail
	for _, id := range b.order {
		e := b.entries[id]
		d := e.detail
		if e.verifyOnly || d.IsEpic || d.IsParent() || d.Assignee == "" || d.Hitl() ||
			!strings.EqualFold(strings.TrimSpace(d.Status), StatusInProgress) {
			continue
		}
		running = append(running, d)
	}
	out, _, _ := inShareOrder(ctx, ordered, running, rules, func() map[string]bool {
		var blocked []StoryDetail
		for _, id := range b.order {
			if e := b.entries[id]; !e.verifyOnly && len(e.waits) > 0 {
				d := e.detail
				d.Needs = e.waits
				blocked = append(blocked, d)
			}
		}
		return featureWaits(blocked)
	})
	return out
}
