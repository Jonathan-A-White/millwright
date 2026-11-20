package application

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// laneWaits is the ids of the stories a feature waits on, read from the graph's
// open beads: the same set featureWaits finds from a dispatch's blocked
// listing, so DAMPERS, mw queue and dispatch put one story in one lane.
func laneWaits(graph []domain.GraphBead) map[string]bool {
	var open []StoryDetail
	for _, b := range graph {
		if strings.EqualFold(b.Status, StatusClosed) || b.Type == "epic" {
			continue
		}
		open = append(open, StoryDetail{
			Story:    domain.Story{ID: b.ID, Title: b.Title},
			Type:     b.Type,
			Labels:   b.Labels,
			Priority: b.Priority,
			Needs:    blockedOn(b),
		})
	}
	return featureWaits(open)
}

// LaneAlarms are the two balance alarms of the stories ready below P1, a rig at
// a time, in rig name order: a rig with a feature floor and fewer ready
// feature-lane stories than it (the Mayor carves or releases a feature before
// another fix), and a rig with more ready fix-lane stories than fixLimit (zero
// reads DefaultDamperReadyFixesPerRig). A story is counted as the feature share
// counts it (shareEligible, inFeatureLane); a parent, a story for the Governor
// and one parked behind an event are in neither lane.
func LaneAlarms(work WorkInHand, graph []domain.GraphBead, floors map[string]int, fixLimit int) []DamperAlarm {
	fixLimit = DamperLimits{ReadyFixesPerRig: fixLimit}.orDefaults().ReadyFixesPerRig
	waited := laneWaits(graph)
	features := map[string]int{}
	fixes := map[string][]string{}
	for _, d := range work.Ready {
		if !shareEligible(d) {
			continue
		}
		rig := d.Merged().Rig
		if inFeatureLane(d, waited) {
			features[rig]++
		} else {
			fixes[rig] = append(fixes[rig], d.Story.ID)
		}
	}
	var rigs []string
	for rig := range floors {
		rigs = append(rigs, rig)
	}
	for rig := range fixes {
		if _, there := floors[rig]; !there {
			rigs = append(rigs, rig)
		}
	}
	sort.Strings(rigs)
	var alarms []DamperAlarm
	for _, rig := range rigs {
		if floor := floors[rig]; floor > 0 && features[rig] < floor {
			alarms = append(alarms, DamperAlarm{
				Principle: DamperFeatureFloor,
				Text:      fmt.Sprintf("%s: %d features ready, floor %d: carve or release before a fix", rig, features[rig], floor),
			})
		}
		if ids := fixes[rig]; rig != "" && len(ids) > fixLimit {
			sort.Strings(ids)
			alarms = append(alarms, DamperAlarm{
				Principle: DamperFixQueue,
				Text:      fmt.Sprintf("%s: %d fixes ready, limit %d: release none; fold or ice", rig, len(ids), fixLimit),
				Beads:     ids,
			})
		}
	}
	return alarms
}
