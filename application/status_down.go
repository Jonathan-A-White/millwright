package application

import (
	"strings"
	"time"
)

// openRefusedFromElsewhere is alarms with each refused-open alarm of a claim
// held on a host that does not answer (down, by host) said with the cure that
// works from elsewhere: a retry run on that host cannot be run (mw-gq6.1515).
// An alarm of a story not claimed on a down host is left as it was.
func openRefusedFromElsewhere(alarms []DamperAlarm, work WorkInHand, now time.Time, down map[string]string) []DamperAlarm {
	if len(down) == 0 {
		return alarms
	}
	var changed map[string]DamperAlarm
	for _, o := range OpenRefusedStories(work, now) {
		if !strings.EqualFold(o.Story.Status, StatusInProgress) {
			continue
		}
		if said := down[o.Story.ClaimedHost()]; said != "" {
			if changed == nil {
				changed = map[string]DamperAlarm{}
			}
			changed[DamperOpenRefused+":"+o.Story.Story.ID] = o.AlarmDown(said)
		}
	}
	if changed == nil {
		return alarms
	}
	out := make([]DamperAlarm, len(alarms))
	for i, a := range alarms {
		out[i] = a
		if again, ok := changed[a.Identity]; ok && a.Principle == DamperOpenRefused {
			out[i] = again
		}
	}
	return out
}
