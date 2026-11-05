package application

import (
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// NoSessionClaimGrace is how long past its lease a claim no host names must be
// before it is counted a claim with no session. A dispatch writes the host over
// "auto" within a few bd calls of claiming, well inside ClaimLease; the grace
// keeps one that is slow, not dead, from being taken back under its feet.
const NoSessionClaimGrace = 10 * time.Minute

// NoSessionClaim reports whether a story is a claim nothing is working and no
// host's dispatch looks at (mw-gq6.877): in progress, held by a dispatch (an
// assignee mw@<host>), its lease run out by more than NoSessionClaimGrace with
// no heartbeat since, and its Path naming no host (empty, or domain.HostAuto)
// so that no session is named for it. A live lease, a named host, a claim that
// is not a dispatch's, and a story that holds no session by design (queued for
// its batch, handed back for its close-out alone, the Governor's own) are not.
//
// The story mw-5r3p30.940 was claimed by the desktop's dispatch, which died
// before it recorded the host: pathed to "auto" it is on no host's list of what
// is running, and sat so for seven hours. A claim whose Path does name a host is
// that host's to give back (Dispatch.reclaimDeadPane).
func NoSessionClaim(detail StoryDetail, now time.Time) bool {
	return noSessionClaim(detail, now, NoSessionClaimGrace)
}

// noSessionClaim is NoSessionClaim with the grace given.
func noSessionClaim(detail StoryDetail, now time.Time, grace time.Duration) bool {
	if !strings.EqualFold(strings.TrimSpace(detail.Status), StatusInProgress) || ClaimHost(detail.Assignee) == "" {
		return false
	}
	if host := detail.Merged().Host; host != "" && host != domain.HostAuto {
		return false
	}
	if detail.Hitl() || detail.Todo() || QueuedForBatch(detail) || CloseOutAgain(detail) {
		return false
	}
	return !detail.LeaseExpires.IsZero() && now.After(detail.LeaseExpires.Add(grace))
}

// NoSessionClaims are the claims in w that NoSessionClaim says nothing is
// working, in the order they were read.
func (w WorkInHand) NoSessionClaims(now time.Time) []StoryDetail {
	var stuck []StoryDetail
	for _, detail := range w.Running {
		if NoSessionClaim(detail, now) {
			stuck = append(stuck, detail)
		}
	}
	return stuck
}

// NoSessionClaimOf is NoSessionClaim as the dispatch or doctor of host judges
// it (mw-gq6.998): a claim held by host itself needs no grace past its lease.
// The grace keeps a claim another host's dispatch is still starting from being
// taken back; a claim of this host's own is being started by nobody once this
// host's dispatch (which holds the host's lock) or doctor is looking at it, so
// its lapsed lease is all that says nothing works it.
//
// mw-hmnm2xi.2 was claimed by mw@cloud2 as the close-out of its predecessor
// chained a dispatch (Next.carryOn), and never launched; pathed to "auto", it
// waited out the lease and the grace both, and cloud1's dispatch gave it back
// twenty minutes after the claim, with cloud2's cap free all that time.
func NoSessionClaimOf(detail StoryDetail, now time.Time, host string) bool {
	grace := NoSessionClaimGrace
	if host != "" && ClaimHost(detail.Assignee) == host {
		grace = 0
	}
	return noSessionClaim(detail, now, grace)
}

// OwnNoSessionClaim says whether the claim is one host holds itself: the claim
// NoSessionClaimOf gives no grace.
func OwnNoSessionClaim(detail StoryDetail, host string) bool {
	return host != "" && ClaimHost(detail.Assignee) == host
}
