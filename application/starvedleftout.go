package application

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// notCurrentWhy says why a host's note of a rig is not a hold now, in the alarm's
// words, or "" when it is current (StarvedRig.current): a story of the rig landed
// since it was first held, or the host has made no good dispatch run for
// StarvedPassStale, or it gives way (mw-gq6.1660).
func (r StarvedRig) notCurrentWhy(now time.Time) string {
	switch {
	case r.GivesWay:
		return r.Host + " gives way"
	case !r.current(now):
		if !r.Landed.IsZero() && !r.Landed.Before(r.Since) {
			return fmt.Sprintf("%s: a story of it landed since it was held", r.Host)
		}
		if r.PassAt.IsZero() {
			return r.Host + ": no good dispatch run"
		}
		return fmt.Sprintf("%s: no good dispatch run for %s", r.Host, elapsed(now.Sub(r.PassAt)))
	}
	return ""
}

// leftOutWords is the sentence an alarm carries for the hosts whose notes of its
// rig were passed over as not current: the alarm names the holds of the other
// hosts, and without it reads as if they were the whole of what held the rig. ""
// when none was left out.
func leftOutWords(leftOut []string) string {
	if len(leftOut) == 0 {
		return ""
	}
	return "left out as not current: " + strings.Join(slices.Compact(slices.Sorted(slices.Values(leftOut))), "; ")
}
