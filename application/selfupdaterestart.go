package application

import (
	"context"
	"fmt"
	"time"
)

// A tick does not wait for a model run (mw-gq6.1388). The restart of the
// follower a landing asks for waits for its model runs to end, for at most
// AgentRunWaitLimit; run inside a tick, that wait held the dispatch pass (and
// the Millhand's look) for up to half an hour. The self-update therefore reads
// the marks once: with a run under way it restarts nothing of the follower,
// keeps the time it was asked in a mark, says so, and returns. Each later
// tick's self-update looks again, restarts the follower once the runs are gone,
// and at the limit restarts it anyway, saying which run it cut.

// runAwareRestarter is the restarter that can say which model runs are under
// way and restart a unit without waiting for them (RunGuardedRestarter).
type runAwareRestarter interface {
	UnitRestarter
	WaitLimit() time.Duration
	LiveRuns(ctx context.Context) ([]AgentRun, error)
	RestartCutting(ctx context.Context, unit string, live []AgentRun, since time.Time) (bool, error)
}

// restartPendingMark is the key, in Built, under which a host remembers when the
// restart of unit was asked for and left to a later tick.
func restartPendingMark(unit string) string { return unit + ".restart-pending" }

// restartFactoryUnits restarts the factory's units on the build just made. The
// follower is left to a later tick while a model run of its is under way.
func (s SelfUpdate) restartFactoryUnits(ctx context.Context) []string {
	guard, ok := s.Units.(runAwareRestarter)
	if !ok || s.Built == nil {
		said, _ := RestartFactoryUnits(ctx, s.Units, FactoryRig)
		return said
	}
	live, err := guard.LiveRuns(ctx)
	if err != nil || len(live) == 0 {
		// None under way, or none that can be read: the guarded restart reads them
		// itself and goes ahead at once.
		said, _ := RestartFactoryUnits(ctx, s.Units, FactoryRig)
		return said
	}
	notes, _ := restartFactoryUnits(ctx, s.Units, FollowerUnit, s.Units.TryRestart)
	if held, _ := s.Built.Built(ctx, restartPendingMark(FollowerUnit)); held != "" {
		return notes // asked for already, by an earlier build
	}
	said := fmt.Sprintf("%s is left to a later tick to restart on the new build: %s under way, waited for at most %s",
		FollowerUnit, jobNames(live), guard.WaitLimit())
	if err := s.Built.MarkBuilt(ctx, restartPendingMark(FollowerUnit), s.now().UTC().Format(time.RFC3339)); err != nil {
		said += " (it could not be remembered, so the follower is not restarted until a landing does it: " + firstLine(err.Error()) + ")"
	}
	return append(notes, said)
}

// finishPendingRestart is a tick's look at a restart left to it: it restarts the
// follower when no model run is under way, or when the limit has passed since the
// restart was asked for, and says nothing while the runs go on.
func (s SelfUpdate) finishPendingRestart(ctx context.Context) []string {
	guard, ok := s.Units.(runAwareRestarter)
	if !ok || s.Built == nil {
		return nil
	}
	held, _ := s.Built.Built(ctx, restartPendingMark(FollowerUnit))
	if held == "" {
		return nil
	}
	since, err := time.Parse(time.RFC3339, held)
	if err != nil {
		since = s.now()
	}
	live, err := guard.LiveRuns(ctx)
	if err == nil && len(live) > 0 && s.now().Sub(since) < guard.WaitLimit() {
		return nil
	}
	if err := s.Built.MarkBuilt(ctx, restartPendingMark(FollowerUnit), ""); err != nil {
		return []string{"the restart left to a later tick could not be taken off the list, so the follower is not restarted again: " + firstLine(err.Error())}
	}
	try := func(ctx context.Context, unit string) (bool, error) {
		return guard.RestartCutting(ctx, unit, live, since)
	}
	notes, _ := restartFactoryUnits(ctx, s.Units, "", try)
	return notes
}
