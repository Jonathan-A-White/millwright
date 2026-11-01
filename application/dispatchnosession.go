package application

import (
	"context"
	"fmt"
	"time"
)

// reclaimNoSessionClaims gives back the claims NoSessionClaim names, whichever
// host took them (mw-gq6.877): a dispatch that died between claiming a story
// and recording its host left it in progress, pathed to "auto", on no host's
// list of what is running, so no host's reclaimDeadPane ever looked at it.
//
// The lease is the evidence that no session is working it (a live session
// renews it), and ReclaimStory reads the lease itself, so a claim another
// dispatch is starting right now is not taken from it. A session of the story's
// name running on this host, or a run state that says the work is done or
// refused (run=landed, run=blocked), leaves the claim as it is. A story given
// back is open and ready again, and is read as such below in the same pass. A
// dry run reads nothing and writes nothing here. What cannot be read or done is
// a note on the report, and the next tick tries again.
func (d Dispatch) reclaimNoSessionClaims(ctx context.Context, reads *candidateReads, report *DispatchReport) {
	if d.DryRun {
		return
	}
	stale, err := d.Tracker.StaleClaims(ctx, d.now())
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the claims with no session could not be read, so none was given back: %v", err))
		return
	}
	for _, detail := range stale {
		if !NoSessionClaim(detail, d.now()) {
			continue
		}
		id, name := detail.Story.ID, SessionName(detail.Story.ID)
		if status, err := d.Runner.Status(ctx, name); err != nil || status.Running() {
			continue
		}
		run, err := reads.runState(ctx, id)
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: a claim no host names has a lapsed lease, but its run state could not be read, so it was kept: %v", id, err))
			continue
		}
		if run != "" && run != RunRunning {
			continue
		}
		reclaimed, err := d.Tracker.ReclaimStory(ctx, id)
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: a claim no host names has a lapsed lease, but giving it back failed: %v", id, err))
			continue
		}
		if !reclaimed {
			continue
		}
		why := fmt.Sprintf(
			"mw dispatch on %s found %s claimed by %s with no host named in its path and its lease run out at %s with no heartbeat since, "+
				"so no session is working it: the claim was given back so it is dispatched again.",
			d.Host, id, detail.Assignee, detail.LeaseExpires.UTC().Format(time.RFC3339))
		if err := d.Tracker.CommentOnStory(ctx, id, why); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: giving back the claim no host names could not be commented on: %v", id, err))
		}
		report.Reclaimed = append(report.Reclaimed, Reclaimed{StoryID: id, Session: name, LeaseExpired: detail.LeaseExpires})
	}
}
