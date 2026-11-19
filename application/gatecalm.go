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
// lock given back once more. The wait for the lock is no part of the bound. A
// batch's gate takes the lock again ahead of single stories' checks and close-outs
// (WithQueuePrecedence, mw-gq6.1450): one of them may have it meanwhile, but none
// that queued while the batch waited goes before it.
//
// A gate whose lock can keep its holder's place (PlaceKeeper: a check's, taken
// under its story's key) keeps it while it waits, and takes the lock again in the
// place it had, ahead of everyone who asked after it (mw-gq6.1534). On the Laptop
// a millwright story's check that won the lock at load 53 of 20 gave it back, and
// took it again behind the checks and close-outs that had asked after it, until
// its 40m wait ran out having run nothing (mw-gq6.1521, six attempts). A batch
// still goes ahead of the kept place. The place is kept for what is left of the
// wait for calm and calmPlaceSlack more, so a gate that dies in the wait holds
// the queue no longer than that.

// calmedWithoutTheLock is calmedThen's wait, begun at reading with the host busy,
// made with the gate lock c holds given back.
//
// A close-out that holds its rig's merge slot across hosts, as one does from its
// merge to its push, gives that back for the wait too, and takes it again after
// the gate lock (mw-gq6.1423), as waitOffTheSlot does: the other host lands
// meanwhile, and a push from a merged result it left behind is refused and tried
// again from a fresh fetch.
func (n Next) calmedWithoutTheLock(ctx context.Context, report *NextReport, c *closeOut, what, then string, handBack, again bool, reading LoadReading) (_ LoadReading, gaveUp bool, err error) {
	if c.cross != nil {
		report.CrossSlot = append(report.CrossSlot, fmt.Sprintf("gave the cross-host merge slot of %s back while it waited for %s to calm before %s",
			c.path.Rig, n.Host, what))
		report.Notes = append(report.Notes, c.cross.Release(ctx)...)
		c.cross = nil
		defer func() {
			if err == nil {
				n.takeCrossSlot(ctx, c, report)
			}
		}()
	}
	var spent time.Duration
	for {
		gaveBack := n.giveTheLockBackForTheCalm(ctx, c, report, what, n.calmBoundLeft(handBack, spent)+calmPlaceSlack)
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

// calmPlaceSlack is how long past the bound of a wait for calm the place of a gate
// that gave its lock back for the wait is kept: the steps of the wait are counted,
// and the sleeps that overshoot at a high load are not (calmWait).
const calmPlaceSlack = 2 * time.Minute

// calmBoundLeft is what is left of the bound of a wait for calm that has spent
// spent, as calmWait reckons it.
func (n Next) calmBoundLeft(handBack bool, spent time.Duration) time.Duration {
	if handBack && n.CalmHandBack != nil && n.LoadBoundLeft != nil {
		return max(n.LoadBoundLeft(), 0)
	}
	return max(n.loadBound()-spent, 0)
}

// giveTheLockBackForTheCalm gives back the gate lock c holds, when it holds one,
// for a wait for the host to calm before what, and says so. A lock that can keep
// its holder's place in the queue keeps it for kept, before it is given back, so
// that nobody who asked after the holder takes the lock ahead of it again
// (mw-gq6.1534). It reports whether it gave one back.
func (n Next) giveTheLockBackForTheCalm(ctx context.Context, c *closeOut, report *NextReport, what string, kept time.Duration) bool {
	if c.turns == nil || c.turns.lock == nil {
		return false
	}
	again := "behind whoever waits for it"
	if HasQueuePrecedence(ctx) {
		again = "ahead of single stories' checks and close-outs"
	} else if keeper, ok := c.turns.lock.(PlaceKeeper); ok {
		if _, _, ok := keeper.KeepPlace(kept); ok {
			again = "in the place it had in the queue, ahead of whoever asked after it"
		}
	}
	n.print(fmt.Sprintf("  wait    %s gives %s's gate lock back while it waits for the host to calm before %s, and takes it again %s\n",
		c.id, n.Host, what, again))
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
