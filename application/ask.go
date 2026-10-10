package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// The labels that record an ask from or to someone outside the factory. A bead
// that delivers the work carries asked-by:<login> for an inbound ask and
// asked-of:<login> for an outbound one, and ask:<role> for who that person is
// to the Governor (tl, skip, drqs), so the history survives a reorg.
const (
	LabelAskedByPrefix = "asked-by:"
	LabelAskedOfPrefix = "asked-of:"
	LabelAskRolePrefix = "ask:"
)

// LabelWaitingOthers marks a bead whose outbound ask has been sent and now
// waits on someone else: Needs you lists it under Waiting on others, and mw
// status lists it for the Mayor to recheck its Done-when.
const LabelWaitingOthers = "waiting:others"

// ChaseAfterWorkingDays is how many working days (Monday to Friday) a bead
// waiting on others may go without a change before Needs you asks the Governor
// to chase it.
const ChaseAfterWorkingDays = 3

// AskWaitingKey is the note holding the RFC 3339 time a bead began waiting on
// others, written by mw ask waiting and cleared by mw ask done.
func AskWaitingKey(bead string) string { return "ask.waiting." + bead }

// labelValue is what follows prefix in the first of labels that starts with it,
// in any case; "" when none does.
func labelValue(labels []string, prefix string) string {
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if len(label) > len(prefix) && strings.EqualFold(label[:len(prefix)], prefix) {
			return strings.TrimSpace(label[len(prefix):])
		}
	}
	return ""
}

// AskedBy is the login the bead's asked-by label names, "" when it has none.
func (d StoryDetail) AskedBy() string { return labelValue(d.Labels, LabelAskedByPrefix) }

// AskedOf is the login the bead's asked-of label names, "" when it has none.
func (d StoryDetail) AskedOf() string { return labelValue(d.Labels, LabelAskedOfPrefix) }

// AskRole is the role the bead's ask:<role> label names, "" when it has none.
func (d StoryDetail) AskRole() string { return labelValue(d.Labels, LabelAskRolePrefix) }

// WaitingOnOthers reports whether the bead is labelled waiting:others.
func (d StoryDetail) WaitingOnOthers() bool { return hasLabel(d.Labels, LabelWaitingOthers) }

// AddWorkingDays is t moved forward n working days: each step is a day later,
// and a Saturday or Sunday is not counted, so three working days from a Friday
// is the Wednesday. The days are read in t's own zone.
func AddWorkingDays(t time.Time, n int) time.Time {
	for n > 0 {
		t = t.AddDate(0, 0, 1)
		if wd := t.Weekday(); wd != time.Saturday && wd != time.Sunday {
			n--
		}
	}
	return t
}

// askWaitingSince is when d began waiting on others: the time in its note, else
// the last change the tracker recorded, else when it was filed; zero when none
// is known.
func askWaitingSince(note string, d StoryDetail) time.Time {
	if at, err := time.Parse(time.RFC3339, strings.TrimSpace(note)); err == nil {
		return at.UTC()
	}
	return firstKnown(d.Updated, d.Created)
}

// askChaseAt is when d, waiting on others since the given time, is due a chase:
// ChaseAfterWorkingDays working days after the later of that time and the
// bead's last change, so a bead that moves is not chased. Zero when unknown.
func askChaseAt(since time.Time, d StoryDetail) time.Time {
	last := since
	if d.Updated.After(last) {
		last = d.Updated
	}
	if last.IsZero() {
		return time.Time{}
	}
	return AddWorkingDays(last.UTC(), ChaseAfterWorkingDays)
}

// askWord is a login or a role as it is written on a label: without an at
// sign, in lower case.
func askWord(login string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(login), "@"))
}

// AskNotes is the notes mw ask keeps when a bead began waiting: TrackerSync's
// own Note, SetNote and ClearNote, narrowed.
type AskNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
	ClearNote(ctx context.Context, key string) error
}

// Ask records an ask from or to someone outside the factory on a bead, and
// marks the bead waiting on others once the ask is sent, or done waiting.
type Ask struct {
	Tracker WorkTracker
	Notes   AskNotes
	// Now is the clock a wait begins by. The zero value reads the real one.
	Now func() time.Time
	Out io.Writer
}

func (a Ask) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a Ask) say(format string, args ...any) {
	if a.Out != nil {
		fmt.Fprintf(a.Out, format+"\n", args...)
	}
}

// By labels the bead asked-by:<login>, and ask:<role> when role is not empty.
func (a Ask) By(ctx context.Context, bead, login, role string) error {
	return a.label(ctx, bead, LabelAskedByPrefix, login, role, "who asked")
}

// Of labels the bead asked-of:<login>, and ask:<role> when role is not empty.
func (a Ask) Of(ctx context.Context, bead, login, role string) error {
	return a.label(ctx, bead, LabelAskedOfPrefix, login, role, "who was asked")
}

func (a Ask) label(ctx context.Context, bead, prefix, login, role, who string) error {
	login = askWord(login)
	if login == "" {
		return fmt.Errorf("mw ask: %s? name a login", who)
	}
	if _, err := a.open(ctx, bead); err != nil {
		return err
	}
	if err := a.Tracker.AddLabel(ctx, bead, prefix+login); err != nil {
		return fmt.Errorf("mw ask: labelling %s: %w", bead, err)
	}
	if err := a.addRole(ctx, bead, role); err != nil {
		return err
	}
	a.say("%s is labelled %s%s", bead, prefix, login)
	return nil
}

func (a Ask) addRole(ctx context.Context, bead, role string) error {
	role = askWord(role)
	if role == "" {
		return nil
	}
	if err := a.Tracker.AddLabel(ctx, bead, LabelAskRolePrefix+role); err != nil {
		return fmt.Errorf("mw ask: labelling %s: %w", bead, err)
	}
	return nil
}

// open reads the bead and refuses one that is closed.
func (a Ask) open(ctx context.Context, bead string) (StoryDetail, error) {
	d, err := a.Tracker.ShowStory(ctx, bead)
	if err != nil {
		return StoryDetail{}, fmt.Errorf("mw ask: reading %s: %w", bead, err)
	}
	if d.Closed() {
		return d, fmt.Errorf("mw ask: %s is closed", bead)
	}
	return d, nil
}

// Waiting labels the bead waiting:others and notes the time it began, once the
// ask is sent. of, when not empty, also labels it asked-of:<of>. A bead already
// waiting keeps the time it first waited since.
func (a Ask) Waiting(ctx context.Context, bead, of, role string) error {
	d, err := a.open(ctx, bead)
	if err != nil {
		return err
	}
	if of = askWord(of); of != "" {
		if err := a.Tracker.AddLabel(ctx, bead, LabelAskedOfPrefix+of); err != nil {
			return fmt.Errorf("mw ask: labelling %s: %w", bead, err)
		}
	} else {
		of = d.AskedOf()
	}
	if err := a.addRole(ctx, bead, role); err != nil {
		return err
	}
	since := a.now().UTC()
	if note, err := a.Notes.Note(ctx, AskWaitingKey(bead)); err == nil && d.WaitingOnOthers() {
		if kept, perr := time.Parse(time.RFC3339, strings.TrimSpace(note)); perr == nil {
			since = kept.UTC()
		}
	}
	if err := a.Notes.SetNote(ctx, AskWaitingKey(bead), since.Format(time.RFC3339)); err != nil {
		return fmt.Errorf("mw ask: noting when %s began waiting: %w", bead, err)
	}
	if err := a.Tracker.AddLabel(ctx, bead, LabelWaitingOthers); err != nil {
		return fmt.Errorf("mw ask: labelling %s: %w", bead, err)
	}
	if of == "" {
		of = "others"
	}
	a.say("%s is waiting on %s since %s", bead, of, viewClock(since))
	return nil
}

// Done takes the waiting:others label and the time off the bead; who was asked
// stays on it. A bead not waiting is left as it is.
func (a Ask) Done(ctx context.Context, bead string) error {
	d, err := a.Tracker.ShowStory(ctx, bead)
	if err != nil {
		return fmt.Errorf("mw ask: reading %s: %w", bead, err)
	}
	if !d.WaitingOnOthers() {
		a.say("%s was not waiting on others", bead)
		return nil
	}
	if err := a.Tracker.RemoveLabel(ctx, bead, LabelWaitingOthers); err != nil {
		return fmt.Errorf("mw ask: unlabelling %s: %w", bead, err)
	}
	if err := a.Notes.ClearNote(ctx, AskWaitingKey(bead)); err != nil {
		return fmt.Errorf("mw ask: clearing when %s began waiting: %w", bead, err)
	}
	a.say("%s is no longer waiting on others", bead)
	return nil
}
