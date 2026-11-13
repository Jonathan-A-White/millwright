package application

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// ReasonHarnessCrash is a session whose harness crashed under it
// (mw-gq6.1137): Claude Code runs on Bun, and Bun segfaulted on cloud boxes, at
// the session's start and in the middle of its work. It left an empty result and
// a pane ending in Bun's crash report. The crash says nothing of the story, so
// the first one on a story is named tooling, the work the worktree held is
// kept, and the claim is given back for the next dispatch tick, nobody asked; a
// second crash on the same story stops as any session that left no result does
// (ReasonNoResult), for the Mayor.
const ReasonHarnessCrash Reason = "harness-crash"

// HarnessCrashFileName is the run file mw next keeps a harness crash in: the
// banner Bun printed. It is what says, to the next attempt's close-out, that
// the story has been given back for a crash once already.
const HarnessCrashFileName = "harness-crash.txt"

// HarnessCrashFileNameForAttempt is HarnessCrashFileName for one attempt, named
// as the result it stands beside.
func HarnessCrashFileNameForAttempt(attempt int) string {
	return attemptRunFileName(HarnessCrashFileName, attempt)
}

// harnessCrashBanner is the line of a pane's tail that names the Bun runtime
// that crashed ("Bun v1.4.4 (5f3a1c0b) Linux x64"), when the pane shows the
// harness crashing (isHarnessCrash, the reader mw status counts crashes with).
func harnessCrashBanner(tail string) (string, bool) {
	if !isHarnessCrash(tail) {
		return "", false
	}
	for _, line := range strings.Split(tail, "\n") {
		if line = strings.TrimSpace(line); strings.Contains(line, harnessCrashBannerPrefix) {
			return line, true
		}
	}
	return "", false
}

// crashedBefore reports whether an earlier attempt of the story was given back
// for a harness crash already: its crash file is in the story's run.
func (n Next) crashedBefore(ctx context.Context, c *closeOut) bool {
	for attempt := 1; attempt < c.attempt(); attempt++ {
		if _, err := n.Vault.ReadRunFile(ctx, c.id, HarnessCrashFileNameForAttempt(attempt)); err == nil {
			return true
		}
	}
	return false
}

// harnessCrashed is what a close-out does with a session that left no result
// and whose pane shows the harness crashing (mw-gq6.1137), in place of stopping
// it as no-result with the claim kept. The attempt is named tooling (mw cause),
// so that it is not counted against the story; what the worktree holds is
// committed and bundled into the vault, so that a box that is destroyed
// afterwards takes nothing with it; and the claim is given back, the next
// attempt continuing the branch. It is done once per story: a story that was
// given back for a crash before, or whose work or crash record could not be
// kept, is not handled here, and the close-out stops it as it always has, the
// reason in c.crashSaid. handled is whether it was stopped here.
func (n Next) harnessCrashed(ctx context.Context, c *closeOut, report *NextReport) (rep NextReport, handled bool, err error) {
	if ctx.Err() != nil {
		return NextReport{}, false, nil
	}
	tail, lookErr := n.paneTail(ctx, c)
	if lookErr != nil {
		return NextReport{}, false, nil
	}
	banner, crashed := harnessCrashBanner(tail)
	if !crashed {
		return NextReport{}, false, nil
	}
	if n.crashedBefore(ctx, c) {
		c.crashSaid = "The harness crashed under an earlier attempt of this story too, which was given back once for it already, so this one is not: " + banner
		return NextReport{}, false, nil
	}

	kept, err := n.keepCrashedWork(ctx, c, report)
	if err != nil {
		c.crashSaid = fmt.Sprintf("The harness crashed (%s), but the worktree's work could not be kept in a bundle, so the claim is not given back: %v", banner, err)
		return NextReport{}, false, nil
	}
	if _, err := n.Vault.PutRunFile(ctx, c.id, HarnessCrashFileNameForAttempt(c.attempt()), banner+"\n"); err != nil {
		c.crashSaid = fmt.Sprintf("The harness crashed (%s), but that could not be kept in the run of %s, so the claim is not given back: %v", banner, c.id, err)
		return NextReport{}, false, nil
	}

	c.crashFile = HarnessCrashFileNameForAttempt(c.attempt())
	why := fmt.Sprintf("the harness crashed under the session, not its work (%s): the claim is given back once and the story dispatched again", banner)
	said := n.nameToolingCause(ctx, c, report, why) + " " + kept + "\n\nThe last lines of the session's pane:\n\n```\n" + tail + "\n```"
	rep, err = n.stop(ctx, c, report, ReasonHarnessCrash, why, said)
	return rep, true, err
}

// keepCrashedWork commits what the crashed session's worktree holds uncommitted
// onto its branch and bundles the branch's commits ahead of the target into the
// story's run in the vault, committed and pushed there when the vault is a clone
// (the way mw retry keeps an attempt, salvageBranch). The worktree and the
// branch stay where they are for the next dispatch tick to keep. It returns what
// it did in a sentence; an error is work not known to be safe.
func (n Next) keepCrashedWork(ctx context.Context, c *closeOut, report *NextReport) (string, error) {
	hasDir, err := n.Worktrees.Exists(ctx, c.rigDir, c.worktree, "")
	if err != nil {
		return "", fmt.Errorf("checking whether the worktree %s is there: %w", c.worktree, err)
	}
	hasBranch, err := n.Worktrees.Exists(ctx, c.rigDir, "", c.branch)
	if err != nil {
		return "", fmt.Errorf("checking whether the branch %s is there: %w", c.branch, err)
	}
	if !hasDir && !hasBranch {
		return "The session left no worktree or branch, so there was no work to keep.", nil
	}
	said := ""
	if hasDir {
		committed, err := commitLeftovers(ctx, n.Landing, c.worktree, c.id, c.attempt())
		if err != nil {
			return "", err
		}
		if committed {
			said = fmt.Sprintf("What the worktree held uncommitted was committed onto %s. ", c.branch)
		}
	}
	if err := n.fetchRetrying(ctx, report, c.rigDir); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the rig could not be brought up to date with %s before the crashed work was bundled: %v", n.remote(), err))
	}
	base := StartPoint(n.remote(), c.target)
	ahead, err := n.Landing.Ahead(ctx, c.rigDir, c.branch, base)
	if err != nil {
		return "", fmt.Errorf("counting the commits %s has ahead of %s: %w", c.branch, base, err)
	}
	if ahead == 0 {
		return said + fmt.Sprintf("%s has no commits ahead of %s, so there was nothing to bundle.", c.branch, base), nil
	}
	dest := n.Vault.RunFile(c.id, BundleFileName(c.attempt()))
	if _, err := n.Landing.Bundle(ctx, c.rigDir, c.branch, base, dest); err != nil {
		return "", fmt.Errorf("bundling %s into %s: %w", c.branch, dest, err)
	}
	if err := n.Landing.VerifyBundle(ctx, c.rigDir, dest); err != nil {
		return "", fmt.Errorf("the bundle %s does not verify: %w", dest, err)
	}
	rel := BundleRecord(c.id, c.attempt())
	said += fmt.Sprintf("Its %d commit(s) are bundled in the vault as %s", ahead, rel)
	if n.Files == nil {
		return said + ".", nil
	}
	message := fmt.Sprintf("%s: bundle of attempt %d branch %s, kept after the harness crashed", c.id, c.attempt(), c.branch)
	if _, err := commitWritten(ctx, n.Files, message, []string{rel}); err != nil {
		return "", fmt.Errorf("committing the bundle %s in the vault: %w", rel, err)
	}
	if _, err := retryPastASiblingsWrite(ctx, sleepFor, n.Files.Push); err != nil {
		return "", fmt.Errorf("the bundle %s is committed in the vault but could not be pushed: %w", rel, err)
	}
	return said + ".", nil
}

// harnessCrashStopSaid is what a close-out that stops a story as no-result,
// whose pane showed the harness crashing but which was not given back for it,
// says beside the stop (see harnessCrashed).
func harnessCrashStopSaid(c *closeOut, said string) string {
	if c.crashSaid == "" {
		return said
	}
	if said == "" {
		return c.crashSaid
	}
	return c.crashSaid + "\n\n" + said
}

// noResultAt is true for a result path that was never written or holds nothing:
// the two ways a session that died leaves it.
func noResultAt(printed string, readErr error) bool {
	return errors.Is(readErr, fs.ErrNotExist) || strings.TrimSpace(printed) == ""
}
