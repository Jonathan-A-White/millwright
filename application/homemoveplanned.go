package application

import (
	"context"
	"fmt"
	"strings"
)

// The planned path of mw home move: both hosts are up, and the old home hands
// off, flushes and stands down before the new one takes over. Steps 2 to 6 of the
// move for a dead home follow it unchanged, except that beads come from what the
// old home just pushed, and the mail to the new Mayor says so.

// plannedHandoffSubject is the mail that tells the old home's Mayor to hand off.
func (r *homeMoveRun) plannedHandoffSubject() string {
	return "Hand off now: the home moves to " + r.m.Target
}

// plannedSteps is a dead home's steps as a planned move takes them: the old home
// must answer, then it stands down, then the rest as they were.
func (r *homeMoveRun) plannedSteps(dead []homeMoveStep) []homeMoveStep {
	m := r.m
	rest := dead[1:]
	rest[0].plan = append(rest[0].plan,
		fmt.Sprintf("planned move: what GitHub holds is what %s pushed in the step before, so nothing is lost.", r.old))
	rest[3].plan[0] = "sends mail to the Mayor: 'Home moved to " + m.Target + " at <time>: planned move; " + r.old + " handed off and flushed first'."
	rest[3].plan[1] = "runs the vault's bin/mayor-up, which starts a Mayor here from the handoff the old Mayor wrote and pushed (it refuses on a host that is not home: step 4 made this one home)."
	rest[4].plan = []string{"prints how old GitHub's beads backup is and how old the Postern data here is; both are the old home's final push and mirror."}

	first := homeMoveStep{
		title: "the old home",
		plan: []string{
			fmt.Sprintf("asks whether %s answers `%s` within %s. A planned move needs it up.", r.old, strings.Join(r.prefix, " "), HomeMoveAnswerWait),
			"If it does not, the move stops, naming --old-home-dead for a host that is dead.",
		},
		back: "nothing is changed.",
		run:  (*homeMoveRun).plannedOldHome,
	}
	stand := homeMoveStep{
		title: "the old home stands down",
		plan: []string{
			fmt.Sprintf("over ssh, on %s: mails its Mayor '%s' and waits up to %s for .mayor-acting there to be empty or its Mayor process gone. If it does not go, the move stops: a Mayor is never killed.", r.old, r.plannedHandoffSubject(), HomeMoveHandoffWait),
			fmt.Sprintf("runs a final mw sync there (a backup push of the beads and the vault), stops its %s user unit, then runs a final mw postern mirror to this host, and takes bd count there: the number the new home's database must answer with.", PosternBackendUnit),
			fmt.Sprintf("stops its %s user unit, so nothing there writes to beads: once the home changes, beads_sync = auto makes it a boost.", DoltBeadsUnit),
		},
		back: fmt.Sprintf("mail its Mayor to carry on, and start the %s and %s units there again.", PosternBackendUnit, DoltBeadsUnit),
		run:  (*homeMoveRun).standDown,
	}
	return append([]homeMoveStep{first, stand}, rest...)
}

// plannedOldHome asks whether the old home answers; a planned move goes on only
// if it does.
func (r *homeMoveRun) plannedOldHome(ctx context.Context) error {
	m := r.m
	r.say("asking whether %s answers `%s` (%s)", r.old, strings.Join(r.prefix, " "), HomeMoveAnswerWait)
	up, err := m.Machine.OldHomeAnswers(ctx, r.prefix, HomeMoveAnswerWait)
	if err != nil {
		return fmt.Errorf("asking whether %s answers: %w", r.old, err)
	}
	if !up {
		return fmt.Errorf("--planned needs the old home up, and %s did not answer `%s` within %s. If it is dead, say so: mw home move %s --old-home-dead",
			r.old, strings.Join(r.prefix, " "), HomeMoveAnswerWait, m.Target)
	}
	r.say("%s answers; planned move.", r.old)
	return nil
}

// standDown is the old home's part of a planned move, in the order that loses
// nothing: the Mayor hands off, the last writes are pushed, the writers stop, and
// the last of the Postern data is copied.
func (r *homeMoveRun) standDown(ctx context.Context) error {
	m, old, ssh := r.m, r.old, r.prefix

	body := fmt.Sprintf("%s. Write your handoff, mw sync, clear .mayor-acting and stop: mw home move is waiting up to %s. The new home is %s.",
		r.plannedHandoffSubject(), HomeMoveHandoffWait, m.Target)
	if err := m.Old.MailMayor(ctx, ssh, m.Actor, r.plannedHandoffSubject(), body); err != nil {
		return fmt.Errorf("mailing the Mayor on %s: %w", old, err)
	}
	r.say("mailed the Mayor on %s: %s", old, r.plannedHandoffSubject())
	r.undo(fmt.Sprintf("the hand-off mail is sent and stays: tell the Mayor on %s to carry on (`%s` then `mw mail send mayor`).", old, strings.Join(ssh, " ")))

	r.say("waiting up to %s for the Mayor on %s to hand off.", HomeMoveHandoffWait, old)
	gone, said, err := m.Old.MayorGone(ctx, ssh, HomeMoveHandoffWait)
	if err != nil {
		return fmt.Errorf("waiting for the Mayor on %s to hand off: %w", old, err)
	}
	if !gone {
		return fmt.Errorf("the Mayor on %s did not hand off within %s (%s): it is left running, never killed. Ask it to hand off, or stop it yourself, then run the move again", old, HomeMoveHandoffWait, said)
	}
	r.say("the Mayor on %s has handed off (%s).", old, said)

	if err := m.Old.Sync(ctx, ssh); err != nil {
		return fmt.Errorf("the final mw sync on %s: %w", old, err)
	}
	r.say("ran the final mw sync on %s.", old)

	if err := r.stopOld(ctx, PosternBackendUnit); err != nil {
		return err
	}
	if err := m.Old.Mirror(ctx, ssh); err != nil {
		return fmt.Errorf("the final mw postern mirror on %s: %w", old, err)
	}
	r.say("ran the final mw postern mirror on %s: its data is copied here.", old)

	// The count is taken after the final flush and before the server stops: it is
	// what the new home's database has to answer with.
	count, err := m.Old.OldBeadsCount(ctx, ssh)
	if err != nil {
		return fmt.Errorf("counting the beads on %s: %w", old, err)
	}
	r.oldCount, r.haveOldCount = count, true
	r.say("%s counts %d beads after its final flush: the new home's database has to answer with as many.", old, count)

	return r.stopOld(ctx, DoltBeadsUnit)
}

// stopOld stops a user unit on the old home, if it has one, noting how to start
// it again.
func (r *homeMoveRun) stopOld(ctx context.Context, unit string) error {
	m, old := r.m, r.old
	installed, err := m.Old.OldUnitInstalled(ctx, r.prefix, unit)
	if err != nil {
		return fmt.Errorf("asking whether the %s unit is installed on %s: %w", unit, old, err)
	}
	if !installed {
		r.say("no %s unit on %s: nothing to stop.", unit, old)
		return nil
	}
	stopped, err := m.Old.OldStopUnit(ctx, r.prefix, unit)
	if err != nil {
		return fmt.Errorf("stopping the %s unit on %s: %w", unit, old, err)
	}
	if !stopped {
		r.say("the %s unit on %s was not running.", unit, old)
		return nil
	}
	r.say("stopped the %s user unit on %s.", unit, old)
	r.undo(fmt.Sprintf("%s systemctl --user start %s", strings.Join(r.prefix, " "), unit))
	return nil
}
