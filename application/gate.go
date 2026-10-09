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
// be busy: it waits, bounded, for the load to drop below the core count first;
// and a run that fails while the host was busy at a reading it took — before the
// run, or after it — is run once more when the host has calmed, and only a
// second failure counts. A run that fails on a calm host counts at once. A
// command that would not start is not a load fault and is never run again.
func (n Next) gate(ctx context.Context, c *closeOut, report *NextReport, dir, what string) gated {
	before, gaveUp := n.calmed(ctx, report, c, what)
	checked, err := n.Checks.Run(ctx, c.path.Rig, dir)
	if err != nil || checked.NotRun || checked.Passed {
		return gated{Checked: checked, Err: err}
	}

	busy := before
	if !gaveUp {
		after, ok := n.readLoad(ctx)
		if !ok || !after.Busy() {
			return gated{Checked: checked}
		}
		busy = after
	}
	n.print(fmt.Sprintf("  retry   %s failed while %s was at load %.1f of %d cores: running them once more when it calms\n",
		what, n.Host, busy.Load, busy.Cores))
	n.calmed(ctx, report, c, what+" again")
	again, err := n.Checks.Run(ctx, c.path.Rig, dir)
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
