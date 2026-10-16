package application_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// letGoFixture is an applyFixture whose stories mw-e.1 and mw-e.2 are two
// let-go To do cards the weekly card of the epic mw-e listed.
func letGoFixture(t *testing.T, offered []string) *applyFixture {
	t.Helper()
	f := newApplyFixture(t)
	ctx := context.Background()
	for _, id := range []string{"mw-e.1", "mw-e.2"} {
		mustDo(t, f.tracker.SetStatus(id, application.StatusOpen))
		mustDo(t, f.tracker.SetLabels(id, application.LabelHitl, application.LabelHitlTodo))
		mustDo(t, f.tracker.SetStoryMetadata(ctx, id, map[string]string{application.TodoRowField: application.TodoRowLetGo}))
	}
	listed, _ := json.Marshal([]string{"mw-e.1", "mw-e.2"})
	mustDo(t, f.tracker.SetNote(ctx, application.TodoLetGoKey("mw-e"), string(listed)))
	question, _ := json.Marshal(map[string]any{"txid": "tx-weekly", "options": offered})
	mustDo(t, f.tracker.SetNote(ctx, application.PosternQuestionKey("mw-e"), string(question)))
	return f
}

func (f *applyFixture) answer(t *testing.T, txid, bead, answer string) {
	t.Helper()
	reply, _ := json.Marshal(application.PosternReply{Bead: bead, Answer: answer})
	f.message(t, releaseTapGovernorKey, txid, string(reply))
}

// A Drop tap on a question that never offered it closes nothing, and the Mayor
// is told why.
func TestLetGoDropIsRefusedWhenTheQuestionNeverOfferedIt(t *testing.T) {
	f := letGoFixture(t, []string{"A: Keep them all"})
	f.answer(t, "tx-drop", "mw-e", "B: Drop them all")

	f.apply(t)

	for _, id := range []string{"mw-e.1", "mw-e.2"} {
		if reason := f.tracker.CloseReason(id); reason != "" {
			t.Fatalf("expected %s left open, it was closed: %s", id, reason)
		}
	}
	if subjects := f.subjects(t); len(subjects) != 2 || subjects[1] != "Let go not applied: mw-e" {
		t.Fatalf("expected the answer's mail and a not-applied mail, got %v", subjects)
	}
}

// The words of the weekly card's answers on a card that kept no list of let-go
// cards close nothing: only the weekly card's own question is its.
func TestLetGoTapOnACardWithNoListedCardsChangesNothing(t *testing.T) {
	f := letGoFixture(t, []string{"A: Keep them all", "B: Drop them all"})
	mustDo(t, f.tracker.SetNote(context.Background(), application.TodoLetGoKey("mw-e"), ""))
	f.answer(t, "tx-drop", "mw-e", "B: Drop them all")

	f.apply(t)

	for _, id := range []string{"mw-e.1", "mw-e.2"} {
		if reason := f.tracker.CloseReason(id); reason != "" {
			t.Fatalf("expected %s left open, it was closed: %s", id, reason)
		}
	}
	if subjects := f.subjects(t); len(subjects) != 1 {
		t.Fatalf("expected only the answer's mail, got %v", subjects)
	}
}

// A card the pass has since moved out of let-go is not dropped by the tap.
func TestLetGoDropLeavesACardNoLongerInLetGo(t *testing.T) {
	f := letGoFixture(t, []string{"A: Keep them all", "B: Drop them all"})
	mustDo(t, f.tracker.SetStoryMetadata(context.Background(), "mw-e.2", map[string]string{application.TodoRowField: application.TodoRowLater}))
	f.answer(t, "tx-drop", "mw-e", "B: Drop them all")

	f.apply(t)

	if reason := f.tracker.CloseReason("mw-e.1"); reason != application.TodoLetGoReason {
		t.Fatalf("expected mw-e.1 closed with %q, got %q", application.TodoLetGoReason, reason)
	}
	if reason := f.tracker.CloseReason("mw-e.2"); reason != "" {
		t.Fatalf("expected mw-e.2 left open, it was closed: %s", reason)
	}
}
