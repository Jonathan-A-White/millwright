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
//     at a session that is not finished.
//  2. On a host with a [watch] table, mw watch's rule is applied to the host it
//     watches. This comes before the sync, because a fault of this host's own
//     network is one the sync would only time out on: local-fault is said in the
//     line, wakes nobody, and the sync is skipped. The rest of the tick looks on
//     this host all the same.
//  3. One sync, so that this host sees the other one's mail and claims. A sync
//     that fails is said in the line, and the tick looks locally all the same.
//  4. Need is unread mail for this host's Millhand — millhand@<host>, or plain
//     millhand — or a story Sweep newly finds stuck on this host, or a watched
//     host that is unwell, stale or down. The mail is only listed: it stays
//     unread until the Millhand reads it.
//  5. No need is "quiet" and nothing is started; need is ONE routine wake whose
//     reason names the mail subjects, the stuck story titles and the watch line,
//     verbatim. When the watch says the host is down or its Mayor is gone the
//     reason ends with MayorRespawnException.
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

	// Watch is mw watch's rule, applied to the host this one watches. Its
	// Settings are the config's [watch] table: with none, or with no Probes, the
	// tick does not consult it. The tick silences it, and it keeps its own log.
	Watch Watch

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
	if up != "" {
		gone, note, err := t.heal(ctx, up)
		switch {
		case err != nil:
			return joinNotes(alreadyUp(up), []string{note}), false, err
		case !gone:
			return alreadyUp(up), false, nil
		}
		notes = append(notes, note)
	}

	// The watch comes first: a fault of this host's own network is one a sync
	// would only time out on.
	health := t.health(ctx)

	if !health.local {
		if _, err := t.Sync.Run(ctx); err != nil {
			notes = append(notes, syncNote(err))
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

	verdict, reason := "quiet", tickReason(mail, stuck, health)
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
	rule := SeatReap{
		Seats:    t.Millhand.Seats,
		Terminal: t.Millhand.Terminal,
		Seat:     MillhandSeat,
		Host:     t.Host,
		WhenIdle: true,
		Now:      t.now,
		Sleep:    t.Sleep,
	}
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

// unreadMail is the quoted subjects of the unread mail in this host's Millhand's
// mailboxes, oldest first. The mail is not read. What could be listed comes
// back with the error, if a mailbox could not be.
func (t MillhandTick) unreadMail(ctx context.Context) ([]string, error) {
	var messages []Message
	var failed error
	seen := map[string]bool{}
	for _, mailbox := range []string{SeatIdentity(MillhandSeat, t.Host), MillhandSeat} {
		inbox, err := t.Mail.Inbox(ctx, mailbox)
		if err != nil {
			failed = err
			continue
		}
		for _, message := range inbox {
			if !seen[message.ID] {
				seen[message.ID] = true
				messages = append(messages, message)
			}
		}
	}
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].Sent.Before(messages[j].Sent) })

	subjects := make([]string, len(messages))
	for i, message := range messages {
		subjects[i] = fmt.Sprintf("%q", oneLine(message.Subject))
	}
	return subjects, failed
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

// tickReason is what the Millhand is told it was woken for: the mail subjects
// and the stuck stories, TickReasonLimit of each and a count of the rest, and
// the watch line as it was found. It is empty when there is nothing.
func tickReason(mail, stuck []string, health tickHealth) string {
	var parts []string
	if len(mail) > 0 {
		parts = append(parts, counted(len(mail), "unread message", "unread messages")+": "+named(mail))
	}
	if len(stuck) > 0 {
		parts = append(parts, counted(len(stuck), "stuck story", "stuck stories")+": "+named(stuck))
	}
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
