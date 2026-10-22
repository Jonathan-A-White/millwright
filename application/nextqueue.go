package application

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// QueuedForBatch reports whether a story is queued for batch landing
// (RunQueued), as the run label on the story says: a story whose close-out
// passed on a rig that lands in batches, its claim and worktree kept and its
// session gone by design. Read off the listing, so that whatever walks the
// claimed stories can pass it over without asking the tracker again.
func QueuedForBatch(detail StoryDetail) bool {
	return hasLabel(detail.Labels, RunState+":"+RunQueued)
}

// readLanding reads how the story's rig lands its stories, before its checks:
// on a rig that lands in batches, the commit at the branch's tip, and whether
// mw check passed at that very commit, which the checks then trust in place of
// running the rig's tests again (plans/0117, his answer to Q2). A rig that
// lands each by itself, one whose file says nothing of it, and one whose file
// cannot be read are left exactly as they were: the landing reads the file
// again, and says what is wrong with it there.
func (n Next) readLanding(ctx context.Context, c *closeOut, report *NextReport) {
	if n.Rules == nil {
		return
	}
	rules, _, err := n.landingRequirements(ctx, c.path.Rig)
	if err != nil {
		c.landingReadErr = fmt.Errorf("the read of %s/%s.toml before the rig's tests failed: %w", RigsDir, c.path.Rig, err)
		return
	}
	for _, warning := range rules.Warnings {
		report.Notes = append(report.Notes, fmt.Sprintf("%s/%s.toml: %s", RigsDir, c.path.Rig, warning))
	}
	if rules.LandingSettings().Mode != domain.LandingBatch {
		return
	}
	n.markBatch(ctx, c)
}

// markBatch records that the story is to be queued: its branch's tip, and the
// commit mw check passed at, which the checks trust when it is that very tip.
func (n Next) markBatch(ctx context.Context, c *closeOut) {
	c.batch = true
	tip, err := n.Landing.RefHead(ctx, c.rigDir, c.branch)
	if err != nil {
		// The queue reads it again, and stops there if it still cannot.
		return
	}
	c.tip = tip
	log, err := n.Vault.ReadRunFile(ctx, c.id, CheckLogFileName(c.attempt()))
	if err != nil {
		return
	}
	if CheckedTip(log) == tip {
		c.trusted = tip
	}
}

// rereadLanding settles, once the checks have passed, whether the rig lands in
// batches, for a close-out whose first read, before them, said it lands each or
// could not be made. The checks take minutes, and the vault is brought level
// by the hosts meanwhile, so a rig flipped to batch while its story ran is
// read as the batch rig it is now and queued, not stopped by the merge's guard
// (mw-gq6.697). A read that fails again is left to the guard in merge, which
// names it.
func (n Next) rereadLanding(ctx context.Context, c *closeOut, report *NextReport) {
	if n.Rules == nil || c.batch {
		return
	}
	rules, _, err := n.landingRequirements(ctx, c.path.Rig)
	if err != nil {
		c.landingReadErr = fmt.Errorf("the read of %s/%s.toml after the rig's tests failed: %w", RigsDir, c.path.Rig, err)
		return
	}
	c.landingReadErr = nil
	if rules.LandingSettings().Mode != domain.LandingBatch {
		return
	}
	report.Notes = append(report.Notes, fmt.Sprintf("%s lands in batches now: %s/%s.toml said otherwise when this close-out began, and was read again after the rig's tests, so the story is queued",
		c.path.Rig, RigsDir, c.path.Rig))
	n.markBatch(ctx, c)
}

// queue is what a close-out that passed does on a rig that lands in batches,
// in place of the merge: the story's branch pushed to the remote as itself,
// mw/<id>, never forced; run=queued recorded; one line in the seat's ledger
// saying so and which run of the rig's tests it trusted; the vault committed;
// and the baton carried on. The story stays open and claimed, its worktree is
// kept, and the target branch is not touched: its batch lands it (plans/0117,
// Land Rush 3). A push that fails stops the close-out as a landing that failed
// does, and nothing is recorded queued.
func (n Next) queue(ctx context.Context, c *closeOut, report *NextReport) (NextReport, error) {
	if c.tip == "" {
		tip, err := n.Landing.RefHead(ctx, c.rigDir, c.branch)
		if err != nil {
			return n.stop(ctx, c, report, ReasonGitFailed, fmt.Sprintf("the tip of %s could not be read to queue it: %v", c.branch, err), "")
		}
		c.tip = tip
	}
	// What is pushed is the worktree's HEAD: it must be the commit the checks
	// passed, or nothing is pushed.
	if head, err := n.Landing.RefHead(ctx, c.worktree, "HEAD"); err != nil || head != c.tip {
		why := fmt.Sprintf("the worktree %s is not at the tip of %s (%s), so it was not queued", c.worktree, c.branch, shortCommit(c.tip))
		if err != nil {
			why = fmt.Sprintf("the commit the worktree %s is at could not be read, so it was not queued: %v", c.worktree, err)
		}
		return n.stop(ctx, c, report, ReasonGitFailed, why, "")
	}
	if err := n.pushRetrying(ctx, report, c.worktree, c.branch); err != nil {
		if Rejected(err) {
			err = failedFor(ReasonLandingFailed, fmt.Errorf("%s at %s holds commits this branch does not (rebased since it was queued?), and mw forces nothing: %w",
				c.branch, n.remote(), err))
		}
		kept := n.keepLandingError(ctx, c, report, err)
		return n.stop(ctx, c, report, reasonOf(err), firstLine(err.Error()), kept)
	}
	report.Queued, report.QueuedAs, report.Trusted = true, c.branch+" at "+n.remote(), c.trusted

	tested := "mw next re-ran the rig's tests: pass"
	if c.trusted != "" {
		tested = fmt.Sprintf("tested by mw check at %s: pass", shortCommit(c.trusted))
	}
	outcome := fmt.Sprintf("queued for batch landing as %s at %s (%s), %d commits, %s",
		c.branch, n.remote(), shortCommit(c.tip), report.Commits, tested)

	// Written first, as a landing's run=landed is: it is what tells the
	// dispatch, the sweep and a later close-out that the story waits for its
	// batch rather than for a session.
	n.writeOrSay(ctx, c, report, "the run state", fmt.Sprintf("%s could not be recorded as %s=%s", c.id, RunState, RunQueued),
		func() error { return n.Tracker.SetStoryState(ctx, c.id, RunState, RunQueued, outcome) })
	// When it was queued, which mw land merges the queue in the order of.
	n.writeOrSay(ctx, c, report, "the time it was queued", fmt.Sprintf("%s could not be recorded as queued at a time", c.id),
		func() error {
			return n.Tracker.SetStoryMetadata(ctx, c.id, map[string]string{QueuedAtField: n.now().UTC().Format(time.RFC3339)})
		})
	if err := n.ledger(ctx, c, report, outcome); err != nil {
		report.Notes = append(report.Notes, err.Error())
	}
	n.commit(ctx, c, report)

	report.Abandoned = n.abandoned(ctx, c.id, report)
	return n.carryOn(ctx, c, report)
}
