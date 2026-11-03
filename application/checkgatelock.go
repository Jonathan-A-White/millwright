package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// A Builder's whole mw check runs the rig's tests, and the rig's tests fill the
// host: the desktop's gates took 211 s alone and 601 to 797 s with two to four
// stories running, and the five gates a Builder's check finished inside took
// 1,472 s on average against 419 s for the 38 none did (the vault's bench lines
// of the 7 days to 2026-11-03; docs/dispatch-gates.md). So a whole check takes
// the host's gate lock, the one a close-out's gate takes (GateLock, mw-gq6.388),
// and one gate-weight run goes at a time on a host whoever asked for it
// (mw-gq6.906).
//
// The order is the rig's merge slot, shared, and then the gate lock: a close-out
// that holds the slot alone and waits for the lock (mw next holds it throughout)
// can then never wait for a check that waits for the slot. Both waits are one
// budget, partSlotWait: under the 590 seconds a Builder's foreground call is
// given, ending in one line that names the holder, since when, and the command
// to run again, not in a loop that spends the Builder's turns. The wait for a
// calm host that follows (checkcalm.go) is the same deadline, not another: it
// gets what these waits left (Next.LoadBoundLeft, mw-gq6.915). mw check --part
// does not take it (docs/dispatch-gates.md says why).

// CheckGateNoticeEvery is how often a check that waits for the host's gate lock
// says again that it is waiting, and for whom.
const CheckGateNoticeEvery = time.Minute

// gateLockReader is what a GateLock may also say, read without taking it: whose
// gate holds it now and since when. Neither is needed to wait.
type gateLockReader interface {
	Holder() string
	HeldSince() time.Time
}

// CheckGateHeldFileName is the name of the record of the waits of a whole
// check's tests on the host's gate lock, in the story's run directory, one for
// the attempt (mw-gq6.906).
func CheckGateHeldFileName(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-gate.slot", attempt)
}

func (k Check) gateNoticeEvery() time.Duration {
	if k.GateNoticeEvery > 0 {
		return k.GateNoticeEvery
	}
	return CheckGateNoticeEvery
}

// checkHolder is how a check names itself to whoever finds a lock it holds.
func (k Check) checkHolder(c *closeOut) string {
	return fmt.Sprintf("%s checking %s", SeatIdentity("", k.Host), c.id)
}

// checkGateLock is the GateLock the Next a whole check reads its refusals with
// takes before the rig's tests: the rig's merge slot, shared, and then the
// host's gate lock, both within the one wait a check is allowed.
type checkGateLock struct {
	k       Check
	c       *closeOut
	attempt int
}

// Take implements GateLock. A wait that ran out is a GateLockHandBack, which
// ends the gate in one line; the holder name it is given is the check's own.
func (g checkGateLock) Take(ctx context.Context, _ string, held func(by string)) (GateTurn, error) {
	return g.k.takeForTests(ctx, g.c, wholeTry(g.c, "the rig's tests", CheckWholeHeldFileName(g.attempt)), held)
}

// takeForTests takes what a run of the rig's tests holds in a worktree check:
// the rig's merge slot, when it has one, shared with the other checks, and then
// the host's gate lock, when it has one. The wait for both is no longer than
// partSlotWait. held is told who has the gate lock the first time it is found
// held. The holdings are given back together.
func (k Check) takeForTests(ctx context.Context, c *closeOut, try slotTry, held func(by string)) (GateTurn, error) {
	began := time.Now()
	var slot Holding
	if k.Slot != nil {
		taken, err := k.takeSlotFor(ctx, c, try, k.partSlotWait())
		if err != nil {
			return GateTurn{}, GateLockHandBack{Why: err.Error()}
		}
		slot = taken
	}
	if k.GateLock == nil {
		return GateTurn{Holding: bothHoldings{slot: slot}}, nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, max(k.partSlotWait()-time.Since(began), 0))
	defer cancel()
	notices := &gateNotices{k: k, ctx: waitCtx}
	defer notices.stop()
	holder := k.checkHolder(c)
	turn, err := k.GateLock.Take(waitCtx, holder, func(by string) {
		if held != nil {
			held(by)
		}
		notices.start(by)
	})
	if err != nil {
		if slot != nil {
			slot.Release(context.Background())
		}
		return GateTurn{}, k.gateLockRefusal(ctx, waitCtx, c, try, notices.by, time.Since(began), err)
	}
	return GateTurn{Holding: bothHoldings{slot: slot, lock: turn.Holding}, Waited: turn.Waited, For: turn.For}, nil
}

// gateNotices says every CheckGateNoticeEvery, while a check waits for the gate
// lock, that it is still waiting and who holds the lock now.
type gateNotices struct {
	k    Check
	ctx  context.Context
	by   string
	done chan struct{}
	quit chan struct{}
	once sync.Once
}

func (g *gateNotices) start(by string) {
	g.by = by
	if g.k.Out == nil || g.quit != nil {
		return
	}
	g.quit, g.done = make(chan struct{}), make(chan struct{})
	waiting := time.Now()
	go func() {
		defer close(g.done)
		tick := time.NewTicker(g.k.gateNoticeEvery())
		defer tick.Stop()
		for {
			select {
			case <-g.quit:
				return
			case <-g.ctx.Done():
				return
			case <-tick.C:
				holder, since := g.k.gateHolderNow(g.by, waiting)
				fmt.Fprintf(g.k.Out, "  wait    still waiting for %s's gate lock, held by %s since %s (waited %s so far)\n",
					g.k.Host, holder, since.UTC().Format("15:04:05Z"), gateWaited(time.Since(waiting)))
			}
		}
	}()
}

// stop ends the notices and waits for the last to be written.
func (g *gateNotices) stop() {
	g.once.Do(func() {
		if g.quit != nil {
			close(g.quit)
			<-g.done
		}
	})
}

// gateHolderNow is whose gate holds the lock and since when, as the lock says
// when it can; else the holder it was first found held by, since the wait began.
func (k Check) gateHolderNow(by string, began time.Time) (holder string, since time.Time) {
	holder, since = by, began
	if reader, ok := k.GateLock.(gateLockReader); ok {
		if now := reader.Holder(); now != "" {
			holder = now
			if at := reader.HeldSince(); !at.IsZero() {
				since = at
			}
		}
	}
	return holder, since
}

// gateLockRefusal is the one line a check ends with when the host's gate lock
// could not be taken. A wait that ran out of its budget names who holds the
// lock, since when, and the command to run next, which is the same check again:
// the wait is the other gate's, not the check's failure, and a check handed back
// again in the same attempt is counted as the one wait it is. Any other trouble
// is told as it was.
func (k Check) gateLockRefusal(ctx, waitCtx context.Context, c *closeOut, try slotTry, by string, waited time.Duration, err error) error {
	if ctx.Err() != nil || waitCtx.Err() == nil || !errors.Is(err, context.DeadlineExceeded) {
		return GateLockHandBack{Why: fmt.Sprintf("%s's gate lock could not be taken: %v", k.Host, err)}
	}
	holder, since := k.gateHolderNow(by, time.Now().Add(-waited))
	sinceAt := fmt.Sprintf("%s (%s ago)", since.UTC().Format("15:04:05Z"), time.Since(since).Round(time.Second))
	record := strings.TrimSuffix(try.record, ".slot") + "-gate.slot"
	if earlier := k.noteWait(ctx, c.id, record, holder, since); earlier != "" {
		return GateLockHandBack{Why: fmt.Sprintf("%s did not start: after %s %s's gate lock is still held by %s, since %s, and nothing was run; it was held when this %s was tried at %s as well, which is counted once for this attempt and is not a failure of the %s: %s, and if the lock is held again, comment on the story naming this %s and who holds the lock, and hand back",
			try.label, waited.Round(time.Second), k.Host, holder, sinceAt, try.noun, earlier, try.noun, try.again, try.noun)}
	}
	return GateLockHandBack{Why: fmt.Sprintf("%s did not start: after %s %s's gate lock is still held by %s, since %s, and nothing was run; that is the other gate's wait and not a failure of the %s: %s",
		try.label, waited.Round(time.Second), k.Host, holder, sinceAt, try.noun, try.again)}
}

// bothHoldings is the merge slot and the gate lock a check holds around the
// rig's tests; either may be nil. The lock is given back first.
type bothHoldings struct{ slot, lock Holding }

func (b bothHoldings) Release(ctx context.Context) error {
	var errs []error
	if b.lock != nil {
		errs = append(errs, b.lock.Release(ctx))
	}
	if b.slot != nil {
		errs = append(errs, b.slot.Release(ctx))
	}
	return errors.Join(errs...)
}

func (b bothHoldings) HeldBy() string {
	switch {
	case b.lock != nil:
		return b.lock.HeldBy()
	case b.slot != nil:
		return b.slot.HeldBy()
	}
	return ""
}
