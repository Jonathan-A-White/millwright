package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// UnitFailedSeat is the seat the failure mail is signed by, as the unit
// mw-unit-failed@.service has always sent it: doctor@<host>.
const UnitFailedSeat = "doctor"

// UnitFailure is the first of what may become two failures in a row of one unit:
// when its mail was asked for, and the last lines of the unit's journal then.
type UnitFailure struct {
	At      time.Time
	Journal string
}

// UnitSystem is what UnitFailed asks the init system about a unit. The adapter
// is systemctl and journalctl (infrastructure/doctor/unitfailed.go); a fake
// stands in for tests.
type UnitSystem interface {
	// RunByTimer says whether a timer starts the unit: a unit started by hand
	// or by another unit is not one a later run would follow.
	RunByTimer(ctx context.Context, unit string) (bool, error)

	// JournalTail is the last lines of the unit's journal.
	JournalTail(ctx context.Context, unit string) (string, error)

	// RanCleanSince says whether the unit had a run that ended clean after
	// since.
	RanCleanSince(ctx context.Context, unit string, since time.Time) (bool, error)
}

// UnitFailures keeps, per unit, the one failure that has not yet been followed
// by a second or by a clean run.
type UnitFailures interface {
	// Load reports the failure kept for a unit; ok is false when none is.
	Load(ctx context.Context, unit string) (failure UnitFailure, ok bool, err error)

	// Save keeps a unit's failure, replacing any kept.
	Save(ctx context.Context, unit string, failure UnitFailure) error

	// Clear forgets a unit's failure; clearing none is not an error.
	Clear(ctx context.Context, unit string) error
}

// UnitFailed is what runs when a unit ends in failure
// (contrib/systemd/mw-unit-failed@.service). A unit a timer runs often fails
// once and is clean on its next run, so the first failure is only kept; a
// second, with no clean run between, is mailed to the Mayor with the end of
// both journals, and a clean run in between starts the count again. A unit no
// timer runs is mailed at once, as every failure was before.
type UnitFailed struct {
	Units    UnitSystem
	Failures UnitFailures
	Mailbox  Mailbox

	// Host is this host's name, in the mail's subject and the seat that signs it.
	Host string

	// Now reads the clock; nil reads the real one.
	Now func() time.Time

	// Out says what was done, in one line; nil prints nothing.
	Out io.Writer
}

// UnitFailedOutcome says what Run did.
type UnitFailedOutcome int

const (
	// UnitFailureKept: a timer unit's first failure was recorded, no mail sent.
	UnitFailureKept UnitFailedOutcome = iota
	// UnitFailureMailed: the Mayor was mailed.
	UnitFailureMailed
)

// Run handles the failure of unit. A record or a systemd answer that cannot be
// read never holds a mail back: the Mayor is told at once, which is all the
// unit did before there was a count.
func (u UnitFailed) Run(ctx context.Context, unit string) (UnitFailedOutcome, error) {
	unit = strings.TrimSpace(unit)
	if unit == "" || strings.ContainsAny(unit, "/\\") {
		return UnitFailureKept, fmt.Errorf("unit-failed needs the name of a unit, not %q", unit)
	}
	now := time.Now
	if u.Now != nil {
		now = u.Now
	}
	at := now()
	journal := u.journal(ctx, unit)

	timer, err := u.Units.RunByTimer(ctx, unit)
	if err != nil || !timer {
		// Nothing says a later run is coming: tell now. Nothing is kept.
		u.forget(ctx, unit)
		return UnitFailureMailed, u.mail(ctx, unit, fmt.Sprintf("unit failed: %s on %s", unit, u.Host), journal)
	}

	first, kept, err := u.Failures.Load(ctx, unit)
	if err != nil {
		u.say("%s: cannot read the failure kept for it (%v); telling the Mayor\n", unit, err)
		return UnitFailureMailed, u.mail(ctx, unit, fmt.Sprintf("unit failed: %s on %s", unit, u.Host), journal)
	}
	if kept {
		clean, err := u.Units.RanCleanSince(ctx, unit, first.At)
		if err != nil {
			// Cannot tell whether it was clean between: two failures is
			// enough to tell the Mayor.
			clean = false
		}
		if !clean {
			body := fmt.Sprintf("First failure (%s):\n%s\n\nSecond failure (%s):\n%s",
				first.At.UTC().Format(time.RFC3339), first.Journal, at.UTC().Format(time.RFC3339), journal)
			// A fault that lasts is mailed on every second failure, not on
			// every one: the pair is spent by this mail.
			u.forget(ctx, unit)
			return UnitFailureMailed, u.mail(ctx, unit, fmt.Sprintf("unit failed twice in a row: %s on %s", unit, u.Host), body)
		}
	}

	if err := u.Failures.Save(ctx, unit, UnitFailure{At: at, Journal: journal}); err != nil {
		u.say("%s: cannot keep this failure (%v); telling the Mayor\n", unit, err)
		return UnitFailureMailed, u.mail(ctx, unit, fmt.Sprintf("unit failed: %s on %s", unit, u.Host), journal)
	}
	u.say("%s failed once; recorded, no mail until it fails again without a clean run between\n", unit)
	return UnitFailureKept, nil
}

func (u UnitFailed) journal(ctx context.Context, unit string) string {
	text, err := u.Units.JournalTail(ctx, unit)
	if err != nil {
		return fmt.Sprintf("(the journal could not be read: %v)\n%s", err, text)
	}
	return text
}

func (u UnitFailed) forget(ctx context.Context, unit string) {
	if err := u.Failures.Clear(ctx, unit); err != nil {
		u.say("%s: cannot forget the failure kept for it: %v\n", unit, err)
	}
}

func (u UnitFailed) mail(ctx context.Context, unit, subject, body string) error {
	if strings.TrimSpace(body) == "" {
		body = NoBody
	}
	id, err := u.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(UnitFailedSeat, u.Host),
		To:      MayorMailbox,
		Subject: subject,
		Body:    body,
	})
	if err != nil {
		return err
	}
	u.say("%s -> %s\n", id, MayorMailbox)
	return nil
}

func (u UnitFailed) say(format string, args ...any) {
	if u.Out != nil {
		fmt.Fprintf(u.Out, format, args...)
	}
}
