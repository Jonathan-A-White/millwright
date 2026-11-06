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
	if line := k.calmKept(fmt.Sprintf("%s is still at %s, since %s", k.Host, load, since.UTC().Format("15:04:05Z"))); line != "" {
		return errors.New(line)
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
	if line := k.calmKept(fmt.Sprintf("%s is still at %s, since %s, and the run again of the rig's tests, which failed once while it was busy, did not start", k.Host, load, since.UTC().Format("15:04:05Z"))); line != "" {
		return errors.New(line)
	}
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

// calmKept is the line a foreground check (Check.KeepPlace) ends with when the
// host was still busy at the end of its wait: no queue stands behind a busy host,
// so no place is kept, and the same command run again waits for it once more
// (mw-gq6.939). Empty when places are not kept, and then the check ends as it
// always has.
func (k Check) calmKept(still string) string {
	if k.KeepPlace <= 0 {
		return ""
	}
	line := still + "; no place is kept for a busy host: run the same command again to go on waiting"
	k.waiting.set(line)
	return line
}

// CheckPartCalmFileName is the name of the record of the waits of part n of a
// story's attempt for a calm host, in the story's run directory, one file for the
// part and the attempt (mw-gq6.987).
func CheckPartCalmFileName(attempt, n int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-part-%d-calm.slot", attempt, n)
}

// calmForPart is the wait mw check --part N makes for a calm host before the part
// starts, as a whole check's is (mw-gq6.987): Next.calmed with its hand-back set,
// no longer than what is left of the check's one deadline (left), saying every
// minute that it is waiting. A host busy at the end of it hands the part back in
// one line naming the load and the command to run again, and the part is not
// started. A part that finds the host calm, or whose load cannot be read, waits
// for nothing.
func (k Check) calmForPart(ctx context.Context, c *closeOut, attempt, n int, label string, left func() time.Duration) error {
	if k.Load == nil {
		return nil
	}
	_, _, err := Next{
		Load:          k.Load,
		LoadPoll:      k.LoadPoll,
		LoadBound:     k.partSlotWait(),
		LoadBoundLeft: left,
		LoadWait:      k.LoadWait,
		CalmHandBack: func(ctx context.Context, reading LoadReading, since time.Time, waited time.Duration, _ bool) error {
			return k.calmPartRefusal(ctx, c, attempt, n, label, reading, since, waited)
		},
		Host: k.Host,
		Out:  k.Out,
	}.calmed(ctx, &NextReport{}, c, label, true, false)
	return err
}

// calmPartRefusal is calmRefusal for a part: the load, since when, and the part
// to run again. A part handed back on the load again in the same attempt is
// counted as the one wait it is.
func (k Check) calmPartRefusal(ctx context.Context, c *closeOut, attempt, n int, label string, reading LoadReading, since time.Time, waited time.Duration) error {
	load := fmt.Sprintf("load %.1f of %d cores", reading.Load, reading.Cores)
	again := fmt.Sprintf("run the same part again with `mw check %s --part %d`", c.id, n)
	at := since.UTC().Format("15:04:05Z")
	earlier := k.noteWait(ctx, c.id, CheckPartCalmFileName(attempt, n), k.Host+" at "+load, since)
	if earlier != "" {
		return fmt.Errorf("%s did not start: after %s %s is still at %s, since %s, and nothing was run; it was busy when this part was tried at %s as well, which is counted once for this attempt and is not a failure of the part: %s, and if the host is busy again, comment on the story naming the load and hand back",
			label, gateWaited(waited), k.Host, load, at, earlier, again)
	}
	return fmt.Errorf("%s did not start: after %s %s is still at %s, since %s, and nothing was run; that is the host's load and not a failure of the part: %s",
		label, gateWaited(waited), k.Host, load, at, again)
}
