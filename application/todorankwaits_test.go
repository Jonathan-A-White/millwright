package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

type rankRuns struct{ files map[string]string }

func (r *rankRuns) PutRunFile(_ context.Context, dir, name, contents string) (string, error) {
	r.files[dir+"/"+name] = contents
	return "runs/" + dir + "/" + name, nil
}

// rankBoard is a To do list with a card that stands in Do now and waits on
// an unfinished story, and a card that waits on nothing.
type rankBoard struct {
	t       *testing.T
	tracker *apptest.FakeTracker
	grinder *apptest.FakeGrinder
	runs    *rankRuns
	out     bytes.Buffer
}

func aRankBoard(t *testing.T) *rankBoard {
	t.Helper()
	b := &rankBoard{t: t, tracker: apptest.NewFakeTracker(), grinder: &apptest.FakeGrinder{}, runs: &rankRuns{files: map[string]string{}}}
	b.tracker.AddEpic("mw-list", domain.Path{})
	b.card("mw-list.1", "Waits on a story", "now")
	b.card("mw-list.2", "Free to do", "")
	b.tracker.AddStory("mw-list", domain.Story{ID: "mw-blocker", Title: "The story it waits on"})
	if err := b.tracker.AddBlocker(context.Background(), "mw-list.1", "mw-blocker"); err != nil {
		t.Fatal(err)
	}
	return b
}

func (b *rankBoard) card(id, title, row string) {
	b.tracker.AddStory("mw-list", domain.Story{ID: id, Title: title})
	if err := b.tracker.SetLabels(id, application.LabelHitl, application.LabelHitlTodo); err != nil {
		b.t.Fatal(err)
	}
	if row != "" {
		if err := b.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.TodoRowField: row, application.TodoRowWhyField: "was ready"}); err != nil {
			b.t.Fatal(err)
		}
	}
}

func (b *rankBoard) modelSays(rows ...application.TodoRankRow) {
	answer, err := json.Marshal(map[string]any{"cards": rows})
	if err != nil {
		b.t.Fatal(err)
	}
	b.grinder.Result = application.SessionResult{Subtype: "success", Answer: answer}
}

func (b *rankBoard) rank() error {
	pass := application.TodoRankPass{
		Tracker: b.tracker, Closed: b.tracker, Graph: b.tracker, Grinder: b.grinder, Runs: b.runs,
		Model: "sonnet", Effort: "medium", Zone: time.UTC,
		Now: func() time.Time { return time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC) }, Out: &b.out,
	}
	b.out.Reset()
	_, err := pass.Run(context.Background(), false)
	return err
}

func (b *rankBoard) row(id string) (string, string) {
	meta := b.tracker.Metadata(id)
	return meta[application.TodoRowField], meta[application.TodoRowWhyField]
}

func TestRankLeavesACardThatWaitsOnAnUnfinishedStoryOutOfNowAndSaysSo(t *testing.T) {
	b := aRankBoard(t)
	b.modelSays(application.TodoRankRow{Bead: "mw-list.2", Row: "now", Why: "nothing in the way"})

	if err := b.rank(); err != nil {
		t.Fatalf("ranking: %v", err)
	}
	row, why := b.row("mw-list.1")
	if row == application.TodoRowNow {
		t.Fatalf("mw-list.1 waits on mw-blocker but stands in Do now")
	}
	if row != application.TodoRowLater || why != "left out of Now: waits on mw-blocker" {
		t.Errorf("expected mw-list.1 later because %q, got %q because %q", "left out of Now: waits on mw-blocker", row, why)
	}
	if row, _ := b.row("mw-list.2"); row != application.TodoRowNow {
		t.Errorf("expected the free card in now, got %q", row)
	}
	if !strings.Contains(b.out.String(), "left out of Now: waits on mw-blocker") {
		t.Errorf("expected the printed rows to say why, got:\n%s", b.out.String())
	}
	var logged string
	for name, contents := range b.runs.files {
		logged += name + contents
	}
	if !strings.Contains(logged, "left out of Now: waits on mw-blocker") {
		t.Errorf("expected the pass log to say why, got:\n%s", logged)
	}
}

func TestRankLeavesAnInProgressBlockersCardOutOfNow(t *testing.T) {
	b := aRankBoard(t)
	if err := b.tracker.ClaimStory(context.Background(), "mw-blocker"); err != nil {
		t.Fatal(err)
	}
	b.modelSays(application.TodoRankRow{Bead: "mw-list.2", Row: "now", Why: "nothing in the way"})

	if err := b.rank(); err != nil {
		t.Fatalf("ranking: %v", err)
	}
	if row, _ := b.row("mw-list.1"); row == application.TodoRowNow {
		t.Error("mw-list.1 waits on a story in progress but stands in Do now")
	}
}

func TestRankLeavesAWaitingCardAloneThatIsNotInNow(t *testing.T) {
	b := aRankBoard(t)
	if err := b.tracker.SetStoryMetadata(context.Background(), "mw-list.1", map[string]string{application.TodoRowField: application.TodoRowMakeTime, application.TodoRowWhyField: "his pick"}); err != nil {
		t.Fatal(err)
	}
	b.modelSays(application.TodoRankRow{Bead: "mw-list.2", Row: "now", Why: "nothing in the way"})

	if err := b.rank(); err != nil {
		t.Fatalf("ranking: %v", err)
	}
	if row, why := b.row("mw-list.1"); row != application.TodoRowMakeTime || why != "his pick" {
		t.Errorf("expected the card left as it was, got %q because %q", row, why)
	}
}

func TestRankMayPutTheCardInNowOnceItsBlockerCloses(t *testing.T) {
	b := aRankBoard(t)
	b.modelSays(application.TodoRankRow{Bead: "mw-list.2", Row: "now", Why: "nothing in the way"})
	if err := b.rank(); err != nil {
		t.Fatalf("ranking: %v", err)
	}
	if err := b.tracker.CloseStory(context.Background(), "mw-blocker", "done"); err != nil {
		t.Fatal(err)
	}
	b.modelSays(
		application.TodoRankRow{Bead: "mw-list.1", Row: "now", Why: "its blocker is done"},
		application.TodoRankRow{Bead: "mw-list.2", Row: "later", Why: "can wait"},
	)

	if err := b.rank(); err != nil {
		t.Fatalf("ranking again: %v", err)
	}
	if row, why := b.row("mw-list.1"); row != application.TodoRowNow || why != "its blocker is done" {
		t.Errorf("expected mw-list.1 in now, got %q because %q", row, why)
	}
}

func TestRankWithOnlyWaitingCardsAsksNoModel(t *testing.T) {
	b := aRankBoard(t)
	if err := b.tracker.CloseStory(context.Background(), "mw-list.2", "done"); err != nil {
		t.Fatal(err)
	}

	if err := b.rank(); err != nil {
		t.Fatalf("ranking: %v", err)
	}
	if row, _ := b.row("mw-list.1"); row != application.TodoRowLater {
		t.Errorf("expected mw-list.1 later, got %q", row)
	}
	if n := len(b.grinder.Calls()); n != 0 {
		t.Errorf("expected no model call with no free card, got %d", n)
	}
}
