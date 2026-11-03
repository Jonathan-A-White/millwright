package application

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// A whole check on a rig whose gate is not split waits for a calm host before the
// rig's tests, as a close-out does (Next.calmed), but no longer than it waits for
// the merge slot and the gate lock together (CheckPartSlotWait, one deadline from
// the start of the check, mw-gq6.915): the 590 seconds a Builder's foreground call
// is given are not to be spent in a wait that ends in the tool's own silence. A
// host still busy at the end of it hands the check back in one line naming the
// load and the command to run again, and a hand-back repeated in one attempt is
// counted once (mw-gq6.819).

// CheckWholeCalmFileName is the name of the record of the waits of a whole check
// (no --part) on a busy host, in the story's run directory, one for the attempt.
func CheckWholeCalmFileName(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-calm.slot", attempt)
}

// calmRefusal is the one line a check ends with when the host was still busy at
// the end of its wait: the load, since when, and the command to run next, which
// is the same check again. The load is the host's, not the check's failure, and a
// check handed back on it again in the same attempt is counted as the one wait it
// is.
func (k Check) calmRefusal(ctx context.Context, c *closeOut, attempt int, reading LoadReading, since time.Time, waited time.Duration, rerun bool) error {
	load := fmt.Sprintf("load %.1f of %d cores", reading.Load, reading.Cores)
	again := fmt.Sprintf("run the same check again with `mw check %s`", c.id)
	if rerun {
		return k.calmRerunRefusal(ctx, c, attempt, load, again, since, waited)
	}
	if k.HandBackAtOnce {
		return errors.New(k.limitReached("the rig's tests", waited, fmt.Sprintf("%s is still at %s, since %s", k.Host, load, since.UTC().Format("15:04:05Z")), "the load"))
	}
	earlier := k.noteWait(ctx, c.id, CheckWholeCalmFileName(attempt), k.Host+" at "+load, since)
	if earlier != "" {
		return fmt.Errorf("the rig's tests did not start: after %s %s is still at %s, since %s, and nothing was run; it was busy when this check was tried at %s as well, which is counted once for this attempt and is not a failure of the check: %s, and if the host is busy again, comment on the story naming the load and hand back",
			gateWaited(waited), k.Host, load, since.UTC().Format("15:04:05Z"), earlier, again)
	}
	return fmt.Errorf("the rig's tests did not start: after %s %s is still at %s, since %s, and nothing was run; that is the host's load and not a failure of the check: %s",
		gateWaited(waited), k.Host, load, since.UTC().Format("15:04:05Z"), again)
}

// CheckWholeCalmAgainFileName is the name of the record of the waits of a whole
// check for a calm host before the run again of tests that failed on a busy host,
// in the story's run directory, one for the attempt (mw-gq6.918).
func CheckWholeCalmAgainFileName(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-calm-again.slot", attempt)
}

// calmRerunRefusal is calmRefusal for the second wait: the rig's tests were run,
// failed while the host was far over its cores, and the run again that such a
// failure gets would have started on a busy host or past the check's deadline.
// The first failure is the host's and is not counted; nothing of it is kept,
// because the run again is what the next call makes first.
func (k Check) calmRerunRefusal(ctx context.Context, c *closeOut, attempt int, load, again string, since time.Time, waited time.Duration) error {
	if k.HandBackAtOnce {
		return errors.New(k.limitReached("the run again of the rig's tests, which failed once while "+k.Host+" was busy,", waited, fmt.Sprintf("%s is still at %s, since %s", k.Host, load, since.UTC().Format("15:04:05Z")), "the load"))
	}
	earlier := k.noteWait(ctx, c.id, CheckWholeCalmAgainFileName(attempt), k.Host+" at "+load, since)
	if earlier != "" {
		return fmt.Errorf("the rig's tests failed once while %s was busy, and the run again that a failure on a busy host gets did not start: after %s %s is still at %s, since %s; it was busy when this check was tried at %s as well, which is counted once for this attempt and is not a failure of the check: %s, and if the host is busy again, comment on the story naming the load and hand back",
			k.Host, gateWaited(waited), k.Host, load, since.UTC().Format("15:04:05Z"), earlier, again)
	}
	return fmt.Errorf("the rig's tests failed once while %s was busy, and the run again that a failure on a busy host gets did not start: after %s %s is still at %s, since %s; the first failure is not counted and nothing has been run since, and that is the host's load and not a failure of the check: %s",
		k.Host, gateWaited(waited), k.Host, load, since.UTC().Format("15:04:05Z"), again)
}
