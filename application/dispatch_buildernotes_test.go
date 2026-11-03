package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.892: a note to the Builder that found no session to be typed into is
// in the boot file of the story's next attempt, and an ordinary comment is not.
func TestTheNextAttemptIsBootedWithTheNotesToItsBuilder(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, dir := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	if err := tracker.CommentOnStory(ctx, "mw-gq6.1", "an ordinary word that is not for the Builder"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.CommentOnStory(ctx, "mw-gq6.1", application.NoteToBuilderMark+" Never touch the lxx-morph list."); err != nil {
		t.Fatal(err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 1 {
		t.Fatalf("expected the story started, got %+v", report)
	}

	boot, err := os.ReadFile(filepath.Join(dir, "runs", "mw-gq6.1", application.BootFileName))
	if err != nil {
		t.Fatalf("reading the boot file: %v", err)
	}
	if !strings.Contains(string(boot), "Never touch the lxx-morph list.") {
		t.Fatalf("expected the note in the boot file, got:\n%s", boot)
	}
	if strings.Contains(string(boot), "an ordinary word") {
		t.Fatalf("expected an ordinary comment left out of the boot file, got:\n%s", boot)
	}
}
