package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// LoopSittingKey is the note that keeps the day of the last weekly loop sitting,
// as YYYY-MM-DD. mw loop done writes it; the DAMPERS section reads it (mw-gq6.997).
const LoopSittingKey = "loop.sitting"

// LoopSittingOverdueDays is how many days may pass after a sitting before the
// next is overdue: a week, and a day of grace for a sitting held a day late.
const LoopSittingOverdueDays = 8

// LoopSitting is what the tracker's notes say of the weekly loop sitting.
type LoopSitting struct {
	// Read says the note was looked for; the DAMPERS section judges the sitting
	// only when it was, so a reader with no notes says nothing of it.
	Read bool
	// Last is the day of the last sitting recorded (UTC midnight); zero when none is.
	Last time.Time
}

// ParseLoopSitting reads the note's value; the zero value, with Read set, when
// the note is empty or not a day.
func ParseLoopSitting(value string) LoopSitting {
	last, err := time.Parse(LedgerDate, strings.TrimSpace(value))
	if err != nil {
		return LoopSitting{Read: true}
	}
	return LoopSitting{Read: true, Last: last}
}

// Alarm is the DAMPERS line for a sitting that is overdue at now, false when it
// is not (or was not read).
func (s LoopSitting) Alarm(now time.Time) (DamperAlarm, bool) {
	if !s.Read {
		return DamperAlarm{}, false
	}
	if s.Last.IsZero() {
		return DamperAlarm{Principle: DamperAgeing,
			Text: "no weekly loop sitting recorded (mw loop done records one)"}, true
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	days := int(today.Sub(s.Last).Hours() / 24)
	if days <= LoopSittingOverdueDays {
		return DamperAlarm{}, false
	}
	return DamperAlarm{Principle: DamperAgeing,
		Text: fmt.Sprintf("weekly loop sitting overdue: the last was %s, %d days ago (mw loop done records one)",
			s.Last.Format(LedgerDate), days)}, true
}

// LoopSittingNotes is the note store mw loop done reads and writes.
type LoopSittingNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
}

// LoopDone is mw loop done: it records that the weekly loop sitting was held.
type LoopDone struct {
	Notes LoopSittingNotes
	Now   func() time.Time
	// Day is the day of the sitting, YYYY-MM-DD; empty is today, UTC.
	Day string
	Out io.Writer
}

// Run records the sitting. A day earlier than the one already recorded changes
// nothing, so that a late entry of an old sitting cannot make the last look older.
func (l LoopDone) Run(ctx context.Context) error {
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	day := strings.TrimSpace(l.Day)
	if day == "" {
		day = now.UTC().Format(LedgerDate)
	}
	at, err := time.Parse(LedgerDate, day)
	if err != nil {
		return fmt.Errorf("--day %q is not a day: give it as YYYY-MM-DD", l.Day)
	}
	if at.After(now.UTC()) {
		return fmt.Errorf("--day %s is in the future", day)
	}
	held, err := l.Notes.Note(ctx, LoopSittingKey)
	if err != nil {
		return fmt.Errorf("reading the last loop sitting: %w", err)
	}
	if last := ParseLoopSitting(held).Last; !last.IsZero() && last.After(at) {
		l.say("the last loop sitting recorded is %s, later than %s; nothing changed\n", last.Format(LedgerDate), day)
		return nil
	}
	if err := l.Notes.SetNote(ctx, LoopSittingKey, day); err != nil {
		return fmt.Errorf("recording the loop sitting: %w", err)
	}
	l.say("loop sitting of %s recorded; the next is due within %d days\n", day, LoopSittingOverdueDays)
	return nil
}

func (l LoopDone) say(format string, args ...any) {
	if l.Out != nil {
		fmt.Fprintf(l.Out, format, args...)
	}
}
