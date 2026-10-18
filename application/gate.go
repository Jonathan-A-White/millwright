package application

import (
	"context"
	"fmt"
	"time"
)

// The defaults of how a close-out lets a busy host calm before the rig's gate:
// it looks at the load every GateLoadPoll, and gives up waiting once GateLoadBound
// has gone by. A gate run on a busy host fails on timeouts that have nothing to
// do with the story (mw-gq6.228), and a refusal for that costs the Mayor a re-run
// by hand and holds the chain.
const (
	GateLoadPoll  = 30 * time.Second
	GateLoadBound = 15 * time.Minute
)

// GateLock is the port that keeps this host to one rig gate at a time
// (mw-gq6.388): the rig's tests are heavy enough to fill every core, and two
// of them side by side fail each other on timeouts. It is per host, for every
// rig's gate, and so is not the rig's merge slot, which keeps two close-outs
// off one rig's target branch.
type GateLock interface {
	// Take waits for the host's gate lock and reports the turn, which the
	// caller gives back. holder says whose gate it is, for whoever finds it
	// taken; held, when it is not nil, is told who has the lock the first time
	// it is found held, before Take waits for it, so that a caller that gives
	// up in held takes the lock only when it is free (mw-gq6.422). It gives up
	// when ctx does, or when the wait has gone on past the most a close-out
	// waits.
	Take(ctx context.Context, holder string, held func(by string)) (GateTurn, error)
}

// GateTurn is a gate lock somebody has, and what its taking waited for. Waited
// is zero and For empty when the lock was free.
type GateTurn struct {
	Holding Holding
	Waited  time.Duration
	For     string
}

func (n Next) loadPoll() time.Duration {
	if n.LoadPoll <= 0 {
		return GateLoadPoll
	}
	return n.LoadPoll
}

func (n Next) loadBound() time.Duration {
	if n.LoadBound <= 0 {
		return GateLoadBound
	}
	return n.LoadBound
}

// loadWait pauses for d: the real clock, unless LoadWait says otherwise.
func (n Next) loadWait(ctx context.Context, d time.Duration) error {
	if n.LoadWait != nil {
		return n.LoadWait(ctx, d)
	}
	return waitFor(ctx, d)
}

// readLoad is how busy the host is now. A host whose load is not asked for, or
// cannot be read, is not busy: the gate then runs as it always did.
func (n Next) readLoad(ctx context.Context) (LoadReading, bool) {
	if n.Load == nil {
		return LoadReading{}, false
	}
	reading, err := n.Load.Load(ctx)
	return reading, err == nil
}

// calmed waits, up to the bound, for the host to stop being busy, and says so as
// it does. It returns the last reading it took, and whether the bound ran out
// with the host still busy. The gate runs after it either way.
func (n Next) calmed(ctx context.Context, report *NextReport, c *closeOut, what string) (reading LoadReading, gaveUp bool) {
	reading, ok := n.readLoad(ctx)
	if !ok || !reading.Busy() {
		return reading, false
	}
	n.print(fmt.Sprintf("  wait    %s is at load %.1f of %d cores: waiting up to %s for it to calm before %s\n",
		n.Host, reading.Load, reading.Cores, n.loadBound(), what))
	// The close-out says it is waiting, and says it is running again once it stops.
	n.markCloseOut(ctx, c.id, c.began, true)
	defer n.markCloseOut(ctx, c.id, c.began, false)
	var waited time.Duration
	for reading.Busy() {
		if waited >= n.loadBound() {
			note := fmt.Sprintf("%s was still at load %.1f of %d cores after waiting %s, so %s ran anyway",
				n.Host, reading.Load, reading.Cores, waited, what)
			n.print("  wait    " + note + "\n")
			report.Notes = append(report.Notes, note)
			return reading, true
		}
		if err := n.loadWait(ctx, n.loadPoll()); err != nil {
			return reading, false
		}
		waited += n.loadPoll()
		next, ok := n.readLoad(ctx)
		if !ok {
			return reading, false
		}
		reading = next
	}
	report.Notes = append(report.Notes, fmt.Sprintf("%s was busy: waited %s for it to calm before %s", n.Host, waited, what))
	return reading, false
}

// gated is one run of the rig's tests as the close-out treats it. Said, when it
// is not empty, is how the two runs went: the first failed while the host was
// busy, and a second was made once it calmed.
type gated struct {
	Checked Checked
	Err     error
	Said    string
}

// gate runs the rig's tests in dir the way a close-out must on a host that may
// be busy, one gate at a time on the host: it takes the host's gate lock first,
// waiting for another close-out's gate to end, and keeps it until its own ends,
// the retry included, however it ends (mw-gq6.388). A lock that cannot be taken
// is said, and the tests run without it.
func (n Next) gate(ctx context.Context, c *closeOut, report *NextReport, dir, what string, span RelatedSpan) gated {
	if lock := n.waitForTheGateLock(ctx, c, report, what); lock != nil {
		defer n.giveTheGateLockBack(report, lock)
	}
	return n.gateOnACalmHost(ctx, c, report, dir, what, span)
}

// waitForTheGateLock takes this host's gate lock for the tests what names,
// waiting for another gate to end, and says so. A lock that cannot be taken is
// said, and nil returned: the tests run without it. A nil GateLock takes none.
func (n Next) waitForTheGateLock(ctx context.Context, c *closeOut, report *NextReport, what string) Holding {
	if n.GateLock == nil {
		return nil
	}
	turn, err := n.GateLock.Take(ctx, c.id, func(by string) {
		n.print(fmt.Sprintf("  wait    %s's gate lock is held by %s: waiting for it before %s\n", n.Host, by, what))
		// The close-out says it is waiting, so that mw status and the quiet
		// alarm do not take the story for quiet while it queues.
		n.markCloseOut(ctx, c.id, c.began, true)
	})
	if err != nil {
		n.markCloseOut(ctx, c.id, c.began, false)
		n.noteLocklessGate(report, err, what)
		return nil
	}
	if turn.For != "" {
		n.markCloseOut(ctx, c.id, c.began, false)
		report.Notes = append(report.Notes, fmt.Sprintf("gate waited %s for %s before %s", gateWaited(turn.Waited), turn.For, what))
	}
	return turn.Holding
}

// noteLocklessGate says the gate lock could not be taken, and that the tests
// what names ran without it.
func (n Next) noteLocklessGate(report *NextReport, err error, what string) {
	note := fmt.Sprintf("%s's gate lock could not be taken (%v), so %s ran without it", n.Host, err, what)
	n.print("  wait    " + note + "\n")
	report.Notes = append(report.Notes, note)
}

// giveTheGateLockBack gives this host's gate lock back, and says so when it
// could not be.
func (n Next) giveTheGateLockBack(report *NextReport, lock Holding) {
	if err := lock.Release(context.Background()); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s's gate lock could not be given back: %v", n.Host, err))
	}
}

// onMerged is the gate on a merged result, as what it says names it.
const onMerged = "the rig's tests on the merged result"

// mergedGate is what a close-out's gate on a merged result keeps between its
// tries at a landing, so that a try made after the rig's merge slot across
// hosts was given back for a wait finds that wait done (mw-gq6.422).
type mergedGate struct {
	// asked says this host's gate lock was asked for, and lock is it when it
	// was had: it is kept from then until the landing ends.
	asked bool
	lock  Holding

	// calmed says the host was let calm before the next run, which then runs at
	// once; before is the reading that run is judged against, and gaveUp says
	// the wait for calm ran out with the host still busy.
	calmed bool
	before LoadReading
	gaveUp bool

	// failed says the first run failed with the host busy, at the reading
	// busy: the next run is the second, made once it calms, and only its
	// failure counts.
	failed bool
	busy   LoadReading
}

// what is the next run, as what it says names it.
func (m mergedGate) what() string {
	if m.failed {
		return onMerged + " again"
	}
	return onMerged
}

// mergedResultGate runs the rig's tests on a merged result in dir as gate runs
// them in a worktree, but it never waits for this host while the close-out
// holds its rig's merge slot across hosts (mw-gq6.422): the other host's
// close-out of the rig would wait for this host's whole queue of gates, any
// rig's, and for its calm. When the gate lock is another gate's, or the host
// is busy before a run, it runs nothing and returns what it would wait for;
// merge gives the slot back while it waits (waitForTheMergedGate), takes it
// again, and tries again from a fresh fetch. A close-out that holds no slot
// across hosts waits where it is.
func (n Next) mergedResultGate(ctx context.Context, c *closeOut, report *NextReport, dir string, span RelatedSpan) (gated, string) {
	m := &c.merged
	for {
		waits := n.mergedGateWaits(ctx, c, report)
		if waits == "" {
			break
		}
		if c.cross != nil {
			return gated{}, waits
		}
		n.waitForTheMergedGate(ctx, c, report)
	}

	if m.failed {
		again, err := n.timedChecks(ctx, c, report, dir, span)
		c.gate.settle(again, err, m.busy)
		said := fmt.Sprintf("%s ran twice: the first run failed while %s was at load %.1f of %d cores, the second run %s",
			onMerged, n.Host, m.busy.Load, m.busy.Cores, secondRun(again, err))
		report.Notes = append(report.Notes, said)
		m.calmed, m.failed = false, false
		return gated{Checked: again, Err: err, Said: said}, ""
	}

	before := m.before
	m.calmed = false
	n.beginGate(ctx, c, before)
	checked, err := n.timedChecks(ctx, c, report, dir, span)
	if err != nil || checked.NotRun || checked.Passed {
		c.gate.settle(checked, err, before)
		return gated{Checked: checked, Err: err}, ""
	}
	busy := before
	if !m.gaveUp {
		after, ok := n.readLoad(ctx)
		if !ok || !after.Busy() {
			c.gate.settle(checked, nil, before)
			return gated{Checked: checked}, ""
		}
		busy = after
	}
	n.print(fmt.Sprintf("  retry   %s failed while %s was at load %.1f of %d cores: running them once more when it calms\n",
		onMerged, n.Host, busy.Load, busy.Cores))
	m.failed, m.busy = true, busy
	return n.mergedResultGate(ctx, c, report, dir, span)
}

// mergedGateWaits says what the next run on a merged result would wait for,
// without waiting for it: this host's gate lock, when another gate has it, or
// the host, to calm. It is empty when the run can be made now.
func (n Next) mergedGateWaits(ctx context.Context, c *closeOut, report *NextReport) string {
	m := &c.merged
	if n.GateLock != nil && !m.asked {
		// Asked for without waiting: the lock says it is held before it waits,
		// and the ask gives up there.
		try, cancel := context.WithCancel(ctx)
		var heldBy string
		turn, err := n.GateLock.Take(try, c.id, func(by string) { heldBy = by; cancel() })
		cancel()
		switch {
		case err == nil:
			m.asked, m.lock = true, turn.Holding
			if turn.For != "" {
				report.Notes = append(report.Notes, fmt.Sprintf("gate waited %s for %s before %s", gateWaited(turn.Waited), turn.For, m.what()))
			}
		case heldBy != "":
			return fmt.Sprintf("%s's gate lock, held by %s", n.Host, heldBy)
		default:
			m.asked = true
			n.noteLocklessGate(report, err, m.what())
		}
	}
	if m.calmed {
		return ""
	}
	reading, ok := n.readLoad(ctx)
	if ok && reading.Busy() {
		return fmt.Sprintf("%s to calm, at load %.1f of %d cores", n.Host, reading.Load, reading.Cores)
	}
	m.calmed, m.before, m.gaveUp = true, reading, false
	return ""
}

// waitForTheMergedGate waits for what the next run on a merged result waits
// for: this host's gate lock, which it keeps, and then the host, to calm. A
// close-out holding its rig's merge slot across hosts gives it back first.
func (n Next) waitForTheMergedGate(ctx context.Context, c *closeOut, report *NextReport) {
	m := &c.merged
	if n.GateLock != nil && !m.asked {
		m.asked, m.lock = true, n.waitForTheGateLock(ctx, c, report, m.what())
	}
	if !m.calmed {
		m.before, m.gaveUp = n.calmed(ctx, report, c, m.what())
		m.calmed = true
	}
}

// gateWaited is a wait as the report says it: "under a second", "40s", "7m",
// "7m30s", "1h5m".
func gateWaited(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Second {
		return "under a second"
	}
	var said string
	if h := d / time.Hour; h > 0 {
		said += fmt.Sprintf("%dh", h)
	}
	if m := d % time.Hour / time.Minute; m > 0 {
		said += fmt.Sprintf("%dm", m)
	}
	if s := d % time.Minute / time.Second; s > 0 {
		said += fmt.Sprintf("%ds", s)
	}
	return said
}

// gateOnACalmHost runs the rig's tests in dir the way a close-out must on a
// host that may be busy: it waits, bounded, for the load to drop below the core
// count first;
// and a run that fails while the host was busy at a reading it took — before the
// run, or after it — is run once more when the host has calmed, and only a
// second failure counts. A run that fails on a calm host counts at once. A
// command that would not start is not a load fault and is never run again.
func (n Next) gateOnACalmHost(ctx context.Context, c *closeOut, report *NextReport, dir, what string, span RelatedSpan) gated {
	before, gaveUp := n.calmed(ctx, report, c, what)
	n.beginGate(ctx, c, before)
	checked, err := n.timedChecks(ctx, c, report, dir, span)
	if err != nil || checked.NotRun || checked.Passed {
		c.gate.settle(checked, err, before)
		return gated{Checked: checked, Err: err}
	}

	busy := before
	if !gaveUp {
		after, ok := n.readLoad(ctx)
		if !ok || !after.Busy() {
			c.gate.settle(checked, nil, before)
			return gated{Checked: checked}
		}
		busy = after
	}
	n.print(fmt.Sprintf("  retry   %s failed while %s was at load %.1f of %d cores: running them once more when it calms\n",
		what, n.Host, busy.Load, busy.Cores))
	n.calmed(ctx, report, c, what+" again")
	again, err := n.timedChecks(ctx, c, report, dir, span)
	c.gate.settle(again, err, busy)
	said := fmt.Sprintf("%s ran twice: the first run failed while %s was at load %.1f of %d cores, the second run %s",
		what, n.Host, busy.Load, busy.Cores, secondRun(again, err))
	report.Notes = append(report.Notes, said)
	return gated{Checked: again, Err: err, Said: said}
}

func secondRun(checked Checked, err error) string {
	switch {
	case err != nil, checked.NotRun:
		return "could not be run"
	case checked.Passed:
		return "passed"
	}
	return "failed"
}
