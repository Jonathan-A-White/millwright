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
// than the narrowing, and lampas's related run was its whole vitest anyway. A
// close-out's gate of a single story takes turns the same way (Next.gate,
// mw-gq6.1301); the gate on a merged result keeps the lock it holds across the
// merge (mergedResultGate) and runs the rig's gate whole.
//
// A batch no longer takes turns (mw-gq6.1450): it takes precedence over single
// stories' checks and close-outs in every queue it waits in on the host
// (WithQueuePrecedence), so nobody it would give the lock to between its parts may
// go ahead of it, and lampas's batch 555, which gave it back after each part and
// queued behind single stories' close-outs, took hours. A gate whose waits take
// precedence keeps the lock across its parts, and still gives it back for a wait
// for a calm host (gatecalm.go), asking for it again at the head of the queue.

// gateTurns is the host's gate lock as a gate that takes turns holds it: what it
// holds now, nil once the lock could not be taken again, and how long the turns
// waited for other gates, which is no time of its own gate's.
type gateTurns struct {
	lock   Holding
	waited time.Duration
	// whole says the gate runs the rig's tests whole, and holds the lock this way
	// only to give it back for a wait for a calm host (holdingWhole); kept says the
	// turns waited past the limit, and the lock is kept for the remaining parts.
	whole, kept bool
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
// unless the close-out holds the lock by turns (a batch's gate or a story's) and the rig's gate
// is named as more than one part on the host.
func (n Next) turnParts(c *closeOut) []GatePart {
	if c.turns == nil || c.turns.whole || n.GateParts == nil {
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
// (takeTurn) until the turns have waited past the limit (keepsTheLock), and a host
// that gives way starts no more of them. The Checked is the
// run as a whole: its command the parts that ran, joined as one line joins its
// steps, and its output theirs in turn.
func (n Next) partsTakingTurns(ctx context.Context, c *closeOut, report *NextReport, dir string, parts []GatePart) (Checked, error) {
	var commands []string
	var output strings.Builder
	ahead := n.keepsTheLockAhead(ctx, c, parts)
	// last is the part that ran before this one, -1 when none has: a part that
	// passed at the tip (c.parts) is not run, and the turns are between the parts
	// that are (nextparts.go).
	last := -1
	for i, part := range parts {
		if c.parts.skip(dir == c.worktree, i+1) {
			continue
		}
		if last >= 0 && !ahead && !n.keepsTheLock(c, i, parts) {
			n.takeTurn(ctx, c, report, last, i, parts)
		}
		if last >= 0 {
			if err := n.gateGivesWay(ctx, c); err != nil {
				return Checked{}, err
			}
		}
		last = i
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

// keepsTheLockAhead says whether c's gate keeps the lock across all its parts
// because its waits take precedence over everyone else's (a batch's, mw-gq6.1450),
// and says so once when it does.
func (n Next) keepsTheLockAhead(ctx context.Context, c *closeOut, parts []GatePart) bool {
	if !HasQueuePrecedence(ctx) {
		return false
	}
	n.print(fmt.Sprintf("  turn    %s keeps %s's gate lock across its %d parts: a batch goes ahead of single stories' checks and close-outs\n",
		c.id, n.Host, len(parts)))
	return true
}

// takeTurn gives the host's gate lock back after part last of parts (counted from
// zero) and takes it again behind whoever queued for it meanwhile, before part
// next, and says so. A lock that cannot be taken again is said, and the rest run
// without it, as a close-out's gate does.
func (n Next) takeTurn(ctx context.Context, c *closeOut, report *NextReport, last, next int, parts []GatePart) {
	turns := c.turns
	if turns.lock != nil {
		n.giveTheGateLockBack(report, turns.lock)
		turns.lock = nil
	}
	n.print(fmt.Sprintf("  turn    %s gives %s's gate lock back after part %d of %d (%s), and takes it again behind whoever waits for it\n",
		c.id, n.Host, last+1, len(parts), parts[last].Name))
	began := n.now()
	turns.lock, _ = n.takeTheGateLock(ctx, c, report, fmt.Sprintf("part %d of %d of the rig's gate (%s)", next+1, len(parts), parts[next].Name))
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

// DefaultGateTurnsLimit is how long the turns of one gate may wait for other
// gates in all before it keeps the lock for its remaining parts (mw-gq6.1420):
// lampas's batch 551 waited 14m22s, 36m42s, 26m49s and 15m2s before four of its
// 11 parts, and would have waited so before each. It is provisional: the config
// file's gate_turns_limit_minutes changes it.
const DefaultGateTurnsLimit = 30 * time.Minute

func (n Next) gateTurnsLimit() time.Duration {
	if n.GateTurnsLimit <= 0 {
		return DefaultGateTurnsLimit
	}
	return n.GateTurnsLimit
}

// keepsTheLock says whether c's gate keeps the lock after part done of parts in
// place of taking a turn: once its turns have waited for other gates longer in
// all than the limit, it keeps it for every part left, and says so on one line
// (mw-gq6.1420). A gate's turns are then bounded by the limit and one round of
// the gates queued behind it.
func (n Next) keepsTheLock(c *closeOut, done int, parts []GatePart) bool {
	turns := c.turns
	if turns.kept {
		return true
	}
	limit := n.gateTurnsLimit()
	if turns.waited <= limit {
		return false
	}
	turns.kept = true
	n.print(fmt.Sprintf("  turn    %s's turns have waited %s for other gates, over the limit of %s: it keeps %s's gate lock for its remaining %d of %d parts\n",
		c.id, gateWaited(turns.waited), gateWaited(limit), n.Host, len(parts)-done, len(parts)))
	return true
}
