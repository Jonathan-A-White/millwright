package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// A batch gate holds the host's gate lock for as long as its rig's tests run:
// lampas's took 39 to 46 minutes on the laptop, and every other rig's check and
// gate on the host waited behind it, 40 minutes to 1h27m, past any call of a
// Builder's (mw-gq6.1275). A rig whose gate is named as parts on the host (its
// [test_parts.<rig>] table, each part short enough for a Builder's call) is gated
// by a batch part by part instead: the lock is given back after each part and
// taken again, behind whoever queued for it meanwhile, so no wait behind a batch
// gate is longer than one part of it and the gates of other rigs between. The
// parts are the rig's whole gate, so a batch of such a rig runs them in place of
// its related-tests line too: a wait bounded by a part is worth more to the host
// than the narrowing, and lampas's related run was its whole vitest anyway.

// gateTurns is the host's gate lock as a gate that takes turns holds it: what it
// holds now, nil once the lock could not be taken again, and how long the turns
// waited for other gates, which is no time of its own gate's.
type gateTurns struct {
	lock   Holding
	waited time.Duration
}

// takingTurns gives c the gate lock lock to take turns with until the returned
// function is called, which gives back whatever the turns hold by then. A nil
// lock takes no turns, and the function does nothing.
func (n Next) takingTurns(c *closeOut, report *NextReport, lock Holding) func() {
	if lock == nil {
		return func() {}
	}
	turns := &gateTurns{lock: lock}
	c.turns = turns
	return func() {
		c.turns = nil
		if turns.lock != nil {
			n.giveTheGateLockBack(report, turns.lock)
		}
	}
}

// turnParts is the parts of the rig's gate a close-out takes turns between: none
// unless the close-out holds the lock by turns (a batch's gate) and the rig's gate
// is named as more than one part on the host.
func (n Next) turnParts(c *closeOut) []GatePart {
	if c.turns == nil || n.GateParts == nil {
		return nil
	}
	if parts := n.GateParts.Parts(c.path.Rig); len(parts) > 1 {
		return parts
	}
	return nil
}

// partsTakingTurns runs the parts of the rig's gate in dir one after another, as
// the rig's one line runs its steps: the first that fails, or does not start, ends
// the run. Between two parts the gate lock is given back and taken again
// (takeTurn), and a host that gives way starts no more of them. The Checked is the
// run as a whole: its command the parts that ran, joined as one line joins its
// steps, and its output theirs in turn.
func (n Next) partsTakingTurns(ctx context.Context, c *closeOut, report *NextReport, dir string, parts []GatePart) (Checked, error) {
	var commands []string
	var output strings.Builder
	for i, part := range parts {
		if i > 0 {
			n.takeTurn(ctx, c, report, i, parts)
			if err := n.gateGivesWay(ctx, c); err != nil {
				return Checked{}, err
			}
		}
		run, err := n.GateParts.RunLine(ctx, c.path.Rig, dir, part.Command)
		if err != nil {
			return Checked{}, fmt.Errorf("part %d of %d of the rig's gate (%s) could not be run: %w", i+1, len(parts), part.Name, err)
		}
		commands = append(commands, part.Command)
		output.WriteString(run.Output)
		if run.Output != "" && !strings.HasSuffix(run.Output, "\n") {
			output.WriteString("\n")
		}
		if run.NotRun || !run.Passed {
			return Checked{Command: strings.Join(commands, " && "), NotRun: run.NotRun, Output: output.String()}, nil
		}
	}
	return Checked{Command: strings.Join(commands, " && "), Passed: true, Output: output.String()}, nil
}

// takeTurn gives the host's gate lock back after part done of parts (counted from
// zero, the part that ran last being done-1) and takes it again behind whoever
// queued for it meanwhile, and says so. A lock that cannot be taken again is said,
// and the rest run without it, as a close-out's gate does.
func (n Next) takeTurn(ctx context.Context, c *closeOut, report *NextReport, done int, parts []GatePart) {
	turns := c.turns
	if turns.lock != nil {
		n.giveTheGateLockBack(report, turns.lock)
		turns.lock = nil
	}
	n.print(fmt.Sprintf("  turn    %s gives %s's gate lock back after part %d of %d (%s), and takes it again behind whoever waits for it\n",
		c.id, n.Host, done, len(parts), parts[done-1].Name))
	began := n.now()
	turns.lock, _ = n.takeTheGateLock(ctx, c, report, fmt.Sprintf("part %d of %d of the rig's gate (%s)", done+1, len(parts), parts[done].Name))
	turns.waited += n.now().Sub(began)
}

// turnsWaited is how long the turns of c's gate have waited for other gates so far.
func (c *closeOut) turnsWaited() time.Duration {
	if c.turns == nil {
		return 0
	}
	return c.turns.waited
}

// batchRigOf is the rig of a gate lock's holder that is a batch ("batch 526 of
// lampas", BatchLabel); ok is false for any other holder.
func batchRigOf(holder string) (rig string, ok bool) {
	if !strings.HasPrefix(holder, "batch ") {
		return "", false
	}
	_, rig, ok = strings.Cut(holder, " of ")
	return rig, ok && rig != ""
}
