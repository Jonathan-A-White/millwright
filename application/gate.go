package application

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
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

// GateLockHandBack is what a GateLock's Take returns to end the gate it was
// asked for, in place of letting the tests run without the lock: a mw check
// whose wait for the lock ran out in its foreground budget (mw-gq6.906). A
// close-out's lock is never one that hands back, so its gate goes on without a
// lock it could not take, as before (mw-gq6.388). Why is one line, and is what
// the refusal says.
type GateLockHandBack struct{ Why string }

func (e GateLockHandBack) Error() string { return e.Why }

// GateTurn is a gate lock somebody has, and what its taking waited for. Waited
// is zero and For empty when the lock was free.
type GateTurn struct {
	Holding Holding
	Waited  time.Duration
	For     string
}

// HostBusyError is why the rig's tests were not run, or not counted, on a host
// far over its cores (LoadReading.FarOver, mw-gq6.903): the wait for it to calm
// ran out with it still there, or a run failed and the host was far over when it
// ended. It is the host's, not the story's: a close-out hands the story back for
// its close-out alone, and no attempt is spent.
type HostBusyError struct {
	Host    string
	Reading LoadReading
	// Waited is how long the wait for the host to calm went on, zero when the
	// error is of a run that failed far over.
	Waited time.Duration
	// What names the run that was not made or not counted.
	What string
	// After says a run was made and failed, with the host far over after it.
	After bool
}

func (e HostBusyError) Error() string {
	load := fmt.Sprintf("load %.1f of %d cores, at least %.0f times them", e.Reading.Load, e.Reading.Cores, GateFarOver)
	if e.After {
		return fmt.Sprintf("%s failed and %s was at %s when it ended, so the failure is the host's and not the story's: nothing is counted against the story",
			e.What, e.Host, load)
	}
	return fmt.Sprintf("%s was still at %s after waiting %s, so %s was not run: that is the host's load and not a failure of the story",
		e.Host, load, gateWaited(e.Waited), e.What)
}

// HostStillBusyError is why a batch's tests were not run: the wait for the host
// to calm ran out with it over its cores, though not far over (a host far over
// is a HostBusyError). A run at that load times out jsdom tests of a good batch
// (batch 410 of lampas, 30.4 of 20 cores, mw-gq6.1007), so the batch is left
// queued for the next landing and no fixer is started.
type HostStillBusyError struct {
	Reading LoadReading
}

func (e HostStillBusyError) Error() string {
	return fmt.Sprintf("host still at load %.1f of %d: the batch waits for the next landing", e.Reading.Load, e.Reading.Cores)
}

// isHostStillBusy reports whether err is a HostStillBusyError.
func isHostStillBusy(err error) bool {
	var busy HostStillBusyError
	return errors.As(err, &busy)
}

// isHostBusy reports whether err is a HostBusyError.
func isHostBusy(err error) bool {
	var busy HostBusyError
	return errors.As(err, &busy)
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

// calmedMinute is how often a wait that ends in a hand-back (Next.CalmHandBack)
// says it is still going on.
const calmedMinute = time.Minute

// calmed waits, up to the bound, for the host to stop being busy, and says so as
// it does. It returns the last reading it took, and whether the bound ran out
// with the host still busy. The gate runs after it either way, unless handBack
// is asked for and Next.CalmHandBack is set: then a bound that ran out returns
// that hook's error, and the gate does not run (mw-gq6.819); again says the wait
// is the second one, before the rerun of a run that failed while the host was
// busy, which is under the same deadline (mw-gq6.918). A bound that ran out
// with the host far over its cores returns a HostBusyError either way, and the
// gate does not run (mw-gq6.903); a batch's gate (closeOut.waitsWhenBusy) does
// not run with the host busy at all, and returns a HostStillBusyError (mw-gq6.1007).
func (n Next) calmed(ctx context.Context, report *NextReport, c *closeOut, what string, handBack, again bool) (reading LoadReading, gaveUp bool, err error) {
	return n.calmedThen(ctx, report, c, what, "ran anyway", handBack, again)
}

// calmedThen is calmed, with the end of the note a bound that ran out leaves
// when the host is busy but not far over: then says what became of the run, "ran
// anyway" for a run that is made, so that a run that is not made can say so
// (mw-gq6.930).
func (n Next) calmedThen(ctx context.Context, report *NextReport, c *closeOut, what, then string, handBack, again bool) (reading LoadReading, gaveUp bool, err error) {
	reading, ok := n.readLoad(ctx)
	if !ok || !reading.Busy() {
		return reading, false, nil
	}
	handBack = handBack && n.CalmHandBack != nil
	bound := n.loadBound()
	if handBack && n.LoadBoundLeft != nil {
		bound = max(n.LoadBoundLeft(), 0)
	}
	n.print(fmt.Sprintf("  wait    %s is at load %.1f of %d cores: waiting up to %s for it to calm before %s\n",
		n.Host, reading.Load, reading.Cores, bound, what))
	// The close-out says it is waiting, and says it is running again once it stops.
	n.markCloseOut(ctx, c.id, c.began, true)
	defer n.markCloseOut(ctx, c.id, c.began, false)
	if n.SlotYield != nil {
		defer n.SlotYield(ctx)()
	}
	since := n.now()
	var waited, said time.Duration
	for reading.Busy() {
		// The steps waited are a count, not the clock: the reads of a host at load 43
		// and the sleeps that overshoot are not in it. A call's deadline that has
		// passed ends the wait however few steps were counted (mw-gq6.1251).
		if handBack && n.LoadBoundLeft != nil && n.LoadBoundLeft() <= 0 {
			waited = max(waited, bound)
		}
		if waited >= bound {
			if handBack {
				return reading, true, n.CalmHandBack(ctx, reading, since, waited, again)
			}
			if reading.FarOver() {
				// Far over the cores a run only fails for the host, and
				// takes the host's other work down with it.
				busy := HostBusyError{Host: n.Host, Reading: reading, Waited: waited, What: what}
				n.print("  wait    " + busy.Error() + "\n")
				return reading, true, busy
			}
			if c.waitsWhenBusy {
				still := HostStillBusyError{Reading: reading}
				n.print("  wait    " + still.Error() + "\n")
				return reading, true, still
			}
			note := fmt.Sprintf("%s was still at load %.1f of %d cores after waiting %s, so %s %s",
				n.Host, reading.Load, reading.Cores, waited, what, then)
			n.print("  wait    " + note + "\n")
			report.Notes = append(report.Notes, note)
			return reading, true, nil
		}
		step := n.loadPoll()
		if handBack {
			step = min(step, bound-waited)
		}
		if err := n.loadWait(ctx, step); err != nil {
			return reading, false, nil
		}
		waited += step
		next, ok := n.readLoad(ctx)
		if !ok {
			return reading, false, nil
		}
		reading = next
		if handBack && reading.Busy() && waited-said >= calmedMinute {
			said = waited
			n.print(fmt.Sprintf("  wait    still waiting for %s to calm: at load %.1f of %d cores since %s (%s so far, up to %s)\n",
				n.Host, reading.Load, reading.Cores, since.UTC().Format("15:04:05Z"), gateWaited(waited), gateWaited(bound)))
		}
	}
	report.Notes = append(report.Notes, fmt.Sprintf("%s was busy: waited %s for it to calm before %s", n.Host, waited, what))
	return reading, false, nil
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
	lock, err := n.takeTheGateLock(ctx, c, report, what)
	if err != nil {
		return gated{Err: err}
	}
	if lock != nil {
		defer n.giveTheGateLockBack(report, lock)
	}
	return n.gateOnACalmHostHolding(ctx, c, report, dir, what, span, lock)
}

// gateWithSpecs is gate for a landing that has no close-out of its own to run
// the rig's changed-specs command after the gate, as mw next does (specsRefusal):
// a batch (mw-gq6.627). Under the one gate lock it runs the rig's tests, and,
// when they passed, the rig's changed-specs command on the specs the span's
// range added or changed in dir, once. The gated it returns is the verdict: the
// tests' own when they did not pass or no spec was changed, else the specs'
// run, with Said naming the specs. log is what the gate ran as a gate log
// holds it, one "$ command" and its output after another.
func (n Next) gateWithSpecs(ctx context.Context, c *closeOut, report *NextReport, dir, what string, span RelatedSpan) (verdict gated, log string) {
	// The lock is held by turns: a rig whose gate is named as parts gives it back
	// between them (gateturns.go, mw-gq6.1275).
	defer n.takingTurns(c, report, n.waitForTheGateLock(ctx, c, report, what))()
	tests := n.gateOnACalmHost(ctx, c, report, dir, what, span)
	log = gateLogOf(tests.Checked)
	if tests.Err != nil || tests.Checked.NotRun || !tests.Checked.Passed || n.SpecChecks == nil {
		return tests, log
	}
	if err := n.gateGivesWay(ctx, c); err != nil {
		return gated{Err: err}, log
	}
	run, err := n.SpecChecks.RunSpecs(ctx, c.path.Rig, dir, span.Base)
	if err != nil {
		return gated{Err: fmt.Errorf("the rig's changed specs could not be run in %s: %w", dir, err)}, log
	}
	n.sayGateRead(run.Checked)
	if len(run.LeftOut) > 0 {
		if !strings.HasSuffix(log, "\n") {
			log += "\n"
		}
		log += LeftOutTimingSpecs(run.LeftOut) + "\n"
	}
	if len(run.Specs) == 0 {
		return tests, log
	}
	if !strings.HasSuffix(log, "\n") {
		log += "\n"
	}
	log += gateLogOf(run.Checked)
	verdict = gated{Checked: run.Checked}
	if !run.Passed && !run.NotRun {
		verdict.Said = "the changed specs: " + strings.Join(run.Specs, ", ")
	}
	return verdict, log
}

// GivesWayError is why a gate part was not started: the host gave way to the
// person at its screen after the gate began, and a host that gives way starts no
// part of a gate (mw-2891blf.19). It is no failure of the tests.
type GivesWayError struct{ Standing GiveWayStanding }

func (e *GivesWayError) Error() string {
	return fmt.Sprintf("the host %s, so no part of the gate is started", e.Standing)
}

// isGivesWay reports whether err is a GivesWayError.
func isGivesWay(err error) bool {
	var giving *GivesWayError
	return errors.As(err, &giving)
}

// gateGivesWay is the read of the give-way marker (GivesWayNow) right before a part
// of a batch's gate starts: the rig's tests, their run again, and the changed
// specs. It is nil for a gate whose close-out does not read the marker
// (closeOut.readsGiveWay) and for a host that is free.
func (n Next) gateGivesWay(ctx context.Context, c *closeOut) error {
	if !c.readsGiveWay {
		return nil
	}
	if standing, giving := GivesWayNow(ctx, n.GiveWay, false); giving {
		return &GivesWayError{Standing: standing}
	}
	return nil
}

// gateLogOf is one command of a gate as a gate log holds it.
func gateLogOf(c Checked) string {
	var read strings.Builder
	for _, said := range c.Read.Lines(c.Command) {
		fmt.Fprintf(&read, "# %s\n", said)
	}
	return fmt.Sprintf("$ %s\n%s%s", c.Command, read.String(), c.Output)
}

// sayGateRead prints, as a gate command runs, when its line was read from the
// host's config and that it differs from the line the batch began with
// (mw-gq6.1006). Only a port that reads the line afresh (mw land's) has anything
// to say; the lines open as the rest of mw land's log do.
func (n Next) sayGateRead(c Checked) {
	for _, said := range c.Read.Lines(c.Command) {
		n.print(BatchLogPrefix + said + "\n")
	}
}

// waitForTheGateLock takes this host's gate lock for the tests what names,
// waiting for another gate to end, and says so. A lock that cannot be taken is
// said, and nil returned: the tests run without it. A nil GateLock takes none.
func (n Next) waitForTheGateLock(ctx context.Context, c *closeOut, report *NextReport, what string) Holding {
	lock, _ := n.takeTheGateLock(ctx, c, report, what)
	return lock
}

// takeTheGateLock is waitForTheGateLock for a gate that must not run without
// the lock when the lock says to hand it back (GateLockHandBack): then the lock
// is nil and the error is the hand-back, which ends the gate.
func (n Next) takeTheGateLock(ctx context.Context, c *closeOut, report *NextReport, what string) (Holding, error) {
	if n.GateLock == nil {
		return nil, nil
	}
	var resume func()
	_, saysItsOwn := n.GateLock.(checkGateLock)
	turn, err := n.GateLock.Take(ctx, c.id, func(by string) {
		// A check's lock says its own wait, naming more (Check.heldLine).
		if !saysItsOwn {
			n.print(fmt.Sprintf("  wait    %s's gate lock is held by %s: waiting for it before %s\n", n.Host, by, what))
		}
		// The close-out says it is waiting, so that mw status and the quiet
		// alarm do not take the story for quiet while it queues.
		n.markCloseOut(ctx, c.id, c.began, true)
		if n.SlotYield != nil && resume == nil {
			resume = n.SlotYield(ctx)
		}
	})
	if resume != nil {
		resume()
	}
	if err != nil {
		n.markCloseOut(ctx, c.id, c.began, false)
		var handBack GateLockHandBack
		if errors.As(err, &handBack) {
			return nil, handBack
		}
		n.noteLocklessGate(report, err, what)
		return nil, nil
	}
	if turn.For != "" {
		n.markCloseOut(ctx, c.id, c.began, false)
		report.Notes = append(report.Notes, fmt.Sprintf("gate waited %s for %s before %s", gateWaited(turn.Waited), turn.For, what))
	}
	return turn.Holding, nil
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
	// hostBusy is the wait for calm that ended with the host far over its cores:
	// the next run is not made, and it is what that run returns (mw-gq6.903).
	hostBusy error

	// failed says the first run failed with the host busy, at the reading
	// busy: the next run is the second, made once it calms, and only its
	// failure counts.
	failed bool
	busy   LoadReading
	// first is the first run, whose output names the tests that failed.
	first Checked
}

// what is the next run, as what it says names it.
func (m mergedGate) what() string {
	if m.failed {
		return onMerged + " again"
	}
	return onMerged
}

// mergedResultGate runs the rig's tests on a merged result in dir as gate runs
// them in a worktree, but it never waits for this host where the merge is: the
// landing in dir was cut from a target that moves while a gate queues, and the
// other host's close-out of the rig would wait for this host's whole queue of
// gates, any rig's, and for its calm, if the close-out held the rig's merge
// slot across hosts (mw-gq6.422). When the gate lock is another gate's, or the
// host is busy before a run, it runs nothing and returns what it would wait
// for; merge gives the slot back if it holds it, waits (waitForTheMergedGate),
// takes it again, and tries again from a fresh fetch, so the merged result
// that is tested is the target as it is after the wait (mw-gq6.872).
func (n Next) mergedResultGate(ctx context.Context, c *closeOut, report *NextReport, dir string, span RelatedSpan) (gated, string) {
	m := &c.merged
	if waits := n.mergedGateWaits(ctx, c, report); waits != "" {
		return gated{}, waits
	}
	if m.hostBusy != nil {
		over := m.hostBusy
		m.hostBusy, m.calmed, m.failed = nil, false, false
		return gated{Err: over}, ""
	}

	if m.failed {
		again, err := n.timedChecks(ctx, c, report, dir, span)
		c.gate.settle(again, err, m.busy)
		m.calmed, m.failed = false, false
		if over := n.failedFarOver(ctx, onMerged, again, err); over != nil {
			return gated{Err: over}, ""
		}
		said := ranTwice(onMerged, n.Host, m.busy, m.first, again, err)
		report.Notes = append(report.Notes, said)
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
		if after, ok := n.readLoad(ctx); ok && after.Busy() {
			busy = after
		}
	}
	if !busy.FarOver() {
		c.gate.settle(checked, nil, busy)
		if busy.Busy() {
			n.noteCountedAtOnce(report, onMerged, busy)
		}
		return gated{Checked: checked}, ""
	}
	n.print(fmt.Sprintf("  retry   %s failed while %s was at load %.1f of %d cores: running them once more when it calms\n",
		onMerged, n.Host, busy.Load, busy.Cores))
	m.failed, m.busy, m.first = true, busy, checked
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
		m.before, m.gaveUp, m.hostBusy = n.calmed(ctx, report, c, m.what(), false, false)
		m.calmed = true
	}
}

// yieldSlot gives the rig's merge slot up for a wait of the close-out's own, when
// it was set to (SlotYield); the function returned takes the slot back.
func (n Next) yieldSlot(ctx context.Context) func() {
	if n.SlotYield == nil {
		return func() {}
	}
	return n.SlotYield(ctx)
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
// count first, and does not run them at all if the wait ends with the host far
// over its cores (HostBusyError). A run that fails with the host far over its
// cores at a reading it took — before the run, or after it — is run once more
// when the host has calmed, and only a second failure counts; one that fails with
// the host between its cores and twice them counts at once, as a rerun there has
// not passed (GateFarOver). A second run that fails with the host far over again
// after it is the host's, not the story's (HostBusyError). A run that fails on a
// calm host counts at once. A command that would not start is not a load fault
// and is never run again.
func (n Next) gateOnACalmHost(ctx context.Context, c *closeOut, report *NextReport, dir, what string, span RelatedSpan) gated {
	return n.gateOnACalmHostHolding(ctx, c, report, dir, what, span, nil)
}

// gateOnACalmHostHolding is gateOnACalmHost for a gate that holds the host's gate
// lock, lock, and may be told by Next.AfterCalm, once the wait for the host is
// over, that it is too late to start (mw-gq6.945).
func (n Next) gateOnACalmHostHolding(ctx context.Context, c *closeOut, report *NextReport, dir, what string, span RelatedSpan, lock Holding) gated {
	before, gaveUp, handedBack := n.calmed(ctx, report, c, what, true, false)
	if handedBack != nil {
		return gated{Err: handedBack}
	}
	if lock != nil && n.AfterCalm != nil {
		if err := n.AfterCalm(lock); err != nil {
			return gated{Err: err}
		}
	}
	if err := n.gateGivesWay(ctx, c); err != nil {
		return gated{Err: err}
	}
	n.beginGate(ctx, c, before)
	checked, err := n.timedChecks(ctx, c, report, dir, span)
	if err != nil || checked.NotRun || checked.Passed {
		c.gate.settle(checked, err, before)
		return gated{Checked: checked, Err: err}
	}

	busy := before
	if !gaveUp {
		after, ok := n.readLoad(ctx)
		if ok && after.Busy() {
			busy = after
		}
	}
	if !busy.FarOver() {
		c.gate.settle(checked, nil, busy)
		if busy.Busy() {
			n.noteCountedAtOnce(report, what, busy)
		}
		return gated{Checked: checked}
	}
	n.print(fmt.Sprintf("  retry   %s failed while %s was at load %.1f of %d cores: running them once more when it calms\n",
		what, n.Host, busy.Load, busy.Cores))
	if _, _, err := n.calmed(ctx, report, c, what+" again", true, true); err != nil {
		return gated{Err: err}
	}
	if err := n.gateGivesWay(ctx, c); err != nil {
		return gated{Err: err}
	}
	again, err := n.timedChecks(ctx, c, report, dir, span)
	c.gate.settle(again, err, busy)
	if over := n.failedFarOver(ctx, what, again, err); over != nil {
		return gated{Err: over}
	}
	said := ranTwice(what, n.Host, busy, checked, again, err)
	report.Notes = append(report.Notes, said)
	return gated{Checked: again, Err: err, Said: said}
}

// failedFarOver says a run that was made, and failed, ended with the host far
// over its cores: the failure is the host's, and the HostBusyError says so. It
// is nil for a run that passed, did not start, or ended with the host not far
// over, or whose load cannot be read.
func (n Next) failedFarOver(ctx context.Context, what string, run Checked, err error) error {
	if err != nil || run.NotRun || run.Passed {
		return nil
	}
	if after, ok := n.readLoad(ctx); ok && after.FarOver() {
		return HostBusyError{Host: n.Host, Reading: after, What: what, After: true}
	}
	return nil
}

// noteCountedAtOnce says a failure read with the host busy, but not far over its
// cores, counts at once and is not run again.
func (n Next) noteCountedAtOnce(report *NextReport, what string, busy LoadReading) {
	note := fmt.Sprintf("%s failed while %s was at load %.1f of %d cores, under %.0f times them, so the failure counts and they were not run again",
		what, n.Host, busy.Load, busy.Cores, GateFarOver)
	n.print("  note    " + note + "\n")
	report.Notes = append(report.Notes, note)
}

// waitToRunAgain is what every run of tests that failed is dealt with first, the
// one place both a part of a check (Check.runPartAgain) and a nightly timing run
// (NightlyTiming.runRig) ask whether the failure is the host's (mw-gq6.1063): the
// load read after the failed run is taken, the busier of it and before standing
// as the load the run failed at. A failure read with the host far over its cores
// (again is true) says it will be run once more, and waits, as every run again
// does (Next.calmed, with handBack as the caller asks for it), for the host to
// calm; a failure read between its cores and twice them is counted at once and a
// calm one is not remarked on. it is how the line names the tests ("it", "them").
// The error is the wait's: a HostBusyError for a host still far over its cores at
// the bound, or the caller's own hand-back. A Next with no load to read never
// asks for a run again.
func (n Next) waitToRunAgain(ctx context.Context, c *closeOut, report *NextReport, what, it string, before LoadReading, handBack bool) (busy LoadReading, again bool, err error) {
	busy = before
	reading, ok := n.readLoad(ctx)
	if !ok && n.Load == nil {
		return busy, false, nil
	}
	if ok && reading.Busy() {
		busy = reading
	}
	if !busy.FarOver() {
		if busy.Busy() {
			n.noteCountedAtOnce(report, what, busy)
		}
		return busy, false, nil
	}
	n.print(fmt.Sprintf("  retry   %s failed while %s was at load %.1f of %d cores: running %s once more when it calms\n",
		what, n.Host, busy.Load, busy.Cores, it))
	if _, _, err := n.calmed(ctx, report, c, what+" again", handBack, true); err != nil {
		return busy, true, err
	}
	return busy, true, nil
}

// ranTwice says how a gate that was run twice went. A first run whose tests
// passed on the second is a flake, and the sentence names the tests the first
// run failed, as the output of that run is not kept (mw-gq6.699).
func ranTwice(what, host string, busy LoadReading, first, again Checked, err error) string {
	said := fmt.Sprintf("%s ran twice: the first run failed while %s was at load %.1f of %d cores, the second run %s",
		what, host, busy.Load, busy.Cores, secondRun(again, err))
	if err == nil && !again.NotRun && again.Passed {
		flaked := FlakedTests(first.Output)
		if flaked == "" {
			flaked = "the first run's output named no failing test"
		}
		said += ". Flaked: " + flaked
	}
	return said
}

// flakedListed is the most flaked tests a sentence names.
const flakedListed = 5

// goTestFile is a line of go test's output that names a file of a test, as
// `    a_test.go:9: message` does.
var goTestFile = regexp.MustCompile(`^\s+(\S+_test\.go):\d+`)

// FlakedTests is the tests a run's output says failed as `<file>:<case>`, the
// ones the Mayor files or folds a flake by: go test's `--- FAIL: TestX` with
// the file of the first line printed under it, vitest's `FAIL file > test` as
// `file:test`; a failure whose file is not told is the case alone. A go test
// whose subtest failed too is named by the subtest. They are joined by "; ",
// at most flakedListed of them, and empty when the output names none.
func FlakedTests(output string) string {
	lines := strings.Split(strings.ReplaceAll(domain.StripANSI(output), "\r\n", "\n"), "\n")
	failing, _ := FailingTests(output, 1<<30)
	var named []string
	for _, line := range failing {
		switch {
		case strings.HasPrefix(line, "--- FAIL: "):
			named = append(named, strings.Fields(strings.TrimPrefix(line, "--- FAIL: "))[0])
		case strings.HasPrefix(line, "FAIL "):
			file, test, ok := strings.Cut(strings.TrimPrefix(line, "FAIL "), " > ")
			if ok {
				named = append(named, file+":"+test)
			} else {
				named = append(named, file)
			}
		default:
			named = append(named, line)
		}
	}
	var said []string
	for i, name := range named {
		if i+1 < len(named) && strings.HasPrefix(named[i+1], name+"/") {
			continue // the subtest says more than its parent
		}
		if !strings.Contains(name, ":") {
			if file := goTestFileOf(lines, name); file != "" {
				name = file + ":" + name
			}
		}
		said = append(said, name)
	}
	if len(said) > flakedListed {
		said = append(said[:flakedListed], fmt.Sprintf("and %d more", len(said)-flakedListed))
	}
	return strings.Join(said, "; ")
}

// goTestFileOf is the test file the first line under go test's `--- FAIL: name`
// names, empty when none is told before the next failure.
func goTestFileOf(lines []string, name string) string {
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "--- FAIL: "+name+" ") {
			continue
		}
		for _, next := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(next), "--- FAIL: ") {
				break
			}
			if m := goTestFile.FindStringSubmatch(next); m != nil {
				return m[1]
			}
		}
		return ""
	}
	return ""
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

// maxCheckParts is how many parts of a gate a story's check records are looked
// for; a rig's gate is a handful of parts.
const maxCheckParts = 32

// ownCheck is a story whose own check records, the check-<attempt>.log and
// check-<attempt>-part-<n>.log of its run, are read for the tests that failed.
type ownCheck struct {
	id      string
	attempt int
}

// failingTestFile is a line of a run's output that names a failing test by its
// file: vitest's `FAIL <file> > <test>` and the `×` and `✘` lines of its list
// and Playwright's. The file is the first word that looks like a path with an
// extension; go test's `--- FAIL: TestX` names none.
var (
	failingTestFile = regexp.MustCompile(`^\s*(?:FAIL +|× +|✘ +)`)
	testFilePath    = regexp.MustCompile(`[A-Za-z0-9_@.\-]+(?:/[A-Za-z0-9_@.\-]+)*\.[A-Za-z][A-Za-z0-9]*`)
	lineColumn      = regexp.MustCompile(`(?::\d+)+$`)
)

// failingTestFiles is the files a failing run's output names as the file of a
// failing test, each once, in the order it named them (mw-gq6.808).
func failingTestFiles(output string) []string {
	var files []string
	for _, line := range strings.Split(strings.ReplaceAll(domain.StripANSI(WithoutExpectedFailures(output)), "\r\n", "\n"), "\n") {
		if !failingTestFile.MatchString(line) {
			continue
		}
		rest := failingTestFile.ReplaceAllString(line, "")
		for _, word := range strings.Fields(rest) {
			file := lineColumn.ReplaceAllString(word, "")
			if testFilePath.FindString(file) != file || strings.HasPrefix(file, "-") {
				continue
			}
			if !slices.Contains(files, file) {
				files = append(files, file)
			}
			break
		}
	}
	return files
}

// ownChecks are the stories whose own checks the close-out's related run is
// given the failures of: the story itself, or, for a batch, every story it
// merged.
func (c *closeOut) ownChecks() []ownCheck {
	if c.own != nil {
		return c.own
	}
	return []ownCheck{{id: c.id, attempt: c.attempt()}}
}

// failedInOwnChecks are the test files that failed in the newest own check of
// each story the close-out gates (mw-gq6.808), read once: a rig's related-tests
// line selects tests by the files the branch changed, so a test that reads
// source files as text, and a story's own check failed, would otherwise pass
// the gate unseen. A check log that is not there, or cannot be read, names
// none; the record of a gate part is read only when it says it failed.
func (n Next) failedInOwnChecks(ctx context.Context, c *closeOut) []string {
	if c.failedRead || n.Vault == nil {
		return c.failedFiles
	}
	c.failedRead = true
	for _, story := range c.ownChecks() {
		whole, err := n.Vault.ReadRunFile(ctx, story.id, CheckLogFileName(story.attempt))
		if err == nil {
			c.failedFiles = appendNew(c.failedFiles, failingTestFiles(whole)...)
		}
		for part := 1; part <= maxCheckParts; part++ {
			record, err := n.Vault.ReadRunFile(ctx, story.id, CheckPartFileName(story.attempt, part))
			if err != nil {
				continue
			}
			lines := strings.SplitN(record, "\n", 3)
			if len(lines) < 3 || strings.TrimSpace(lines[1]) != partFailed {
				continue
			}
			c.failedFiles = appendNew(c.failedFiles, failingTestFiles(lines[2])...)
		}
	}
	return c.failedFiles
}
