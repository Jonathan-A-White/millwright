package application

import (
	"context"
	"fmt"
	"time"
)

// No holder of the host's gate lock keeps it while it only waits for the host to
// calm (mw-gq6.1420). A batch's gate on the Laptop waited 'up to 15m0s for it to
// calm' with the lock held, and so did a whole check, at load 24 of 20, while two
// Builders' part checks, which take no lock, kept the host there: every holder
// kept the lock 15 to 37 minutes, and a batch of 11 parts queued behind each of
// them after every part, over 4 hours. A gate holding the lock (closeOut.turns)
// gives it back for the wait and takes it again behind whoever queued meanwhile;
// when that take waited for another gate, the host that gate leaves is read again,
// and a host busy again is waited on with what is left of the same bound, the
// lock given back once more. The wait for the lock is no part of the bound.

// calmedWithoutTheLock is calmedThen's wait, begun at reading with the host busy,
// made with the gate lock c holds given back.
func (n Next) calmedWithoutTheLock(ctx context.Context, report *NextReport, c *closeOut, what, then string, handBack, again bool, reading LoadReading) (LoadReading, bool, error) {
	var spent time.Duration
	for {
		gaveBack := n.giveTheLockBackForTheCalm(c, report, what)
		var gaveUp bool
		var err error
		reading, gaveUp, spent, err = n.calmWait(ctx, report, c, what, then, handBack, again, reading, spent)
		if !gaveBack || err != nil {
			// A wait that ends the gate takes no lock again: nothing is run.
			return reading, gaveUp, err
		}
		behindAnother, err := n.takeTheLockAgainAfterTheCalm(ctx, c, report, what)
		if err != nil || gaveUp || !behindAnother {
			return reading, gaveUp, err
		}
		now, ok := n.readLoad(ctx)
		if !ok {
			return reading, false, nil
		}
		if !now.Busy() {
			return now, false, nil
		}
		reading = now
	}
}

// giveTheLockBackForTheCalm gives back the gate lock c holds, when it holds one,
// for a wait for the host to calm before what, and says so. It reports whether it
// gave one back.
func (n Next) giveTheLockBackForTheCalm(c *closeOut, report *NextReport, what string) bool {
	if c.turns == nil || c.turns.lock == nil {
		return false
	}
	n.print(fmt.Sprintf("  wait    %s gives %s's gate lock back while it waits for the host to calm before %s, and takes it again behind whoever waits for it\n",
		c.id, n.Host, what))
	n.giveTheGateLockBack(report, c.turns.lock)
	c.turns.lock = nil
	return true
}

// takeTheLockAgainAfterTheCalm takes the gate lock again once a wait for a calm
// host before what is over, and counts the wait with the turns' waits for other
// gates. behindAnother says the lock was another gate's when it was asked for. A
// lock that hands back ends the gate with its error; one that cannot be taken is
// said, and the gate goes on without it, as a gate does.
func (n Next) takeTheLockAgainAfterTheCalm(ctx context.Context, c *closeOut, report *NextReport, what string) (behindAnother bool, err error) {
	turns := c.turns
	began := n.now()
	lock, turn, err := n.takeTheGateLockTurn(ctx, c, report, what)
	turns.waited += n.now().Sub(began)
	if err != nil {
		return false, err
	}
	turns.lock = lock
	return lock != nil && turn.For != "", nil
}

// holdingWhole is takingTurns for a gate that runs the rig's tests whole and
// takes no turns between parts, but gives its lock back for a wait for a calm
// host all the same: the run of the failing tests on main alone, and the run of
// a story's tests with the target merged in.
func (n Next) holdingWhole(c *closeOut, report *NextReport, lock Holding) func() {
	release := n.takingTurns(c, report, lock)
	if c.turns != nil {
		c.turns.whole = true
	}
	return release
}

// heldNow is the gate lock a gate that took lock holds now: the one its turns
// hold, which a wait for a calm host may have given back and taken again.
func (c *closeOut) heldNow(lock Holding) Holding {
	if lock != nil && c.turns != nil {
		return c.turns.lock
	}
	return lock
}

// gateTook is the line that ends a batch's gate in mw land's log (mw-gq6.1420):
// how it ended, how long it ran, and how long of that it waited for other gates
// and for the host to calm.
func gateTook(label, host, ended string, took, others, calm time.Duration) string {
	return fmt.Sprintf("the gate of %s ended (%s): it ran %s, and waited %s of it for other gates and %s for %s to calm",
		label, ended, gateWaited(took), gateWaited(others), gateWaited(calm), host)
}

// specsAgain is a batch's changed specs, which failed in first, dealt with as a
// gate's failed run is (waitToRunAgain): a failure read with the host far over its
// cores waits for it to calm, with the gate lock given back, and the specs are run
// once more, the second run standing; any other failure stands at once
// (mw-gq6.1079). The error is the wait's, or the second run's.
func (n Next) specsAgain(ctx context.Context, c *closeOut, report *NextReport, dir string, span RelatedSpan, before LoadReading, first SpecsChecked) (SpecsChecked, error) {
	busy, again, err := n.waitToRunAgain(ctx, c, report, "the changed specs", "them", before, false)
	if err != nil || !again {
		return first, err
	}
	if err := n.gateGivesWay(ctx, c); err != nil {
		return first, err
	}
	run, err := n.SpecChecks.RunSpecs(ctx, c.path.Rig, dir, span.Base)
	report.Notes = append(report.Notes, ranTwice("the changed specs", n.Host, busy, first.Checked, run.Checked, err))
	return run, err
}

// gateEnded is how a batch's gate ended, as gateTook says it.
func gateEnded(g gated) string {
	switch {
	case g.Err != nil:
		return "not run: " + firstLine(g.Err.Error())
	case g.Checked.NotRun:
		return "did not start"
	case g.Checked.Passed:
		return "passed"
	}
	return "failed"
}
