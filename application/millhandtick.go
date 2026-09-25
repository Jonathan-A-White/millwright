package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// TickLogLines is how many lines of the tick log are kept: the log is appended
// to every 15 minutes for ever, and it is the one thing that grows.
const TickLogLines = 500

// TickReasonLimit is how many mail subjects and how many stuck story titles a
// wake's reason names; the rest are counted.
const TickReasonLimit = 5

// ResumeGap is how much longer than the timer that runs mw millhand tick
// (every 15 minutes) a gap since the tick log's newest line must be before a
// tick calls it a resume — the host slept, rather than merely run its
// ordinary turn.
const ResumeGap = 30 * time.Minute

// GraceAfterResume is how long, after a resume or a local network fault, a
// tick defers doctor notes and the health verdict rather than waking the
// Millhand for them: room enough for a network that is only settling back
// in — DNS, a tunnel restarting itself — to clear on its own before it is
// treated as real trouble.
const GraceAfterResume = 3 * time.Minute

// TickLocalNetworkFault is what a Millhand tick's line says when Reach
// cannot reach any of its targets at all: this host's own network, not real
// trouble, found without needing a [watch] table at all. It is the same
// words mw dispatch's own LocalNetworkFault prints, for the same kind of
// finding, under a name of its own so the two are never confused as one type.
const TickLocalNetworkFault = "local network fault"

// Reach is whether the internet looks reachable at all from this host — the
// same TCP-dial test mw doctor's wifi and tunnel checks make — asked here to
// tell a local network fault (every target unreachable) from real trouble
// worth a wake. A nil Reach is never asked, so a tick with none wired takes
// no grace on its account.
type Reach interface {
	Reachable(ctx context.Context) bool
}

// TickLog is the log a host keeps of what a timer's runs found: one line each,
// appended, and never more than TickLogLines of them — an adapter drops the
// oldest as it appends. mw millhand tick keeps one and mw dispatch another.
type TickLog interface {
	Append(ctx context.Context, line string) error

	// Read is the lines the log holds, oldest first; none, and no error, for a
	// log nothing has been written to. mw status and mw sync count them.
	Read(ctx context.Context) ([]string, error)
}

// The words of a tick's line that MillhandTickOutcome reads it by: how a tick
// says it could not look for the Millhand's window, could not tell whether the
// Millhand is needed, could not start a wake, or could not sync.
const (
	TickCouldNotLook = "could not look for the Millhand's window: "
	TickCouldNotTell = "could not tell whether the Millhand is needed"
	TickWakeFailed   = "wake failed: "
	TickSyncFailed   = "sync failed: "
)

// Notifier is where mw millhand tick raises the one desktop notice a sync
// halt is worth, once, on a host that has one to raise it through. A host with
// none reads a nil Notifier and sends nothing beyond the mark and the status
// line.
type Notifier interface {
	Notify(ctx context.Context, line string) error
}

// MayorRespawnException is the one thing the Millhand's charter lets it do to
// the Mayor's seat on the VPS, and the last thing a wake called for by a host
// that is down or has lost its Mayor is told.
const MayorRespawnException = "If the Mayor's process is gone and no handoff is under way you may run the one respawn command on the VPS."

// MillhandTickReport is what one tick found: the one dated line it printed, and
// whether it woke the Millhand.
type MillhandTickReport struct {
	Line string
	Woke bool
}

// String is the report as it is printed: the one line.
func (r MillhandTickReport) String() string { return r.Line + "\n" }

// MillhandTick is what the routine timer runs. It spends no tokens unless
// something needs the Millhand:
//
//  1. A Millhand already up is the end of it: "already up" — unless its session
//     is finished, by the rule mw seat reap --when-idle closes by: it wrote a
//     handoff after its window opened, and its pane is idle at an empty input
//     line on two looks Recheck apart. Then the tick closes the window, adds one
//     line to the reaper log, says so in its line, and goes on as if no Millhand
//     were up, so that the same tick may wake a fresh one. A window whose input
//     line holds text is never closed, and a look that cannot be made is a look
//     at a session that is not finished. A Millhand whose wake never got going
//     is restarted: on the same two looks its pane is idle and it has written
//     no handoff since its window opened. The tick closes the window, says
//     "restarted" in its line and goes on, and the wake it ends with, whatever
//     else there is or is not to wake for, tells the fresh Millhand why. While
//     mw watch says this host's own network is down, a stalled Millhand is left
//     alone instead: restarting it would only spawn another Millhand that can
//     do no more than the last, since the fault that stopped this one is the
//     same fault that would keep Claude Code from reaching the API. The tick
//     says so in its line and closes nothing; the next tick that still finds it
//     stalled once the fault clears restarts it as usual.
//  2. On a host with a [watch] table, mw watch's rule is applied to the host it
//     watches. This comes before the sync, because a fault of this host's own
//     network is one the sync would only time out on: local-fault is said in the
//     line, wakes nobody, and the sync is skipped. The rest of the tick looks on
//     this host all the same.
//  3. One sync, so that this host sees the other one's mail and claims. A sync
//     that fails is said in the line, and the tick looks locally all the same.
//  4. Need is unread mail in any mailbox this host's Millhand reads —
//     millhand@<host>, plain millhand, or the host's own box <host>, which the
//     Millhand reads once it is up — or a story Sweep newly finds stuck on this
//     host, or a watched host that is unwell, stale or down. The mail is only
//     listed: it stays unread until the Millhand reads it.
//  5. No need is "quiet" and nothing is started; need is ONE routine wake whose
//     reason names each mailbox's unread mail, box by box, the stuck story
//     titles and the watch line, verbatim. When the watch says the host is down
//     or its Mayor is gone the reason ends with MayorRespawnException.
//
// A dry run does the same but starts nothing, and it runs neither the sweep nor
// the watch: a sweep records the stories it finds stuck, and Sweep only ever
// reports a story once; a watch remembers a failed check, and the second is what
// calls a host down. A rehearsal that recorded either would leave nobody to wake
// for it.
//
// It prints one dated line and appends it to Log. It leaves with no error for
// every outcome that is not a fault of its own: a failed sync is in the line, and
// is no fault. A wake that fails is one, and so is mail, a sweep or a watch that
// could not be looked at when nothing else called for a wake — that is not
// "quiet". With no [watch] table, mw watch is not consulted at all.
type MillhandTick struct {
	// Millhand is the wake a tick starts; the tick sets its kind and reason and
	// silences what seat up says, so that a tick prints one line.
	Millhand Millhand
	Sync     HostSync
	Mail     Mailbox
	Sweep    Sweep
	Log      TickLog

	// DoctorNotes is where mw doctor leaves the note that a check needs a
	// person's attention, one key per check under DoctorNotePrefix; the tick
	// reads every one of them and remembers, in the same store, which it has
	// already woken the Millhand for. A nil DoctorNotes is no doctor notes to
	// look for at all.
	DoctorNotes DoctorNotes

	// SyncHalts is this host's own mark of a halted sync: written once a sync
	// halts on a merge conflict or a stuck working set, left alone on a halt
	// that repeats, and cleared once a sync is level again. A nil SyncHalts
	// writes and clears nothing, and mw status here has no local mark to read.
	SyncHalts SyncHaltMarker

	// Notify is where the one desktop notice a fresh halt is worth is raised.
	// A nil Notify, or a host with no notifier of its own, sends nothing more
	// than the mark and the status line.
	Notify Notifier

	// Watch is mw watch's rule, applied to the host this one watches. Its
	// Settings are the config's [watch] table: with none, or with no Probes, the
	// tick does not consult it. The tick silences it, and it keeps its own log.
	Watch Watch

	// Reach tells a local network fault (every target unreachable) from real
	// trouble, opening the same grace a resume does. A nil Reach is never
	// asked, so a tick with none wired never finds one this way.
	Reach Reach

	// ReapLog is where a tick that closes a finished Millhand's window says so:
	// the Millhand's reaper log. Recheck is how long the tick waits between its
	// two looks at the window's pane, and Sleep waits it out, or until ctx ends;
	// a nil Sleep sleeps for real, and a zero Recheck does not wait.
	ReapLog ReapLog
	Recheck time.Duration
	Sleep   func(ctx context.Context, d time.Duration) error

	// Host is this host, whose Millhand's mail is looked for.
	Host   string
	DryRun bool

	// Now is the clock the line is dated by; nil is time.Now.
	Now func() time.Time

	// Out is where the line is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run does one tick.
func (t MillhandTick) Run(ctx context.Context) (MillhandTickReport, error) {
	switch {
	case t.Millhand.Windows == nil || t.Sync == nil || t.Mail == nil:
		return MillhandTickReport{}, fmt.Errorf("a tick needs a terminal to look for the Millhand in, a sync and a mailbox")
	case t.Host == "":
		return MillhandTickReport{}, fmt.Errorf("ticking: which host is this? set MW_HOST, or host in the config file")
	}

	line, woke, err := t.look(ctx)

	line = t.now().UTC().Format(time.RFC3339) + " " + line
	if t.Out != nil {
		fmt.Fprintln(t.Out, line)
	}
	if t.Log != nil {
		if logErr := t.Log.Append(ctx, line); logErr != nil && err == nil {
			err = fmt.Errorf("appending to the tick log: %w", logErr)
		}
	}
	return MillhandTickReport{Line: line, Woke: woke}, err
}

// look is the rule itself: what the tick found and did, as the words of its
// line, and the error if it could not do it.
func (t MillhandTick) look(ctx context.Context) (line string, woke bool, err error) {
	up, err := MillhandWindow(ctx, t.Millhand.Windows)
	if err != nil {
		return TickCouldNotLook + oneLine(err.Error()), false, err
	}

	var notes []string
	var restarted string
	var health tickHealth
	healthLooked := false
	if up != "" {
		gone, note, err := t.heal(ctx, up)
		if err == nil && !gone && t.firstRunStuck(ctx, up) {
			return firstRunStuckLine(up, t.noteFirstRunStuck(ctx, up)), false, nil
		}
		if err == nil && !gone && t.stalledCandidate(ctx, up) {
			health = t.health(ctx)
			healthLooked = true
			if health.local {
				note = leftAloneNote(up)
			} else {
				gone, note, restarted, err = t.restart(ctx, up)
			}
		}
		switch {
		case err != nil:
			return joinNotes(alreadyUp(up), []string{note}), false, err
		case !gone:
			if note != "" {
				return joinNotes(alreadyUp(up), []string{note}), false, nil
			}
			return alreadyUp(up), false, nil
		}
		notes = append(notes, note)
	}

	// A resume or a local network fault opens a short grace: doctor notes and
	// the health verdict are deferred rather than read as trouble, giving a
	// network that is only settling back in room to clear on its own. Mail and
	// a newly stuck story still wake the Millhand regardless: they are real
	// work, resume or not.
	announce, justResumed := TickResumed(ctx, t.Log, t.now())
	resuming := justResumed || ReadResumeGrace(ctx, t.Log, t.now()).Open(t.now())
	localFault := t.Reach != nil && !t.Reach.Reachable(ctx)
	deferring := resuming || localFault
	if justResumed {
		notes = append(notes, announce)
	}
	if localFault {
		notes = append(notes, TickLocalNetworkFault)
	}

	// The watch comes first: a fault of this host's own network is one a sync
	// would only time out on.
	if !healthLooked {
		health = t.health(ctx)
	}

	if !health.local {
		if _, err := t.Sync.Run(ctx); err != nil {
			notes = append(notes, syncNote(err))
			if RecordSyncHalt(ctx, t.SyncHalts, err, t.now()) {
				if note := t.notifyHalt(ctx, err); note != "" {
					notes = append(notes, note)
				}
			}
		} else {
			ClearSyncHalt(ctx, t.SyncHalts)
		}
	}
	if health.note != "" {
		notes = append(notes, health.note)
	}

	// What could not be looked at is said, and if nothing else is found it is a
	// fault: "quiet" is only for a tick that looked.
	lookErr := health.err
	mail, err := t.unreadMail(ctx)
	if err != nil {
		lookErr = err
		notes = append(notes, "mail could not be read: "+oneLine(err.Error()))
	}

	var stuck []string
	if t.DryRun {
		notes = append(notes, "stuck stories not looked for in a dry run")
	} else {
		var sweepNotes []string
		stuck, sweepNotes, err = t.sweep(ctx)
		if err != nil {
			lookErr = err
			notes = append(notes, "sweep failed: "+oneLine(err.Error()))
		}
		notes = append(notes, sweepNotes...)
	}

	var doctor []string
	pending := 0
	switch {
	case t.DryRun:
		notes = append(notes, "doctor notes not looked for in a dry run")
	case deferring:
		var pendErr error
		pending, pendErr = t.pendingDoctorNotes(ctx)
		if pendErr != nil {
			lookErr = pendErr
			notes = append(notes, "doctor notes could not be read: "+oneLine(pendErr.Error()))
		}
	default:
		var doctorNotes []string
		doctor, doctorNotes, err = t.doctor(ctx)
		if err != nil {
			lookErr = err
			notes = append(notes, "doctor notes could not be read: "+oneLine(err.Error()))
		}
		notes = append(notes, doctorNotes...)
	}

	healthForReason := health
	if deferring {
		healthForReason.wake = false
	}
	if deferring && !t.DryRun && (pending > 0 || health.wake) {
		notes = append(notes, tickResumingPrefix+counted(pending, "doctor note", "doctor notes")+" and health deferred")
	}

	verdict, reason := "quiet", tickReason(mail, stuck, doctor, healthForReason)
	switch {
	case restarted == "":
	case reason == "":
		reason = restarted
	default:
		reason += "; " + restarted
	}
	switch {
	case reason == "" && lookErr != nil:
		verdict = TickCouldNotTell
		if t.DryRun {
			verdict = "dry run: " + verdict
		}
		return joinNotes(verdict, notes), false, lookErr
	case reason == "" && t.DryRun:
		verdict = "dry run: quiet"
	case reason == "":
	case t.DryRun:
		verdict = "dry run: would wake the Millhand: " + reason
	default:
		verdict, woke, err = t.wake(ctx, reason)
		if err != nil {
			return joinNotes(verdict, notes), false, err
		}
	}
	return joinNotes(verdict, notes), woke, nil
}

// heal asks whether the Millhand whose window is open is finished, and closes
// the window if it is. It says whether the Millhand is to be counted as gone —
// the window was closed, or in a dry run would have been — and the words for the
// line. A Millhand that is not finished is left alone: gone is false and there
// is nothing to say. The window is closed only when it was found idle twice,
// Recheck apart, and never on a doubt.
func (t MillhandTick) heal(ctx context.Context, name string) (gone bool, note string, err error) {
	if t.Millhand.Terminal == nil || t.Millhand.Seats == nil {
		return false, "", nil
	}
	rule := t.reapRule()
	window, there := t.reapWindow(ctx, name)
	if !there {
		return false, "", nil
	}
	if _, finished := rule.Finished(ctx, window); !finished {
		return false, "", nil
	}
	if err := rule.wait(ctx, t.Recheck); err != nil {
		return false, "", nil
	}
	handoff, finished := rule.Finished(ctx, window)
	if !finished {
		return false, "", nil
	}

	if t.DryRun {
		return true, fmt.Sprintf("dry run: would close the finished Millhand's window %s (its handoff of %s, window left open)", name, handoff.UTC().Format(time.RFC3339)), nil
	}
	if err := t.Millhand.Terminal.Close(ctx, window.ID); err != nil {
		return false, "could not close the finished Millhand's window: " + oneLine(err.Error()), fmt.Errorf("closing the window %s: %w", name, err)
	}
	note = fmt.Sprintf("closed the finished Millhand's window %s (its handoff of %s, window left open)", name, handoff.UTC().Format(time.RFC3339))
	if t.ReapLog != nil {
		said := fmt.Sprintf("%s reap %s: closed by the tick: finished at %s, window left open",
			t.now().UTC().Format(time.RFC3339), window.ID, handoff.UTC().Format(time.RFC3339))
		if err := t.ReapLog.Note(ctx, MillhandSeat, said); err != nil {
			note += "; the reaper log could not be written: " + oneLine(err.Error())
		}
	}
	return true, note, nil
}

// restart asks whether the Millhand whose window is open is a wake that never
// got going, and closes the window if it is, by the reaper's own path. It says
// whether the Millhand is to be counted as gone — the window was closed, or in a
// dry run would have been — the words for the line, and what the fresh Millhand
// is to be told of it. The window is closed only when it was found stalled twice,
// Recheck apart, and never on a doubt.
func (t MillhandTick) restart(ctx context.Context, name string) (gone bool, note, told string, err error) {
	if t.Millhand.Terminal == nil || t.Millhand.Seats == nil {
		return false, "", "", nil
	}
	rule := t.reapRule()
	window, there := t.reapWindow(ctx, name)
	if !there || !t.stalled(ctx, rule, window) {
		return false, "", "", nil
	}
	if err := rule.wait(ctx, t.Recheck); err != nil || !t.stalled(ctx, rule, window) {
		return false, "", "", nil
	}

	opened := window.Opened.UTC().Format(time.RFC3339)
	told = fmt.Sprintf("the Millhand woken before this one sat idle at its prompt since %s with no handoff, and the tick closed its window %s", opened, name)
	if t.DryRun {
		return true, fmt.Sprintf("dry run: would be restarted: up but idle since %s, no handoff (window %s left open)", opened, name), told, nil
	}
	if err := t.Millhand.Terminal.Close(ctx, window.ID); err != nil {
		return false, "could not close the stalled Millhand's window: " + oneLine(err.Error()), "", fmt.Errorf("closing the window %s: %w", name, err)
	}
	note = fmt.Sprintf("restarted: up but idle since %s, no handoff (closed the window %s)", opened, name)
	if t.ReapLog != nil {
		said := fmt.Sprintf("%s reap %s: closed by the tick: up but idle since %s, no handoff",
			t.now().UTC().Format(time.RFC3339), window.ID, opened)
		if err := t.ReapLog.Note(ctx, MillhandSeat, said); err != nil {
			note += "; the reaper log could not be written: " + oneLine(err.Error())
		}
	}
	return true, note, told, nil
}

// stalledCandidate is a first look — before health is known, and before any
// waiting — at whether the window is a wake that never got going: whether
// restart would be worth trying at all, and so whether health.local is worth
// asking about before it is.
func (t MillhandTick) stalledCandidate(ctx context.Context, name string) bool {
	if t.Millhand.Terminal == nil || t.Millhand.Seats == nil {
		return false
	}
	rule := t.reapRule()
	window, there := t.reapWindow(ctx, name)
	return there && t.stalled(ctx, rule, window)
}

// leftAloneNote is what the tick's line says when a stalled Millhand's window
// is left alone because this host's own network is down: restarting it would
// only spawn another Millhand that could do no more than the last.
func leftAloneNote(window string) string {
	return WatchLocalFault + ": stalled Millhand's window " + window + " left alone (no outside place answers)"
}

// FirstRunDoctorCheck is the name the tick's own note about a Millhand stuck
// at Claude Code's first-run screen is kept under: doctor.<host>.<check>, the
// same note kind mw doctor's give-ups use, so the Mayor's notifier sees it
// with no check of its own having to know the Millhand never came up.
const FirstRunDoctorCheck = "millhand-first-run"

// firstRunStuck is two looks, Recheck apart, at whether the Millhand's window
// is showing Claude Code's own first-run screen — a theme choice or the login
// menu, not yet a prompt at all. It is the one state a restart cannot fix,
// since the fresh session it opens would show the very same screen: a person
// is needed. Never on a doubt: a look that cannot be made, or a window that
// cannot be found, is a look at a pane that is not stuck.
func (t MillhandTick) firstRunStuck(ctx context.Context, name string) bool {
	if t.Millhand.Terminal == nil {
		return false
	}
	window, there := t.reapWindow(ctx, name)
	if !there || !t.atFirstRun(ctx, window.ID) {
		return false
	}
	if err := t.reapRule().wait(ctx, t.Recheck); err != nil {
		return false
	}
	return t.atFirstRun(ctx, window.ID)
}

// atFirstRun is one look at whether the window's pane shows Claude Code's own
// first-run screen.
func (t MillhandTick) atFirstRun(ctx context.Context, windowID string) bool {
	state, err := t.Millhand.Terminal.PaneState(ctx, windowID)
	return err == nil && state == PaneFirstRun
}

// firstRunHandStep is the standing instructions the tick's line and doctor
// note both give: what a person does about a Millhand stuck at Claude Code's
// first-run screen.
const firstRunHandStep = "hands needed: tmux attach -t mw-seats, finish it, detach"

// firstRunStuckLine is what the tick's line says when the Millhand's window is
// stuck at Claude Code's first-run screen, with whatever noteErr adds — a
// doctor note's own trouble, or nothing when there was none.
func firstRunStuckLine(window, noteErr string) string {
	line := "the Millhand is stuck at claude's first-run screen (" + firstRunHandStep + ") (" + window + ")"
	if noteErr != "" {
		line = joinNotes(line, []string{noteErr})
	}
	return line
}

// noteFirstRunStuck leaves the tick's own doctor note the first time the
// Millhand's window is found stuck at Claude Code's first-run screen, under
// FirstRunDoctorCheck, and does nothing on a later tick that finds it stuck
// still: DoctorNotes.Note reads back "" for a key nothing has set, which is
// how a note already left is told apart from none at all. A nil DoctorNotes
// leaves no note, and a dry run leaves none either — nothing else a dry run
// does is real. It says what went wrong leaving the note, "" when nothing
// did.
func (t MillhandTick) noteFirstRunStuck(ctx context.Context, window string) string {
	if t.DoctorNotes == nil {
		return ""
	}
	key := DoctorNoteKey(t.Host, FirstRunDoctorCheck)
	existing, err := t.DoctorNotes.Note(ctx, key)
	if err != nil {
		return "doctor note could not be read: " + oneLine(err.Error())
	}
	if existing != "" {
		return ""
	}
	if t.DryRun {
		return "dry run: would leave a doctor note that the Millhand is stuck at claude's first-run screen"
	}
	value := t.now().UTC().Format(time.RFC3339) + " faulty stuck at claude's first-run screen: " + firstRunHandStep + " (" + window + ")"
	if err := t.DoctorNotes.SetNote(ctx, key, value); err != nil {
		return "doctor note could not be written: " + oneLine(err.Error())
	}
	return ""
}

// stalled is one look at whether the Millhand in a window is a wake that never
// got going: the terminal can say when its window opened, no handoff has been
// written since, and its pane is idle at an empty input line — the reaper's
// newest handoff since and its idle pane. A look that cannot be made is a look at
// a wake that is going.
func (t MillhandTick) stalled(ctx context.Context, rule SeatReap, window ReapWindow) bool {
	if window.Opened.IsZero() {
		return false
	}
	start, err := rule.Seats.SeatStart(ctx, rule.Seat, rule.Host)
	if err != nil {
		return false
	}
	if _, since := newestHandoffSince(start.Handoffs, window.Opened); since {
		return false
	}
	return rule.paneIdle(ctx, window.ID)
}

// reapRule is the rule of mw seat reap --when-idle, for this host's Millhand.
func (t MillhandTick) reapRule() SeatReap {
	return SeatReap{
		Seats:    t.Millhand.Seats,
		Terminal: t.Millhand.Terminal,
		Seat:     MillhandSeat,
		Host:     t.Host,
		WhenIdle: true,
		Now:      t.now,
		Sleep:    t.Sleep,
	}
}

// reapWindow is the window of that name as the terminal's reaper side sees it,
// with its id and when it opened; false when it cannot be found.
func (t MillhandTick) reapWindow(ctx context.Context, name string) (ReapWindow, bool) {
	open, err := t.Millhand.Terminal.OpenWindows(ctx)
	if err != nil {
		return ReapWindow{}, false
	}
	for _, window := range open {
		if window.Name == name {
			return window, true
		}
	}
	return ReapWindow{}, false
}

// tickHealth is what applying mw watch's rule found, as the tick needs it.
type tickHealth struct {
	// line is the watch line, verbatim; empty when the watch found nothing: no
	// [watch] table, a dry run, or a watch that failed.
	line string
	// wake is whether the line calls for a wake, and local whether it says this
	// host's own network is down, so that a sync is not worth trying.
	wake, local bool
	// note is what the tick's line says of it besides a wake's reason.
	note string
	// err is the fault of a watch that could not be run.
	err error
}

// health applies mw watch's rule, once. It does not fail the tick by itself: a
// watch that could not be run is said in the line, and is a fault only when
// nothing else calls for a wake.
func (t MillhandTick) health(ctx context.Context) tickHealth {
	if t.Watch.Settings.Empty() || t.Watch.Probes == nil {
		return tickHealth{}
	}
	if t.DryRun {
		return tickHealth{note: "mw watch not run in a dry run"}
	}

	watching := t.Watch
	watching.Out = nil
	report, err := watching.Run(ctx)
	if _, wakes := WatchWakes(err); wakes {
		err = nil
	}
	found := tickHealth{line: report.Line, wake: report.Wake, err: err}
	switch {
	case err != nil:
		found.note = "watch failed: " + oneLine(err.Error())
	case report.Line == WatchLocalFault:
		found.local = true
		found.note = WatchLocalFault + ": this host's network is down, so it did not sync"
	case strings.HasPrefix(report.Line, WatchUnreachable):
		found.note = "watch: " + report.Line
	}
	return found
}

// syncNote says what a sync that stopped did not do, in a phrase.
func syncNote(err error) string {
	if blocked, ok := Blocked(err); ok {
		return "sync did only its beads half: the vault holds uncommitted changes to " + strings.Join(blocked.Files, ", ")
	}
	return TickSyncFailed + oneLine(err.Error())
}

// notifyHalt raises the one desktop notice a fresh sync halt is worth. It is
// called only once RecordSyncHalt has already said this is the first halt of a
// run of them, and only on a real run: a dry run sends nothing, since nothing
// else it does is real either. A nil Notify sends nothing. A notice that
// cannot be raised is said as a note of its own, never a failure of the tick.
func (t MillhandTick) notifyHalt(ctx context.Context, err error) string {
	if t.Notify == nil || t.DryRun {
		return ""
	}
	halt, ok := Halted(err)
	if !ok {
		return ""
	}
	line := fmt.Sprintf("mw: sync halted on %s since %s: %s", t.Host, t.now().UTC().Format(LastSyncFormat), halt.Said)
	if notifyErr := t.Notify.Notify(ctx, line); notifyErr != nil {
		return "notify failed: " + oneLine(notifyErr.Error())
	}
	return ""
}

// mailBoxes is every mailbox this host's Millhand reads, in the order the
// wake reason names them: its host-qualified box, its plain seat box, and the
// host's own box, which the Millhand reads once it is up.
func (t MillhandTick) mailBoxes() []string {
	return []string{SeatIdentity(MillhandSeat, t.Host), MillhandSeat, t.Host}
}

// boxMail is the unread mail found in one mailbox: its quoted subjects,
// oldest first.
type boxMail struct {
	box      string
	subjects []string
}

// unreadMail is the unread mail in every mailbox this host's Millhand reads,
// one group per box that holds any, oldest message first within a box. The
// mail is not read. What could be listed comes back with the error, if a
// mailbox could not be.
func (t MillhandTick) unreadMail(ctx context.Context) ([]boxMail, error) {
	var groups []boxMail
	var failed error
	seen := map[string]bool{}
	for _, mailbox := range t.mailBoxes() {
		inbox, err := t.Mail.Inbox(ctx, mailbox)
		if err != nil {
			failed = err
			continue
		}
		var messages []Message
		for _, message := range inbox {
			if !seen[message.ID] {
				seen[message.ID] = true
				messages = append(messages, message)
			}
		}
		if len(messages) == 0 {
			continue
		}
		sort.SliceStable(messages, func(i, j int) bool { return messages[i].Sent.Before(messages[j].Sent) })

		subjects := make([]string, len(messages))
		for i, message := range messages {
			subjects[i] = fmt.Sprintf("%q", oneLine(message.Subject))
		}
		groups = append(groups, boxMail{box: mailbox, subjects: subjects})
	}
	return groups, failed
}

// sweep is the stories Sweep newly finds stuck on this host, as `"title" (id)`,
// and what it noted on the way. Sweep prints nothing here.
func (t MillhandTick) sweep(ctx context.Context) (stuck, notes []string, err error) {
	sweep := t.Sweep
	sweep.Out = nil
	report, err := sweep.Run(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, detail := range report.Stuck {
		stuck = append(stuck, fmt.Sprintf("%q (%s)", oneLine(detail.Story.Title), detail.Story.ID))
	}
	for _, note := range report.Notes {
		notes = append(notes, "sweep: "+oneLine(note))
	}
	return stuck, notes, nil
}

// DoctorSeenKey is where the tick remembers, for one check on this host, the
// normalised text (normalizeDoctorNote) of the doctor.<check> note it last
// woke the Millhand for — its own memory, kept beside the doctor's notes, the
// way Sweep's memory of a session is kept beside its story. A note whose
// verdict and reason have not changed since is not woken for again, even
// though the doctor rewrites its timestamp and its "last log lines" tail on
// every run; a fresh one — a new fault, a changed reason, or the same one
// again after the check went ok and cleared it — is.
func DoctorSeenKey(host, check string) string { return "millhandtick.doctor." + host + "." + check }

// normalizeDoctorNote is a doctor note's text with the parts that change on
// every run, whether or not anything is actually new, taken back out: the
// leading RFC3339 timestamp Doctor.writeNote stamps it with, and the
// "| last log lines: ..." tail it appends. What is left is the verdict and
// the reason, which is what changing means for a doctor note.
func normalizeDoctorNote(value string) string {
	if i := strings.Index(value, " | last log lines:"); i >= 0 {
		value = value[:i]
	}
	if sp := strings.IndexByte(value, ' '); sp >= 0 {
		if _, err := time.Parse(time.RFC3339, value[:sp]); err == nil {
			value = value[sp+1:]
		}
	}
	return value
}

// doctor is every doctor.<host>.<check> note of this tick's own host that it
// has not already woken the Millhand for, as ready-made wake reasons, and
// what it noted on the way. A note of another host is left alone — two
// hosts' doctors share the one table, and only the host it is about should
// ever wake for it. A leftover doctor.<check> note with no host, from before
// notes were host-qualified, does not parse and is skipped the same way,
// rather than read as any one host's. It is skipped, quietly, with no
// DoctorNotes wired at all.
func (t MillhandTick) doctor(ctx context.Context) (reasons, notes []string, err error) {
	if t.DoctorNotes == nil {
		return nil, nil, nil
	}
	found, err := t.DoctorNotes.NotesWithPrefix(ctx, DoctorNotePrefix)
	if err != nil {
		return nil, nil, err
	}
	checks := make([]string, 0, len(found))
	for key := range found {
		host, check, ok := ParseDoctorNoteKey(key)
		if !ok || host != t.Host {
			continue
		}
		checks = append(checks, check)
	}
	sort.Strings(checks)

	for _, check := range checks {
		value := found[DoctorNoteKey(t.Host, check)]
		normalized := normalizeDoctorNote(value)
		seenKey := DoctorSeenKey(t.Host, check)
		seen, readErr := t.DoctorNotes.Note(ctx, seenKey)
		if readErr != nil {
			notes = append(notes, fmt.Sprintf("doctor: %s's seen mark could not be read: %s", check, oneLine(readErr.Error())))
			continue
		}
		if seen == normalized {
			continue
		}
		if setErr := t.DoctorNotes.SetNote(ctx, seenKey, normalized); setErr != nil {
			notes = append(notes, fmt.Sprintf("doctor: %s's seen mark could not be written: %s", check, oneLine(setErr.Error())))
			continue
		}
		reasons = append(reasons, doctorReasonPart(check, value))
	}
	return reasons, notes, nil
}

// pendingDoctorNotes is how many of this host's own doctor.<host>.<check>
// notes are not yet marked seen, without marking any of them — unlike
// doctor, which marks a note seen the moment it turns it into a wake reason.
// A grace's deferral must not mark a note seen: that would lose it, rather
// than merely hold it back for the tick that reads it once the grace ends.
func (t MillhandTick) pendingDoctorNotes(ctx context.Context) (int, error) {
	if t.DoctorNotes == nil {
		return 0, nil
	}
	found, err := t.DoctorNotes.NotesWithPrefix(ctx, DoctorNotePrefix)
	if err != nil {
		return 0, err
	}
	count := 0
	for key, value := range found {
		host, check, ok := ParseDoctorNoteKey(key)
		if !ok || host != t.Host {
			continue
		}
		seen, readErr := t.DoctorNotes.Note(ctx, DoctorSeenKey(t.Host, check))
		if readErr != nil {
			continue
		}
		if seen != normalizeDoctorNote(value) {
			count++
		}
	}
	return count, nil
}

// doctorReasonPart is one doctor note's wake reason: the check it is about,
// its note text verbatim, and the standing instructions for a person taking
// it from there.
func doctorReasonPart(check, note string) string {
	return fmt.Sprintf(
		"doctor: %s: %s. Run `mw doctor %s` by hand, read ~/.local/state/mw-doctor/log, report.",
		check, note, check)
}

// wake starts the one routine wake, and says what came of it. A Millhand that
// came up since the check is not a failure.
func (t MillhandTick) wake(ctx context.Context, reason string) (verdict string, woke bool, err error) {
	millhand := t.Millhand
	millhand.Wake, millhand.Reason, millhand.Out = WakeRoutine, reason, nil
	_, err = millhand.Run(ctx)
	if up, isUp := MillhandIsUp(err); isUp {
		return alreadyUp(up.Window), false, nil
	}
	if err != nil {
		return TickWakeFailed + oneLine(err.Error()), false, err
	}
	return "woke the Millhand: " + reason, true, nil
}

func alreadyUp(window string) string { return "already up (" + window + ")" }

// tickReason is what the Millhand is told it was woken for: the unread mail
// of each mailbox it reads, named with its box and TickReasonLimit subjects
// then a count of the rest, the stuck stories the same way, every doctor note
// newly seen, and the watch line as it was found. It is empty when there is
// nothing.
func tickReason(mail []boxMail, stuck, doctor []string, health tickHealth) string {
	var parts []string
	for _, group := range mail {
		parts = append(parts, counted(len(group.subjects), "unread", "unread")+" in "+group.box+": "+named(group.subjects))
	}
	if len(stuck) > 0 {
		parts = append(parts, counted(len(stuck), "stuck story", "stuck stories")+": "+named(stuck))
	}
	parts = append(parts, doctor...)
	if health.wake {
		part := "mw watch says: " + health.line
		if mayorMayBeGone(health.line) {
			part += ". " + MayorRespawnException
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

// mayorMayBeGone reports whether a watch line that calls for a wake is one where
// the Mayor may be gone: the host is down, so nothing is known of its Mayor, or
// it is unwell and its reasons say mayor_gone (the health line's mayor=gone).
func mayorMayBeGone(line string) bool {
	fields := strings.Fields(line)
	switch {
	case len(fields) == 0:
		return false
	case fields[0] == WatchDown:
		return true
	case fields[0] != WatchUnwell:
		return false
	}
	for _, field := range fields[1:] {
		for _, reason := range strings.Split(field, ",") {
			if reason == "mayor_gone" || reason == "mayor=gone" {
				return true
			}
		}
	}
	return false
}

// named lists the first TickReasonLimit names, and counts the rest.
func named(names []string) string {
	shown := names
	if len(shown) > TickReasonLimit {
		shown = shown[:TickReasonLimit]
	}
	list := strings.Join(shown, ", ")
	if more := len(names) - len(shown); more > 0 {
		list += fmt.Sprintf(" and %d more", more)
	}
	return list
}

func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// joinNotes puts what went wrong on the way after what the tick found.
func joinNotes(verdict string, notes []string) string {
	return strings.Join(append([]string{verdict}, notes...), "; ")
}

// oneLine is text on a single line: every run of whitespace, newlines included,
// is one space, so that a complaint of many lines is still one line of the log.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func (t MillhandTick) now() time.Time {
	if t.Now == nil {
		return time.Now()
	}
	return t.Now()
}
