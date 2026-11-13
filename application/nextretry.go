package application

import (
	"context"
	"fmt"
	"time"
)

// pushRetrying pushes what the landing worktree has checked out, and tries
// again after a wait when the push itself failed on a fault at the remote
// worth trying again, up to PushTries times in all, the waits growing as a
// fetch's do (retryPause). It never retries a push the remote refused for a
// stated reason, or the race with the other host — Rejected — which merge's
// own loop answers by fetching and merging afresh; either comes back as it
// was, for merge to tell apart. Every attempt is counted on the report,
// whichever kind of failure eventually stops it, and every retry is one note.
func (n Next) pushRetrying(ctx context.Context, report *NextReport, dir, branch string) error {
	var last error
	for try := 1; try <= n.pushTries(); try++ {
		if try > 1 {
			wait := n.retryPause(try - 1)
			report.Notes = append(report.Notes, retryNote("push", last, wait))
			if err := n.retryWait(ctx, wait); err != nil {
				return failedFor(ReasonLandingFailed, fmt.Errorf("waiting %s to try the push to %s again: %w", wait, branch, err))
			}
		}
		report.Pushes++
		err := n.Landing.Push(ctx, dir, n.remote(), branch)
		if err == nil {
			return nil
		}
		if WorkflowScopeRefused(err) {
			// Never retried: only another host's credential changes it.
			return failedFor(ReasonWorkflowScope, workflowScopeStop{
				why: fmt.Sprintf("GitHub refused the push to %s: it creates or updates workflow files and the credential %s pushes with lacks the workflow scope, so %s cannot make it",
					branch, n.Host, n.Host),
				err: err,
			})
		}
		if !Transient(err) {
			return err
		}
		last = err
	}
	return failedFor(ReasonLandingFailed, fmt.Errorf("the push to %s kept failing on a fault at the remote, %d time(s) running: %w",
		branch, n.pushTries(), last))
}

// FetchRetries is how many times a fetch that failed on a passing fault of the
// network is tried again before the close-out stops (mw-gq6.373).
const FetchRetries = 3

// fetchRetrying brings the rig's view of its origin up to date, and when the
// fetch failed on a passing fault — a name that would not resolve, a
// connection that timed out — tries it again FetchRetries times, a growing
// wait apart and about a minute in all with the default PushWait, one note per
// retry. A fetch that failed for any other reason comes back at once as it was.
func (n Next) fetchRetrying(ctx context.Context, report *NextReport, rigDir string) error {
	_, err := fetchWhilePassing(ctx, FetchRetries+1,
		func() error { return n.Worktrees.Fetch(ctx, rigDir) },
		n.retryPause, n.retryWait,
		func(err error, wait time.Duration) {
			report.Notes = append(report.Notes, retryNote("fetch", err, wait))
		})
	return err
}

// readRetrying runs one read of the work tracker, and when it failed on a
// passing fault of the network — a Dolt server that did not answer in time —
// tries it again ReadRetries times, a growing wait apart and under a minute in
// all with the default PushWait, one note per retry. A read that failed for any
// other reason, "no issue found" among them, comes back at once as it was; so
// does one that fails every time, in the tracker's own words (mw-gq6.374).
func readRetrying[T any](ctx context.Context, n Next, report *NextReport, what string, read func() (T, error)) (T, error) {
	return retrying(ctx, n, report, "read", what, read)
}

// writeRetrying is readRetrying for a write to the tracker or the mailbox
// (mw-gq6.375): a write that met a passing fault is tried again, ReadRetries
// times, and one that fails every time comes back in the tracker's own words.
func writeRetrying(ctx context.Context, n Next, report *NextReport, what string, write func() error) error {
	_, err := retrying(ctx, n, report, "write", what, func() (struct{}, error) { return struct{}{}, write() })
	return err
}

func retrying[T any](ctx context.Context, n Next, report *NextReport, verb, what string, try func() (T, error)) (T, error) {
	return retryWrite(ctx, n.PushWait, n.retryWait, report, verb, what, try)
}

// retryPause is how long to wait before the retry'th try again of a git step
// that met a passing fault: half of PushWait, then PushWait, then half as much
// again, so that three retries wait 60s in all by default.
func (n Next) retryPause(retry int) time.Duration {
	return n.PushWait / 2 * time.Duration(retry)
}

// retryNote is the one line a retry leaves in the close-out.
func retryNote(step string, err error, wait time.Duration) string {
	why := "passing fault"
	if fault, ok := Faulted(err); ok {
		why = fault.Why
	}
	return fmt.Sprintf("%s failed (%s), retried in %s", step, why, wait)
}
