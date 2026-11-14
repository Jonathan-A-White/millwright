package application

import (
	"cmp"
	"context"
	"fmt"
	"time"
)

// The one lock order of the rig's merge slot and the host's gate lock is the
// slot, then the lock: a check (Check.takeForTests) and a close-out take them so.
// A batch landing holds the slot alone and gives it up to shared for a wait of its
// own (SlotYield: for the gate lock, for a calm host, for its fixer), which makes
// it a taker of the slot after the lock whenever the lock is already held when the
// wait ends: it holds the lock and asks for the slot alone, which waits for every
// check that took the slot shared, and a whole check that took it waits for the
// lock the landing holds. Neither moves until the check's wait runs out
// (mw-gq6.1316). So a landing holding the gate lock gives it back before it takes
// the slot alone again, and asks for it again once it has the slot.

// slotAndLockTries is how many times a close-out that gives its merge slot up for
// a wait for the gate lock gives the lock back to take the slot alone again, and
// waits for the lock once more with the slot given up, before it keeps the slot.
const slotAndLockTries = 3

// takeGateLockUnderTheSlot is the host's gate lock taken by a close-out that may
// yield its merge slot for the wait (Next.SlotYield), in the lock order above: the
// slot is given up for the wait for the lock and, when the lock comes, the lock is
// given back, the slot taken alone again and the lock asked for once more, up to
// slotAndLockTries times; after that the lock is waited for with the slot kept, as
// no check can then wait for the lock beside it. The turn's wait is the sum of the
// asks'.
func (n Next) takeGateLockUnderTheSlot(ctx context.Context, c *closeOut, report *NextReport, what string) (GateTurn, error) {
	_, saysItsOwn := n.GateLock.(checkGateLock)
	var turn GateTurn
	var err error
	var waited time.Duration
	var waitedFor string
	for try := 0; ; try++ {
		var resume func()
		yielding := n.SlotYield != nil && try < slotAndLockTries
		turn, err = n.GateLock.Take(ctx, c.id, func(by string) {
			// A check's lock says its own wait, naming more (Check.heldLine).
			if !saysItsOwn && try == 0 {
				n.print(fmt.Sprintf("  wait    %s's gate lock is held by %s: waiting for it before %s\n", n.Host, by, what))
			}
			// The close-out says it is waiting, so that mw status and the quiet
			// alarm do not take the story for quiet while it queues.
			n.markCloseOut(ctx, c.id, c.began, true)
			if yielding && resume == nil {
				resume = n.SlotYield(ctx)
			}
		})
		waited += turn.Waited
		waitedFor = cmp.Or(waitedFor, turn.For)
		if resume == nil {
			break
		}
		if err == nil && turn.Holding != nil {
			n.giveBackBeforeTheSlot(report, turn.Holding)
			resume()
			continue
		}
		resume()
		break
	}
	turn.Waited, turn.For = waited, waitedFor
	return turn, err
}

// giveBackBeforeTheSlot gives the gate lock back when the merge slot is about to
// be taken alone again, and says so when it could not be.
func (n Next) giveBackBeforeTheSlot(report *NextReport, lock Holding) {
	if err := lock.Release(context.Background()); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s's gate lock could not be given back before the merge slot was taken alone again: %v", n.Host, err))
	}
}

// slotYieldingLock is the gate lock of a close-out that yields its merge slot,
// held in a way that lets a wait of the close-out's own end by giving the lock
// back and asking for it again once the slot is taken alone (resumeSlot). It is
// the Holding the gate's callers give back as any other; a lock that could not be
// asked for again is nil inside, and giving it back does nothing.
type slotYieldingLock struct {
	c     *closeOut
	what  string
	inner Holding
}

// keepTheLockForTheSlot is lock as the close-out holds it: itself when the
// close-out yields no slot, else a slotYieldingLock c can find.
func (n Next) keepTheLockForTheSlot(c *closeOut, what string, lock Holding) Holding {
	if lock == nil || n.SlotYield == nil {
		return lock
	}
	held := &slotYieldingLock{c: c, what: what, inner: lock}
	c.gateHold = held
	return held
}

func (l *slotYieldingLock) Release(ctx context.Context) error {
	if l.c.gateHold == l {
		l.c.gateHold = nil
	}
	held := l.inner
	l.inner = nil
	if held == nil {
		return nil
	}
	return held.Release(ctx)
}

func (l *slotYieldingLock) HeldBy() string {
	if l.inner == nil {
		return ""
	}
	return l.inner.HeldBy()
}

// resumeSlot ends a wait of the close-out's that gave its merge slot up
// (SlotYield): resume takes the slot alone again, and the gate lock the close-out
// holds is given back for it and asked for once the slot is held, so that a check
// that took the slot shared meanwhile and waits for the lock is not waited for
// with the lock held (mw-gq6.1316). A lock that cannot be asked for again is said,
// as a lock that could not be taken is, and the close-out goes on without it.
func (n Next) resumeSlot(ctx context.Context, c *closeOut, report *NextReport, resume func()) {
	held := c.gateHold
	if held == nil || held.inner == nil {
		resume()
		return
	}
	n.giveBackBeforeTheSlot(report, held.inner)
	held.inner = nil
	resume()
	turn, err := n.takeGateLockUnderTheSlot(ctx, c, report, held.what)
	if err != nil {
		n.noteLocklessGate(report, err, held.what)
		return
	}
	held.inner = turn.Holding
}
