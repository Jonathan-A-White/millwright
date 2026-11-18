package application

import (
	"context"
	"fmt"
)

// BdLockNotes is what a mw bd leaves for the close-out when a close of formula
// steps ran no bd at all because it could not get this host's bd turn within
// its wait (mw-gq6.1505): the steps are then open, and the Builder did not
// leave them so. Each note is keyed by the id of the step it was a close of and
// says who the close waited for. A later close of the step that goes through
// takes the note away.
type BdLockNotes interface {
	// Lose records that a close of each of ids gave up on the bd lock, held by blocker.
	Lose(ctx context.Context, ids []string, blocker string) error
	// Lost reads the notes of ids: each id with a note, and the blocker in it.
	Lost(ctx context.Context, ids []string) (map[string]string, error)
	// Forget takes the notes of ids away.
	Forget(ctx context.Context, ids []string) error
}

// StepCloseLockTries is how many times the close-out tries to close a step
// whose close a mw bd gave up on for the bd lock before it hands the story
// back for its close-out alone, the waits between growing as a retried push's
// do (retryPause).
const StepCloseLockTries = 3

// noteLockLost leaves the note of a close that waited out its limit for the bd
// lock; a call that is not a close leaves none. A note that cannot be written
// is said on Err: the close-out then reads the open steps as the Builder's.
func (r BdRetry) noteLockLost(ctx context.Context, args []string, busy *BdBusyError) {
	if r.LockNotes == nil {
		return
	}
	call := parseBdCall(args)
	if call.kind() != bdClose || len(call.positional()) == 0 {
		return
	}
	if err := r.LockNotes.Lose(ctx, call.positional(), busy.Blocker); err != nil {
		r.say("mw bd: the close could not be noted as lost to the bd lock, so mw next will read its steps as open: %v\n", err)
	}
}

// noteClosed takes away the notes of a close that went through.
func (r BdRetry) noteClosed(ctx context.Context, args []string) {
	if r.LockNotes == nil {
		return
	}
	if call := parseBdCall(args); call.kind() == bdClose && len(call.positional()) > 0 {
		r.LockNotes.Forget(ctx, call.positional())
	}
}

// stepsLostToTheLock is the close-out's answer to a formula with open steps
// when a mw bd gave up on the bd lock for the close of some of them
// (mw-gq6.1505): those steps were not left open by the Builder. It waits and
// closes them itself, up to StepCloseLockTries tries; what it still cannot close
// is a refusal of ReasonStepCloseLock, which hands the story back for its
// close-out alone and spends no attempt. left is the open steps that remain for
// the open-steps refusal: those with no note, and, with the notes unreadable,
// all of them. A check (previewing) writes nothing: it names the steps and does
// not close them.
func (n Next) stepsLostToTheLock(ctx context.Context, report *NextReport, root string, open []FormulaStep, previewing bool) (left []FormulaStep, refusal Refusal, refused bool) {
	if n.BdLocks == nil {
		return open, Refusal{}, false
	}
	ids := make([]string, len(open))
	for i, step := range open {
		ids[i] = step.ID
	}
	lost, err := n.BdLocks.Lost(ctx, ids)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the notes of closes that gave up on the bd lock could not be read, so the open steps are read as left open: %v", err))
		return open, Refusal{}, false
	}
	var stuck []FormulaStep
	for _, step := range open {
		if _, gaveUp := lost[step.ID]; gaveUp {
			stuck = append(stuck, step)
		} else {
			left = append(left, step)
		}
	}
	if len(stuck) == 0 {
		return open, Refusal{}, false
	}
	var still []FormulaStep
	var last error
	for _, step := range stuck {
		if previewing {
			still = append(still, step)
			continue
		}
		if last = n.closeStepLostToTheLock(ctx, report, root, step); last != nil {
			still = append(still, step)
		}
	}
	if len(still) == 0 {
		report.Notes = append(report.Notes, fmt.Sprintf("%d formula step(s) whose close by the Builder gave up on the bd lock were closed by mw next", len(stuck)))
		return left, Refusal{}, false
	}
	why := fmt.Sprintf("the close of %d formula step(s) of %s could not get the bd lock: the Builder's mw bd waited for %s, and %s",
		len(still), root, lost[still[0].ID], tryingAgain(last, previewing))
	return left, Refusal{ReasonStepCloseLock, why, "The steps whose close could not get the bd lock:\n" + stepList(still)}, true
}

// tryingAgain is the end of the refusal's why: what the close-out's own tries did.
func tryingAgain(last error, previewing bool) string {
	if previewing {
		return "mw next closes them itself when it runs"
	}
	return fmt.Sprintf("so did mw next's own %d tries (%s); the steps were not left open by the Builder, and its close-out runs again", StepCloseLockTries, firstLine(last.Error()))
}

// closeStepLostToTheLock closes one step the Builder's close of which gave up on
// the bd lock, trying StepCloseLockTries times and waiting between the tries.
func (n Next) closeStepLostToTheLock(ctx context.Context, report *NextReport, root string, step FormulaStep) error {
	var err error
	for try := 1; try <= StepCloseLockTries; try++ {
		if try > 1 {
			wait := n.retryPause(try - 1)
			report.Notes = append(report.Notes, retryNote("the close of "+step.ID, err, wait))
			if werr := n.retryWait(ctx, wait); werr != nil {
				return werr
			}
		}
		if err = n.closeFormulaBead(ctx, root, step.ID, "closed by mw next: the Builder's close of it could not get the bd lock"); err == nil {
			n.BdLocks.Forget(ctx, []string{step.ID})
			return nil
		}
	}
	return err
}
