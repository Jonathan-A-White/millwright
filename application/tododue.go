package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// The metadata a hitl:todo card carries once it has a due time. todo_due is
// the time, RFC 3339, that the Mayor (mw todo due) or the Governor's phone
// (todo_edit's due) set; todo_reminded is the todo_due the follower last sent
// him a push for, so a card is reminded once for each time it is given.
const (
	TodoDueField      = "todo_due"
	TodoRemindedField = "todo_reminded"
)

// TodoReminderJobName is the follower job that sends the reminders.
const TodoReminderJobName = "todo-remind"

// TodoReminderEvery is how often the follower looks for a card past due.
const TodoReminderEvery = time.Minute

// TodoReminderClass is the class of the push a due card sends. Provisionally
// an alarm, which he hears and which opens the Needs you view; if reminders
// come to have a class of their own, it is this one constant to change.
const TodoReminderClass = "alarm"

// parseTodoDue reads a due time as todo_due holds it.
func parseTodoDue(text string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(text))
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a time in RFC 3339, like 2026-10-15T17:00:00-04:00", strings.TrimSpace(text))
	}
	return at, nil
}

// normalTodoDue is a due time as it is kept: RFC 3339, trimmed, and empty for
// none, which clears it. A time that is not RFC 3339 is an error.
func normalTodoDue(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	at, err := parseTodoDue(text)
	if err != nil {
		return "", err
	}
	return at.Format(time.RFC3339), nil
}

// TodoDue sets or clears the due time of a card: an open bead labelled
// hitl:todo. Every refusal comes before the write.
type TodoDue struct {
	Tracker WorkTracker

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run gives the card id the due time when, as RFC 3339, or clears it when
// when is empty. It reports the due time the card now has, "" for none.
func (t TodoDue) Run(ctx context.Context, id, when string) (string, error) {
	id = strings.TrimSpace(id)
	due, err := normalTodoDue(when)
	if err != nil {
		return "", fmt.Errorf("mw todo due: %w", err)
	}
	found, err := t.Tracker.ShowBeads(ctx, []string{id})
	if err != nil {
		return "", fmt.Errorf("mw todo due: reading %s: %w", id, err)
	}
	if len(found) == 0 {
		return "", fmt.Errorf("mw todo due: %s is not a bead", id)
	}
	if why := refuseNotACard(found[0]); why != "" {
		return "", fmt.Errorf("mw todo due: %s: %s", id, why)
	}
	if err := t.Tracker.SetStoryMetadata(ctx, id, map[string]string{TodoDueField: due}); err != nil {
		return "", fmt.Errorf("mw todo due: %w", err)
	}
	if t.Out != nil {
		if due == "" {
			fmt.Fprintf(t.Out, "%s: no due time\n", id)
		} else {
			fmt.Fprintf(t.Out, "%s: due %s\n", id, due)
		}
	}
	return due, nil
}

// TodoReminder sends the Governor one push for each hitl:todo card that is
// past its due time and has not been reminded of it. It is the follower's
// todo-remind job, run every TodoReminderEvery.
type TodoReminder struct {
	Tracker WorkTracker
	Sender  PosternSender

	// Now is the clock; the zero value reads the real one.
	Now func() time.Time

	// Err is where a card passed over is said. A nil Err says nothing.
	Err io.Writer
}

// Run reads the open cards and reminds him of each one due. A card is marked
// reminded before the push is sent, so a pass that dies half-way never sends
// twice; a push that fails takes the mark off again, so the next pass tries it
// once more. It reports how many pushes it sent, and an error only for what
// stops the pass or what a push failed with.
func (r TodoReminder) Run(ctx context.Context) (int, error) {
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	cards, err := r.Tracker.ReadyWithLabel(ctx, LabelHitlTodo)
	if err != nil {
		return 0, fmt.Errorf("reading the To do cards: %w", err)
	}
	sent := 0
	var failed []string
	for _, card := range cards {
		if card.IsEpic || card.Closed() || strings.TrimSpace(card.TodoDue) == "" {
			continue
		}
		due, err := parseTodoDue(card.TodoDue)
		if err != nil {
			r.say("%s: its todo_due is passed over: %v\n", card.Story.ID, err)
			continue
		}
		if due.After(now) || strings.TrimSpace(card.TodoReminded) == strings.TrimSpace(card.TodoDue) {
			continue
		}
		if err := r.remind(ctx, card); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", card.Story.ID, err))
			continue
		}
		sent++
	}
	if len(failed) > 0 {
		return sent, fmt.Errorf("a reminder was not sent: %s", strings.Join(failed, "; "))
	}
	return sent, nil
}

// remind marks card reminded, sends the push, and takes the mark off again
// when the push fails.
func (r TodoReminder) remind(ctx context.Context, card StoryDetail) error {
	id := card.Story.ID
	if err := r.Tracker.SetStoryMetadata(ctx, id, map[string]string{TodoRemindedField: card.TodoDue}); err != nil {
		return fmt.Errorf("marking it reminded: %w", err)
	}
	title := strings.TrimSpace(card.Story.Title)
	_, err := r.Sender.Run(ctx, PosternSendRequest{
		Class: TodoReminderClass, Text: "Due now: " + title, Summary: "Due: " + title,
		Thread: id, Recorded: true,
	})
	if err != nil {
		if clearErr := r.Tracker.SetStoryMetadata(ctx, id, map[string]string{TodoRemindedField: ""}); clearErr != nil {
			return fmt.Errorf("%w (and the mark could not be taken off: %v)", err, clearErr)
		}
		return err
	}
	return nil
}

func (r TodoReminder) say(format string, args ...any) {
	if r.Err != nil {
		fmt.Fprintf(r.Err, format, args...)
	}
}

// TodoReminderJob is the job the follower runs for TodoReminder: no event
// springs it, it runs every d, on the follower's own clock.
func TodoReminderJob(every time.Duration, run func(context.Context) error) SpringJob {
	return ClockJob(TodoReminderJobName, every, run)
}
