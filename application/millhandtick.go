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

// TickLog is the log a host keeps of what its ticks found: one line each,
// appended, and never more than TickLogLines of them — an adapter drops the
// oldest as it appends.
type TickLog interface {
	Append(ctx context.Context, line string) error
}

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
//  1. A Millhand already up is the end of it: "already up".
//  2. One sync, so that this host sees the other one's mail and claims. A sync
//     that fails is said in the line, and the tick looks locally all the same.
//  3. Need is unread mail for this host's Millhand — millhand@<host>, or plain
//     millhand — or a story Sweep newly finds stuck on this host. The mail is
//     only listed: it stays unread until the Millhand reads it.
//  4. No need is "quiet" and nothing is started; need is ONE routine wake whose
//     reason names the mail subjects and the stuck story titles.
//
// A dry run does the same but starts nothing, and it does not sweep either: a
// sweep records the stories it finds stuck, and Sweep only ever reports a story
// once, so a rehearsal that recorded one would leave nobody to wake for it.
//
// It prints one dated line and appends it to Log. It leaves with no error for
// every outcome that is not a fault of its own: a failed sync is in the line, and
// is no fault. A wake that fails is one, and so is mail or a sweep that could not
// be looked at when nothing else called for a wake — that is not "quiet". It does not consult mw watch.
type MillhandTick struct {
	// Millhand is the wake a tick starts; the tick sets its kind and reason and
	// silences what seat up says, so that a tick prints one line.
	Millhand Millhand
	Sync     HostSync
	Mail     Mailbox
	Sweep    Sweep
	Log      TickLog

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
		return "could not look for the Millhand's window: " + oneLine(err.Error()), false, err
	}
	if up != "" {
		return alreadyUp(up), false, nil
	}

	var notes []string
	if _, err := t.Sync.Run(ctx); err != nil {
		notes = append(notes, syncNote(err))
	}

	// What could not be looked at locally is said, and if nothing else is found
	// it is a fault: "quiet" is only for a tick that looked.
	var lookErr error
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

	verdict, reason := "quiet", tickReason(mail, stuck)
	switch {
	case reason == "" && lookErr != nil:
		verdict = "could not tell whether the Millhand is needed"
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

// syncNote says what a sync that stopped did not do, in a phrase.
func syncNote(err error) string {
	if blocked, ok := Blocked(err); ok {
		return "sync did only its beads half: the vault holds uncommitted changes to " + strings.Join(blocked.Files, ", ")
	}
	return "sync failed: " + oneLine(err.Error())
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
		return "wake failed: " + oneLine(err.Error()), false, err
	}
	return "woke the Millhand: " + reason, true, nil
}

func alreadyUp(window string) string { return "already up (" + window + ")" }

// tickReason is what the Millhand is told it was woken for: the mail subjects
// and the stuck stories, TickReasonLimit of each and a count of the rest. It is
// empty when there is nothing.
func tickReason(mail, stuck []string) string {
	var parts []string
	if len(mail) > 0 {
		parts = append(parts, counted(len(mail), "unread message", "unread messages")+": "+named(mail))
	}
	if len(stuck) > 0 {
		parts = append(parts, counted(len(stuck), "stuck story", "stuck stories")+": "+named(stuck))
	}
	return strings.Join(parts, "; ")
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
