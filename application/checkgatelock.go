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
// budget, partSlotWait: under the 600 seconds (the default cap) a Builder's foreground call is
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
	// left is what the check's one deadline has left (the closure Check.check makes).
	left func() time.Duration
	// fit is whether the rig's gate fits what is left of the Builder's call, nil
	// when the check does not ask (mw-gq6.940).
	fit *callFit
}

// Take implements GateLock. A wait that ran out is a GateLockHandBack, which
// ends the gate in one line; the holder name it is given is the check's own.
func (g checkGateLock) Take(ctx context.Context, _ string, held func(by string)) (GateTurn, error) {
	return g.k.takeForTests(ctx, g.c, wholeTry(g.c, "the rig's tests", CheckWholeHeldFileName(g.attempt)), g.left(), g.fit, held)
}

// takeForTests takes what a run of the rig's tests holds in a worktree check:
// the rig's merge slot, when it has one, shared with the other checks, and then
// the host's gate lock, when it has one. The wait for both is no longer than
// within, which is what the check's one deadline has left: none at all when the
// tests before these have spent it, and then each is tried once and the check is
// handed back if either is held (mw-gq6.918). held is told who has the gate lock
// the first time it is found held. The holdings are given back together.
//
// fit, when it is not nil, is asked once the gate lock is won: a gate that would
// not end inside what is left of the Builder's call is not started, the lock is
// given back and the check's place in its queue is kept, with one line saying so
// (mw-gq6.940); a gate that does start says how much of the call is left.
func (k Check) takeForTests(ctx context.Context, c *closeOut, try slotTry, within time.Duration, fit *callFit, held func(by string)) (GateTurn, error) {
	began := k.callNow()
	var slot Holding
	if k.Slot != nil {
		taken, err := k.takeSlotFor(ctx, c, try, within)
		if err != nil {
			return GateTurn{}, GateLockHandBack{Why: err.Error()}
		}
		slot = taken
	}
	if k.GateLock == nil {
		return GateTurn{Holding: bothHoldings{slot: slot}}, nil
	}

	waitFor := max(within-k.callNow().Sub(began), 0)
	waitCtx, cancel := k.boundByClock(ctx, waitFor)
	defer cancel()
	waitCtx, ask := k.askToKeep(waitCtx, c)
	if ask != nil && fit != nil {
		ask.Late = fit.late
	}
	notices := &gateNotices{k: k, ctx: waitCtx}
	defer notices.stop()
	holder := k.checkHolder(c)
	turn, err := k.GateLock.Take(waitCtx, holder, func(by string) {
		if k.Out != nil {
			fmt.Fprintf(k.Out, "  wait    %s\n", k.heldLine(try, by, waitFor))
		}
		if held != nil {
			held(by)
		}
		notices.start(by)
	})
	if err != nil {
		if slot != nil {
			slot.Release(context.Background())
		}
		if errors.Is(err, ErrPlaceKeptLate) {
			return GateTurn{}, GateLockHandBack{Why: k.tooLateLine(c.id, fit, ask)}
		}
		return GateTurn{}, k.gateLockRefusal(ctx, waitCtx, c, try, notices.by, k.callNow().Sub(began), err, ask)
	}
	if fit != nil && k.Out != nil {
		fmt.Fprintf(k.Out, "  gate    expected about %s, %s left in this call\n", gateWaited(fit.expected), gateWaited(fit.left()))
	}
	return GateTurn{Holding: bothHoldings{slot: slot, lock: turn.Holding}, Waited: turn.Waited, For: turn.For}, nil
}

// heldLine is the one line a check says when it finds the host's gate lock held
// (mw-gq6.1275): whose gate holds it, a batch named with its rig, since when and
// for how long so far, that a batch of a rig whose gate is named as parts gives
// the lock back between them (gateturns.go), and what the check does next.
func (k Check) heldLine(try slotTry, by string, waitFor time.Duration) string {
	holder, since := k.gateHolderNow(by, k.callNow())
	line := fmt.Sprintf("%s's gate lock is held by %s, since %s (%s ago)",
		k.Host, holder, since.UTC().Format("15:04:05Z"), gateWaited(k.callNow().Sub(since)))
	if rig, ok := batchRigOf(holder); ok && k.Parts != nil {
		if parts := len(k.Parts.Parts(rig)); parts > 1 {
			line += fmt.Sprintf("; the batch's gate gives the lock back between its %d parts", parts)
		}
	}
	return line + fmt.Sprintf(": this check waits for its turn for up to %s and then runs %s, or ends in one line saying who still holds the lock, having run nothing",
		gateWaited(waitFor), try.label)
}

// callFit is whether the rig's gate fits what is left of a Builder's foreground
// call: the call began at began, may spend budget of it, and the gate usually
// takes expected on this host (mw-gq6.940).
type callFit struct {
	began    time.Time
	budget   time.Duration
	expected time.Duration
	// now is the clock the fit is read by, the real one when it is nil.
	now func() time.Time
}

// callNow is the time the call is said to begin at, by Check.CallNow.
func (k Check) callNow() time.Time {
	if k.CallNow != nil {
		return k.CallNow()
	}
	return time.Now() //mw:realclock the real clock a Check with no CallNow reads; every bound reads callNow
}

// callFitOf is the fit of the rig's gate to the call this check began, nil when
// the check is not asked to keep to a call (no CallBudget) or no figure for the
// rig's gate is known.
func (k Check) callFitOf(ctx context.Context, rig string, began time.Time) *callFit {
	if k.CallBudget <= 0 || k.GateExpected == nil {
		return nil
	}
	expected := k.GateExpected(ctx, rig)
	if expected <= 0 {
		return nil
	}
	return &callFit{began: began, budget: k.CallBudget, expected: expected, now: k.CallNow}
}

// left is what is left of the call's budget.
func (f *callFit) left() time.Duration {
	now := time.Now
	if f.now != nil {
		now = f.now
	}
	return max(f.budget-now().Sub(f.began), 0)
}

// late is whether the gate would not end inside what is left of the call. A gate
// longer than a whole call is never late: no fresh call would fit it either. A
// foreground check that can start a process of its own runs such a gate there
// (Check.GateStarter, checkgatedetached.go, mw-gq6.1273) and never gets here with
// it; one that cannot starts it as it always was.
func (f *callFit) late() bool { return f.expected < f.budget && f.expected > f.left() }

// tooLateLine is the one line a foreground check ends with when it won the gate
// lock too late for the gate to end inside its call and kept its place instead.
func (k Check) tooLateLine(story string, f *callFit, ask *QueuePlace) string {
	_, place, of := ask.Result()
	return k.tooLateAt(story, f, place, of)
}

// backgroundWay is what a too-late line says besides running the same command
// again: a gate whose usual time is just over what a call has left may never fit
// a call, and a check started with --background is not kept to one. It waits
// under the story's key, so it takes up the place kept (mw-gq6.1196, mw-gq6.1197).
func backgroundWay(story string) string {
	return fmt.Sprintf("if it keeps saying so, run `mw check %s --background` instead, which is not kept to a call", story)
}

func (k Check) tooLateAt(story string, f *callFit, place, of int) string {
	line := fmt.Sprintf("%s's gate lock is free, but the gate needs about %s and only %s is left in this call; your place %d of %d in the queue is kept for %s: run the same command again to start it; %s",
		k.Host, gateWaited(f.expected), gateWaited(f.left()), place, of, keptFor(k.KeepPlace), backgroundWay(story))
	k.waiting.set(line)
	return line
}

// afterCalm is the fit asked again once the wait for a calm host is over, with the
// gate lock held: what is left of the call then is what the gate truly has, and a
// calm wait of minutes can have eaten it (mw-gq6.945). A gate that no longer fits
// is not started: the place is put back in the queue and the lock, which the
// caller gives back, ends the gate with the same one line a late win of the lock
// does.
func (g checkGateLock) afterCalm(held Holding) error {
	if g.fit == nil || !g.fit.late() {
		return nil
	}
	if keeper, ok := held.(PlaceKeeper); ok && g.k.KeepPlace > 0 {
		if place, of, kept := keeper.KeepPlace(g.k.KeepPlace); kept {
			return GateLockHandBack{Why: g.k.tooLateAt(g.c.id, g.fit, place, of)}
		}
	}
	line := fmt.Sprintf("the gate needs about %s and only %s is left in this call, after waiting for %s to calm; the gate lock is given back and nothing was run: run the same command again; %s",
		gateWaited(g.fit.expected), gateWaited(g.fit.left()), g.k.Host, backgroundWay(g.c.id))
	g.k.waiting.set(line)
	return GateLockHandBack{Why: line}
}

// tooLatePartLine is the same for a part that won the rig's merge slot too late
// to end inside its call (mw-gq6.944).
func (k Check) tooLatePartLine(try slotTry, rig string, f *callFit, ask *QueuePlace) string {
	_, place, of := ask.Result()
	return fmt.Sprintf("%s did not start: the merge slot of %s is free, but the part needs about %s and only %s is left in this call; your place %d of %d in the queue is kept for %s: run the same command again to start it",
		try.label, rig, gateWaited(f.expected), gateWaited(f.left()), place, of, keptFor(k.KeepPlace))
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
	waiting := g.k.callNow()
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
					g.k.Host, holder, since.UTC().Format("15:04:05Z"), gateWaited(g.k.callNow().Sub(waiting)))
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
func (k Check) gateLockRefusal(ctx, waitCtx context.Context, c *closeOut, try slotTry, by string, waited time.Duration, err error, ask *QueuePlace) error {
	if ctx.Err() != nil || waitCtx.Err() == nil || !errors.Is(err, context.DeadlineExceeded) {
		return GateLockHandBack{Why: fmt.Sprintf("%s's gate lock could not be taken: %v", k.Host, err)}
	}
	holder, since := k.gateHolderNow(by, k.callNow().Add(-waited))
	sinceAt := fmt.Sprintf("%s (%s ago)", since.UTC().Format("15:04:05Z"), k.callNow().Sub(since).Round(time.Second))
	if line, ok := k.placeKept(try, fmt.Sprintf("%s's gate lock", k.Host), holder, sinceAt, ask); ok {
		return GateLockHandBack{Why: line}
	}
	if k.HandBackAtOnce {
		return GateLockHandBack{Why: k.limitReached(try.label, waited, fmt.Sprintf("%s's gate lock is still held by %s, since %s", k.Host, holder, sinceAt), holder)}
	}
	record := strings.TrimSuffix(try.record, ".slot") + "-gate.slot"
	if earlier := k.noteWait(ctx, c.id, record, holder, since); earlier != "" {
		return GateLockHandBack{Why: fmt.Sprintf("%s did not start: after %s %s's gate lock is still held by %s, since %s, and nothing was run; it was held when this %s was tried at %s as well, which is counted once for this attempt and is not a failure of the %s: %s, and if the lock is held again, comment on the story naming this %s and who holds the lock, and hand back",
			try.label, waited.Round(time.Second), k.Host, holder, sinceAt, try.noun, earlier, try.noun, try.again, try.noun)}
	}
	return GateLockHandBack{Why: fmt.Sprintf("%s did not start: after %s %s's gate lock is still held by %s, since %s, and nothing was run; that is the other gate's wait and not a failure of the %s: %s",
		try.label, waited.Round(time.Second), k.Host, holder, sinceAt, try.noun, try.again)}
}

// waitingLine is the one line a foreground check ends with when a wait ran out
// and its place was kept (mw-gq6.939). Every copy of a Check made in one check()
// shares it, as it shares the check's one deadline.
type waitingLine struct {
	mu   sync.Mutex
	line string
}

func (w *waitingLine) set(line string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.line == "" {
		w.line = line
	}
}

func (w *waitingLine) get() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.line
}

// askToKeep asks the queue the check is about to wait in to keep its place when
// the wait runs out (Check.KeepPlace), under a key that is the same on every call
// of the check of this story, so that the check run again takes the same place
// up. It returns the context to wait under and the ask, nil when places are not
// kept.
//
// A check that keeps no place of its own (a background one, or one given --wait)
// still waits under the story's key, with a keep time of none: the queue then
// takes up the place a foreground check of the same story kept, rather than
// queueing behind it, which would be waiting for itself (mw-gq6.1196). What it
// leaves at the end of its wait expires at once.
func (k Check) askToKeep(ctx context.Context, c *closeOut) (context.Context, *QueuePlace) {
	if k.KeepPlace <= 0 {
		return WithQueuePlace(ctx, &QueuePlace{Key: "check " + c.id}), nil
	}
	ask := &QueuePlace{Key: "check " + c.id, For: k.KeepPlace}
	return WithQueuePlace(ctx, ask), ask
}

// placeKept is the line a foreground check ends with when a wait for a queue ran
// out and the queue kept its place: what is still held, by whom and since when,
// where the check stands, how long the place is kept, and that running the same
// command again goes on waiting (mw-gq6.939). ok is false when no place was kept
// (places are not asked for, or the queue kept none), and then the caller ends
// the check as it always has.
func (k Check) placeKept(try slotTry, what, holder, since string, ask *QueuePlace) (line string, ok bool) {
	if ask == nil {
		return "", false
	}
	kept, place, of := ask.Result()
	if !kept {
		return "", false
	}
	line = fmt.Sprintf("%s is still held by %s, since %s; your place %d of %d in the queue is kept for %s: run the same command again to go on waiting",
		what, holder, since, place, of, keptFor(k.KeepPlace))
	if try.noun == "part" {
		line = try.label + " did not start: " + line
	}
	k.waiting.set(line)
	return line, true
}

// keptFor is how long a place is kept, in minutes when it is a whole number.
func keptFor(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		if d == time.Minute {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", d/time.Minute)
	}
	return d.Round(time.Second).String()
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

// PlaceKeeper is what a Holding of a queue may also do: put its holder's place
// in the queue back, kept for the time given, when the holder has won its turn
// and finds it cannot use it (mw-gq6.945). ok is false when no place was kept.
type PlaceKeeper interface {
	KeepPlace(kept time.Duration) (place, of int, ok bool)
}

// KeepPlace implements PlaceKeeper by the gate lock, the queue a check's place is
// kept in.
func (b bothHoldings) KeepPlace(kept time.Duration) (place, of int, ok bool) {
	if keeper, is := b.lock.(PlaceKeeper); is {
		return keeper.KeepPlace(kept)
	}
	return 0, 0, false
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

// limitReached is the one line a check with HandBackAtOnce ends with when a wait
// ran out of its limit (mw-gq6.938): what did not start, that the limit was
// reached, how long it waited, what still held it and who, that nothing was run,
// and that the Builder hands back at once; it is not told to run the check
// again, which would only queue again behind the same holder.
func (k Check) limitReached(label string, waited time.Duration, still, holder string) string {
	return fmt.Sprintf("%s did not start: the wait limit of %s was reached after %s and %s, and nothing was run; that is the holder's wait and not a failure of the check, so do not run it again: comment on the story naming %s and hand back at once",
		label, gateWaited(k.partSlotWait()), gateWaited(waited), still, holder)
}

// clockPoll is how often a wait bounded by the check's clock reads it.
const clockPoll = 5 * time.Millisecond

// boundByClock is the context a wait of at most within is made under. A check
// with no clock of its own (CallNow nil) waits the real time, as a context
// timeout counts it. A check with one (a test, which moves it by hand) gives up
// when that clock has passed within, read by polling, and also when the real time
// has, so a wait is never longer than it was; the context then ends with
// context.DeadlineExceeded, as a timeout's does (mw-gq6.1142).
func (k Check) boundByClock(ctx context.Context, within time.Duration) (context.Context, context.CancelFunc) {
	if k.CallNow == nil {
		return context.WithTimeout(ctx, within)
	}
	b := &clockBound{Context: ctx, done: make(chan struct{})}
	if within <= 0 {
		b.end(context.DeadlineExceeded)
		return b, func() {}
	}
	began := k.CallNow()
	quit := make(chan struct{})
	var once sync.Once
	go func() {
		real := time.NewTimer(within)
		defer real.Stop()
		tick := time.NewTicker(clockPoll)
		defer tick.Stop()
		for {
			select {
			case <-quit:
				b.end(context.Canceled)
				return
			case <-ctx.Done():
				b.end(ctx.Err())
				return
			case <-real.C:
				b.end(context.DeadlineExceeded)
				return
			case <-tick.C:
				if k.CallNow().Sub(began) >= within {
					b.end(context.DeadlineExceeded)
					return
				}
			}
		}
	}()
	return b, func() { once.Do(func() { close(quit) }) }
}

// clockBound is a context that ends when a clock says so, with the error it was
// ended with.
type clockBound struct {
	context.Context
	mu   sync.Mutex
	err  error
	done chan struct{}
}

func (b *clockBound) end(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err == nil {
		b.err = err
		close(b.done)
	}
}

func (b *clockBound) Done() <-chan struct{} { return b.done }

func (b *clockBound) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

func (b *clockBound) Deadline() (time.Time, bool) { return time.Time{}, false }
