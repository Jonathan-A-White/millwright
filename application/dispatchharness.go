package application

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// retryHarnessStop is a dispatch's own mw retry of a story the harness stopped
// (mw-gq6.434): one its close-out refused (run=blocked) whose last attempt's
// result is a usage limit, a rate limit, or an overloaded or failing API. Until
// the time the stop names, the story stays held and the host claims nothing
// new (holdForHarness). Once it has come, the claim is given back and the
// stopped attempt taken off the count, so the story is read fresh among what is
// ready and started again as the same attempt. The worktree and branch the
// attempt left are kept by the fresh cut, as for any earlier attempt
// (keepLeftover). It reports whether the claim was given back; a story whose
// last attempt is anything else is left exactly as it was, held as refused.
func (d Dispatch) retryHarnessStop(ctx context.Context, detail StoryDetail, name string, dead bool, report *DispatchReport) bool {
	stop, stopped := harnessStopOfAttempt(ctx, d.Boot.Vault, detail, d.now())
	if !stopped {
		return false
	}
	if stop.Until.After(d.now()) && !d.loginHoldCleared(ctx, stop) {
		d.holdForHarness(ctx, stop, report, detail.Story.ID)
		return false
	}

	id := detail.Story.ID
	if err := d.Tracker.ReleaseClaimHeldBy(ctx, id, detail.Assignee); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: the harness stopped it (%s) and the time it named has come, but its claim could not be given back, so it is held: %v", id, stop.Reason, err))
		return false
	}
	report.HarnessRetried = append(report.HarnessRetried, id)

	// Given back first, then the count lowered: a count that cannot be lowered
	// leaves the attempt counted, as it was before this existed, never a claim
	// held on a count that no longer names the stopped attempt's result.
	lowered := detail.Attempts - 1
	count := fmt.Sprintf("the attempt is not counted (%s %d -> %d), so it is dispatched again as attempt %d",
		AttemptsField, detail.Attempts, lowered, detail.Attempts)
	if err := d.Tracker.SetStoryMetadata(ctx, id, map[string]string{AttemptsField: strconv.Itoa(lowered)}); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: its claim was given back, but its harness-stopped attempt could not be taken off the count (%s=%d): %v", id, AttemptsField, lowered, err))
		count = fmt.Sprintf("its attempt count of %d is unchanged, as the stopped attempt could not be taken off it", detail.Attempts)
	}
	if dead {
		if err := d.Runner.Close(ctx, name); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: its claim was given back, but the window of %s could not be closed: %v", id, name, err))
		}
	}
	why := fmt.Sprintf(
		"mw dispatch on %s gave the claim on %s back itself: attempt %d was stopped by the harness (%s: %s), not by its work, "+
			"and the time it named, %s, has come. The attempt is not counted: %s.",
		d.Host, id, detail.Attempts, stop.Reason, stop.Said, stop.Until.UTC().Format(time.RFC3339), count)
	if err := d.Tracker.CommentOnStory(ctx, id, why); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the give-back after its harness stop could not be commented on: %v", id, err))
	}
	return true
}

// holdForHarness holds this host's new claims until the stop's time, the
// latest of every stop a pass finds, and keeps it in the host's note so that a
// story given back by hand before then does not lift it (mw-gq6.434).
func (d Dispatch) holdForHarness(ctx context.Context, stop HarnessStop, report *DispatchReport, story string) {
	if report.HarnessHold == nil || stop.Until.After(report.HarnessHold.Until) {
		hold := stop
		report.HarnessHold = &hold
	}
	if d.DryRun {
		return
	}
	if err := keepHarnessHold(ctx, d.Memory, d.Host, stop); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"the hold on new claims until %s could not be kept in the host's note: %v", stop.Until.UTC().Format(time.RFC3339), err))
		return
	}
	said, _ := holdHostForLogin(ctx, d.Memory, d.Mailbox, d.Host, story, stop, d.now())
	report.Notes = append(report.Notes, said...)
}

// loginHoldCleared says the hold an expired login named has been cleared from
// the host's note before its time, which is how the Governor says the login is
// done: the stopped story is then given back at once, though the time it named
// has not come. It needs the host's note to read, and a mw next that kept the
// hold (holdHostForLogin) or this dispatch's own pass before it: with no note
// to read it says nothing.
func (d Dispatch) loginHoldCleared(ctx context.Context, stop HarnessStop) bool {
	if stop.Reason != ReasonLoginExpired || d.Memory == nil {
		return false
	}
	_, held := readHarnessHold(ctx, d.Memory, d.Host)
	return !held
}

// readHarnessHoldNote takes the hold kept in the host's note into the pass —
// one a hand mw retry, or an earlier pass, left — and clears a note whose time
// has passed.
func (d Dispatch) readHarnessHoldNote(ctx context.Context, report *DispatchReport) {
	hold, held := readHarnessHold(ctx, d.Memory, d.Host)
	if !held {
		return
	}
	if hold.Until.After(d.now()) {
		if report.HarnessHold == nil || hold.Until.After(report.HarnessHold.Until) {
			report.HarnessHold = &hold
		}
		return
	}
	if d.DryRun {
		return
	}
	if err := d.Memory.ClearNote(ctx, HarnessHoldKey(d.Host)); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the passed hold on new claims could not be cleared from the host's note: %v", err))
	}
}

// harnessHeld is why a ready story is passed over while the host is held.
func (h HarnessHold) harnessHeld(host string) string {
	return fmt.Sprintf("%s claims nothing new until %s: the harness stopped a session (%s: %s)",
		host, h.Until.UTC().Format(time.RFC3339), h.Reason, h.Said)
}
