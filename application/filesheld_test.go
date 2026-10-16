package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// storyChanging files a story under the epic expecting to change files.
func storyChanging(t *testing.T, add func(string, domain.Story), id, files string) {
	t.Helper()
	story := domain.Story{ID: id, Title: "A story"}
	if err := story.Overrides.Set("files", files); err != nil {
		t.Fatalf("setting the files of %s: %v", id, err)
	}
	add("mw-gq6", story)
}

func TestDispatchKeepsWhenAStoryWasFirstHeldByFiles(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap, dispatch.Memory = 3, tracker
	first := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	clock := first
	dispatch.Now = func() time.Time { return clock }
	storyChanging(t, tracker.AddStory, "mw-gq6.1", "src/Reader.tsx")
	storyChanging(t, tracker.AddStory, "mw-gq6.2", "src/Preface.tsx,src/Reader.tsx")

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	key := application.FilesHeldKey("vps")
	held, _ := tracker.Note(ctx, key)
	if want := "mw-gq6.2 " + first.Format(time.RFC3339); held != want {
		t.Fatalf("expected the note %s to read %q, got %q", key, want, held)
	}

	// Held again an hour on, it was held since the first time, and the note is
	// not written again.
	clock = first.Add(time.Hour)
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching again: %v", err)
	}
	if again, _ := tracker.Note(ctx, key); again != held {
		t.Errorf("expected the note to keep %q, got %q", held, again)
	}
	if writes := tracker.NoteWrites(key); writes != 1 {
		t.Errorf("expected the note written once, got %d writes", writes)
	}

	// Its files narrowed so it no longer shares, it is started, and no longer held.
	if err := tracker.SetStoryMetadata(ctx, "mw-gq6.2", map[string]string{"files": "src/Preface.tsx"}); err != nil {
		t.Fatalf("narrowing the files: %v", err)
	}
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching a third time: %v", err)
	}
	if after, _ := tracker.Note(ctx, key); after != "" {
		t.Errorf("expected the note cleared, got %q", after)
	}
}

func TestADryRunKeepsNoNoteOfWhatFilesHold(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap, dispatch.Memory, dispatch.DryRun = 3, tracker, true
	storyChanging(t, tracker.AddStory, "mw-gq6.1", "src/Reader.tsx")
	storyChanging(t, tracker.AddStory, "mw-gq6.2", "src/Reader.tsx")

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if writes := tracker.NoteWrites(application.FilesHeldKey("vps")); writes != 0 {
		t.Errorf("expected a dry run to write no note, got %d writes", writes)
	}
}

func TestStatusNamesAReadyStoryHeldByFilesPastSixHours(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyChanging(t, tracker.AddStory, "mw-gq6.2", "src/Reader.tsx")
	storyChanging(t, tracker.AddStory, "mw-gq6.3", "src/Preface.tsx")
	since := func(ago time.Duration) string { return statusNow.Add(-ago).Format(time.RFC3339) }
	if err := tracker.SetNote(context.Background(), application.FilesHeldKey("vps"),
		"mw-gq6.2 "+since(3*24*time.Hour+2*time.Hour)+"\nmw-gq6.3 "+since(5*time.Hour)); err != nil {
		t.Fatalf("keeping the note: %v", err)
	}

	printed := otherHostStatus(t, tracker).String()
	if want := "held by files for 3d02h"; !strings.Contains(printed, want) {
		t.Errorf("expected %q in the report, got:\n%s", want, printed)
	}
	if strings.Count(printed, "held by files for") != 1 {
		t.Errorf("expected only the story held past 6h named, got:\n%s", printed)
	}
}
