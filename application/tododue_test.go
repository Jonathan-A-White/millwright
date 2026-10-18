package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A tracker that reads the cards that carry a due time in one cheap read, and
// counts the listings of ready cards the reminder asks for besides.
type dueReadingTracker struct {
	*apptest.FakeTracker
	cards    []application.TodoDueCard
	readErr  error
	listings int
}

func (d *dueReadingTracker) TodoDueCards(context.Context) ([]application.TodoDueCard, error) {
	return d.cards, d.readErr
}

func (d *dueReadingTracker) ReadyWithLabel(ctx context.Context, label string) ([]application.StoryDetail, error) {
	d.listings++
	return d.FakeTracker.ReadyWithLabel(ctx, label)
}

func aTodoReminderOverTheCards(t *testing.T, now time.Time, cards ...application.TodoDueCard) (*dueReadingTracker, *apptest.FakePosternSender, application.TodoReminder) {
	t.Helper()
	fake := apptest.NewFakeTracker()
	fake.AddEpic("mw-todo", domain.Path{})
	tracker := &dueReadingTracker{FakeTracker: fake, cards: cards}
	for _, card := range cards {
		fake.AddStory("mw-todo", domain.Story{ID: card.ID, Title: "Card " + card.ID})
		if err := fake.SetLabels(card.ID, application.LabelHitl, application.LabelHitlTodo); err != nil {
			t.Fatal(err)
		}
		if err := fake.SetStoryMetadata(context.Background(), card.ID, map[string]string{application.TodoDueField: card.Due}); err != nil {
			t.Fatal(err)
		}
	}
	sender := &apptest.FakePosternSender{}
	return tracker, sender, application.TodoReminder{Tracker: tracker, Sender: sender, Now: func() time.Time { return now }}
}

// The follower looks for a card past due every minute, and listing the ready
// cards each time started a bd (several server connections) a minute for as
// long as the follower ran (mw-gq6.608): with no card due, nothing is listed.
func TestAReminderPassWithNoCardDueListsNoReadyCards(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	tracker, sender, reminder := aTodoReminderOverTheCards(t, now,
		application.TodoDueCard{ID: "mw-todo.1", Due: "2026-10-14T10:00:00Z"},
		application.TodoDueCard{ID: "mw-todo.2", Due: "2026-10-14T08:00:00Z", Reminded: "2026-10-14T08:00:00Z"},
	)

	sent, err := reminder.Run(context.Background())
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if sent != 0 || len(sender.Sent()) != 0 {
		t.Errorf("expected no reminder, sent %d", sent)
	}
	if tracker.listings != 0 {
		t.Errorf("expected no listing of the ready cards, got %d", tracker.listings)
	}
}

func TestAReminderPassWithACardDueSendsItOnceAndListsTheReadyCards(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	tracker, sender, reminder := aTodoReminderOverTheCards(t, now,
		application.TodoDueCard{ID: "mw-todo.1", Due: "2026-10-14T08:00:00Z"},
	)

	sent, err := reminder.Run(context.Background())
	if err != nil || sent != 1 {
		t.Fatalf("expected one reminder, sent %d, err %v", sent, err)
	}
	if tracker.listings != 1 {
		t.Errorf("expected the ready cards listed once, got %d", tracker.listings)
	}
	if got := sender.Sent(); len(got) != 1 || got[0].Thread != "mw-todo.1" {
		t.Errorf("expected the reminder threaded on mw-todo.1, got %+v", got)
	}
}

func TestAReminderPassWhoseCheapReadFailsListsTheReadyCardsAsBefore(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	tracker, sender, reminder := aTodoReminderOverTheCards(t, now,
		application.TodoDueCard{ID: "mw-todo.1", Due: "2026-10-14T08:00:00Z"},
	)
	tracker.readErr = errors.New("no connection")

	sent, err := reminder.Run(context.Background())
	if err != nil || sent != 1 || len(sender.Sent()) != 1 {
		t.Fatalf("expected one reminder, sent %d, err %v", sent, err)
	}
	if tracker.listings != 1 {
		t.Errorf("expected the ready cards listed once, got %d", tracker.listings)
	}
}

func TestAReminderPassSaysACardWhoseDueIsNotATimeAndListsNothingForIt(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	tracker, _, reminder := aTodoReminderOverTheCards(t, now,
		application.TodoDueCard{ID: "mw-todo.1", Due: "tomorrow-ish"},
	)
	var said strings.Builder
	reminder.Err = &said

	if _, err := reminder.Run(context.Background()); err != nil {
		t.Fatalf("running: %v", err)
	}
	if tracker.listings != 0 {
		t.Errorf("expected no listing, got %d", tracker.listings)
	}
	if !strings.Contains(said.String(), "mw-todo.1: its todo_due is passed over") {
		t.Errorf("expected the card passed over to be said, got %q", said.String())
	}
}
