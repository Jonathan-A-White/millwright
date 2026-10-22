package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestDispatchKeepsWhenAStoryWasFirstPassedForARigNotCheckedOut(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap, dispatch.Memory = 3, tracker
	dispatch.Rigs = map[string]string{"elsewhere": "/rigs/elsewhere"}
	first := time.Date(2026, 10, 22, 8, 40, 0, 0, time.UTC)
	clock := first
	dispatch.Now = func() time.Time { return clock }
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	key := application.HostPassedKey("vps")
	kept, _ := tracker.Note(ctx, key)
	passed := application.ParseHostPassed(kept)
	if len(passed) != 1 || passed[0].Story != "mw-gq6.1" || passed[0].Gate != "rig-not-checked-out" ||
		!passed[0].Since.Equal(first) || !strings.Contains(passed[0].Why, "not checked out on vps") {
		t.Fatalf("expected the note %s to hold mw-gq6.1 first passed at %s, got %q", key, first, kept)
	}

	// Passed again an hour on, it was passed since the first time, and the note
	// is not written again.
	clock = first.Add(time.Hour)
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching again: %v", err)
	}
	if again, _ := tracker.Note(ctx, key); again != kept {
		t.Errorf("expected the note to keep %q, got %q", kept, again)
	}
	if writes := tracker.NoteWrites(key); writes != 1 {
		t.Errorf("expected the note written once, got %d writes", writes)
	}

	// The rig checked out, the story is started and the note is taken off.
	dispatch.Rigs = map[string]string{"millwright": "/rigs/millwright"}
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching a third time: %v", err)
	}
	if after, _ := tracker.Note(ctx, key); after != "" {
		t.Errorf("expected the note cleared, got %q", after)
	}
}

func TestDispatchKeepsNoNoteOfAStoryAnotherGatePassedOver(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap, dispatch.Memory = 1, tracker
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "A second"})

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if writes := tracker.NoteWrites(application.HostPassedKey("vps")); writes != 0 {
		t.Errorf("expected a story passed for the cap to leave no note, got %d writes", writes)
	}
}

func TestADryRunKeepsNoNoteOfWhatThisHostPassed(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap, dispatch.Memory, dispatch.DryRun = 3, tracker, true
	dispatch.Rigs = map[string]string{}
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if writes := tracker.NoteWrites(application.HostPassedKey("vps")); writes != 0 {
		t.Errorf("expected a dry run to write no note, got %d writes", writes)
	}
}
