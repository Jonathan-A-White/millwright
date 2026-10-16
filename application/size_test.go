package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// size (mw-1ljpo9.3) is printed beside the model by mw brief, and by mw status
// under the story's id; a story without one prints neither.
func TestBriefShowsTheSizeBesideTheModel(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	story := domain.Story{ID: "mw-gq6.30", Title: "A sized story", Overrides: domain.Path{Size: domain.SizeS}}
	tracker.AddStory("mw-gq6", story)
	storyOn(t, tracker, "mw-gq6.31", "An unsized story", "vps")

	var out strings.Builder
	if _, err := (application.Brief{Tracker: tracker, Out: &out}).Run(context.Background(), "mw-gq6"); err != nil {
		t.Fatalf("briefing: %v", err)
	}
	if !strings.Contains(out.String(), "A sized story · mw-gq6.30 · P") || !strings.Contains(out.String(), "opus · size S") {
		t.Errorf("expected the brief to say size S after the model, got:\n%s", out.String())
	}
	if strings.Count(out.String(), " · size ") != 1 {
		t.Errorf("expected only the sized story to carry a size, got:\n%s", out.String())
	}
}

func TestStatusShowsTheSizeOfAStory(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.30", Title: "A sized story", Overrides: domain.Path{Host: "vps", Size: domain.SizeL}})
	storyOn(t, tracker, "mw-gq6.31", "An unsized story", "vps")

	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder", Now: func() time.Time { return statusNow }}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	printed := report.String()
	if !strings.Contains(printed, "mw-gq6.30 · millwright · size L") {
		t.Errorf("expected the sized story's row to say size L, got:\n%s", printed)
	}
	if strings.Contains(printed, "mw-gq6.31 · millwright · size") {
		t.Errorf("expected no size on the unsized story, got:\n%s", printed)
	}
}

func TestPathSetSizeThenPathShowPrintsIt(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	if _, err := (application.PathSet{Tracker: tracker}).Run(context.Background(), "mw-gq6.2", "", []string{"size=S"}); err != nil {
		t.Fatalf("setting size=S: %v", err)
	}
	var out strings.Builder
	if _, err := (application.PathShow{Tracker: tracker, Out: &out}).Run(context.Background(), "mw-gq6.2"); err != nil {
		t.Fatalf("showing the path: %v", err)
	}
	if !strings.Contains(out.String(), "\nsize=S\n") {
		t.Errorf("expected path show to print size=S, got:\n%s", out.String())
	}
}

func TestPathSetRefusesASizeThatIsNotOneAndWritesNothing(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	writes := tracker.Writes()
	_, err := application.PathSet{Tracker: tracker}.Run(context.Background(), "mw-gq6.2", "", []string{"size=huge", "model=sonnet"})
	if err == nil || !strings.Contains(err.Error(), "huge") || !strings.Contains(err.Error(), "XS, S, M, L, XL") {
		t.Fatalf("expected size=huge to be refused naming the sizes, got %v", err)
	}
	if tracker.Writes() != writes {
		t.Errorf("expected nothing written, the tracker took %d writes", tracker.Writes()-writes)
	}
}

func TestPathSetFromASiblingDoesNotCopyItsSize(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.3", Title: "sized sibling",
		Overrides: domain.Path{Rig: "millwright", Formula: "chore", Size: domain.SizeXL}})
	if _, err := (application.PathSet{Tracker: tracker}).Run(context.Background(), "mw-gq6.2", "mw-gq6.3", nil); err != nil {
		t.Fatalf("copying the path: %v", err)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2"); detail.Story.Overrides.Size != "" {
		t.Errorf("expected the sibling's size not to be copied, got %q", detail.Story.Overrides.Size)
	}
}
