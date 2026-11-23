package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// guardStdin is the JSON Claude Code gives a Stop hook on its standard input:
// the shape the Mayor saw on 2026-11-23 (mw-gq6.1676), with the fields the guard
// reads and the ones it does not.
func guardStdin(session string, active bool) string {
	raw, _ := json.Marshal(map[string]any{
		"session_id": session, "hook_event_name": "Stop", "stop_hook_active": active,
		"cwd": "/home/jwhite/.mw-worktrees/mw-gq6.1", "last_assistant_message": "Done.",
	})
	return string(raw)
}

// guardWorld is a story with a poured formula of three steps, the first closed:
// two are open.
type guardWorld struct {
	tracker *apptest.FakeTracker
	files   *vault.Vault
	steps   []application.FormulaStep
}

func newGuardWorld(t *testing.T) *guardWorld {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddStory("", domain.Story{ID: "mw-gq6.1", Title: "The story", Overrides: domain.Path{Rig: "millwright"}})
	tracker.AddFormula("tdd-feature",
		application.FormulaStep{Title: "Understand the story"},
		application.FormulaStep{Title: "Write the failing test"},
		application.FormulaStep{Title: "Run the full build"},
	)
	molecule, err := tracker.PourFormula(context.Background(), "tdd-feature", "mw-gq6.1", "The story", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.1", map[string]string{application.MoleculeField: molecule.RootID}); err != nil {
		t.Fatal(err)
	}
	tracker.CloseStep(molecule.Steps[0].ID)
	return &guardWorld{tracker: tracker, files: vault.New(t.TempDir()), steps: molecule.Steps}
}

func (w *guardWorld) guard(out *strings.Builder) application.OpenStepsGuard {
	return application.OpenStepsGuard{Tracker: w.tracker, Notes: w.tracker, Records: w.files, Out: out, Vault: "/home/me/vault"}
}

func TestOpenStepsGuardBlocksAndNamesEachStep(t *testing.T) {
	w := newGuardWorld(t)
	var said strings.Builder

	blocked, err := w.guard(&said).Run(context.Background(), "mw-gq6.1", strings.NewReader(guardStdin("sess-1", false)))
	if err != nil {
		t.Fatalf("the guard failed: %v", err)
	}
	if !blocked {
		t.Fatalf("the guard let a session end with two steps open")
	}
	for _, step := range w.steps[1:] {
		if want := step.ID + " · " + step.Title; !strings.Contains(said.String(), want) {
			t.Errorf("the message does not name %q:\n%s", want, said.String())
		}
	}
	if strings.Contains(said.String(), w.steps[0].ID) {
		t.Errorf("the message names the closed step %s:\n%s", w.steps[0].ID, said.String())
	}
	for _, want := range []string{"close it", "bd -C /home/me/vault close " + w.steps[1].ID, "mark it the Governor's", "mw step governor mw-gq6.1 " + w.steps[1].ID, "reason"} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("the message does not say %q:\n%s", want, said.String())
		}
	}
}

func TestOpenStepsGuardBlocksAtMostThreeTimes(t *testing.T) {
	w := newGuardWorld(t)
	for try := 1; try <= application.OpenStepsGuardMaxBlocks; try++ {
		var said strings.Builder
		blocked, err := w.guard(&said).Run(context.Background(), "mw-gq6.1", strings.NewReader(guardStdin("sess-1", try > 1)))
		if err != nil || !blocked {
			t.Fatalf("end %d of the session: blocked %v, err %v; want blocked", try, blocked, err)
		}
		if !strings.Contains(said.String(), "block "+string(rune('0'+try))+" of 3") {
			t.Errorf("end %d does not say which block it is:\n%s", try, said.String())
		}
	}
	var said strings.Builder
	blocked, err := w.guard(&said).Run(context.Background(), "mw-gq6.1", strings.NewReader(guardStdin("sess-1", true)))
	if err != nil || blocked || said.Len() != 0 {
		t.Errorf("the fourth end: blocked %v, err %v, said %q; want it let through, saying nothing", blocked, err, said.String())
	}

	// Another session of the same story is a new count.
	blocked, err = w.guard(&said).Run(context.Background(), "mw-gq6.1", strings.NewReader(guardStdin("sess-2", false)))
	if err != nil || !blocked {
		t.Errorf("a second session's first end: blocked %v, err %v; want blocked", blocked, err)
	}
}

func TestOpenStepsGuardPassesWhenEveryStepIsClosed(t *testing.T) {
	w := newGuardWorld(t)
	w.tracker.CloseStep(w.steps[1].ID)
	w.tracker.CloseStep(w.steps[2].ID)
	var said strings.Builder
	blocked, err := w.guard(&said).Run(context.Background(), "mw-gq6.1", strings.NewReader(guardStdin("sess-1", false)))
	if err != nil || blocked || said.Len() != 0 {
		t.Errorf("blocked %v, err %v, said %q; want a pass saying nothing", blocked, err, said.String())
	}
}

func TestOpenStepsGuardReadsNoStdinAsASessionOfItsOwn(t *testing.T) {
	w := newGuardWorld(t)
	var said strings.Builder
	blocked, err := w.guard(&said).Run(context.Background(), "mw-gq6.1", strings.NewReader("not json"))
	if err != nil || !blocked {
		t.Errorf("blocked %v, err %v; want blocked: the hook's input is not what decides a step is open", blocked, err)
	}
}

func TestOpenStepsGuardLetsASessionEndWhenItCannotReadTheSteps(t *testing.T) {
	w := newGuardWorld(t)
	w.tracker.Err = context.DeadlineExceeded
	var said strings.Builder
	blocked, err := w.guard(&said).Run(context.Background(), "mw-gq6.1", strings.NewReader(guardStdin("sess-1", false)))
	if err == nil || blocked {
		t.Errorf("blocked %v, err %v; want an error and no block: a session is not held for what mw could not read", blocked, err)
	}
}

func TestAStepMarkedTheGovernorsLetsTheGuardPassAndIsNamedWithItsReason(t *testing.T) {
	w := newGuardWorld(t)
	ctx := context.Background()
	mark := application.GovernorsStepMark{Tracker: w.tracker, Notes: w.tracker, Now: func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) }}

	if _, err := mark.Run(ctx, "mw-gq6.1", w.steps[1].ID, "  "); err == nil {
		t.Errorf("a mark with no reason was kept")
	}
	if _, err := mark.Run(ctx, "mw-gq6.1", "no-such-step", "he signs in"); err == nil {
		t.Errorf("a mark of a step the story has no open step of was kept")
	}
	if _, err := mark.Run(ctx, "mw-gq6.1", w.steps[0].ID, "he signs in"); err == nil {
		t.Errorf("a mark of a step already closed was kept")
	}

	if _, err := mark.Run(ctx, "mw-gq6.1", w.steps[1].ID, "only he can sign in to the bank"); err != nil {
		t.Fatalf("marking the step: %v", err)
	}
	var said strings.Builder
	blocked, err := w.guard(&said).Run(ctx, "mw-gq6.1", strings.NewReader(guardStdin("sess-1", false)))
	if err != nil || !blocked || strings.Contains(said.String(), w.steps[1].ID) || !strings.Contains(said.String(), w.steps[2].ID) {
		t.Errorf("one step marked, one open: blocked %v, err %v, said:\n%s\nwant it to name only %s", blocked, err, said.String(), w.steps[2].ID)
	}

	if _, err := mark.Run(ctx, "mw-gq6.1", w.steps[2].ID, "only he holds the key"); err != nil {
		t.Fatalf("marking the second step: %v", err)
	}
	said.Reset()
	blocked, err = w.guard(&said).Run(ctx, "mw-gq6.1", strings.NewReader(guardStdin("sess-1", false)))
	if err != nil || blocked {
		t.Errorf("both steps marked: blocked %v, err %v; want a pass", blocked, err)
	}

	marked, err := application.GovernorsSteps(ctx, w.tracker, "mw-gq6.1")
	if err != nil || len(marked) != 2 || marked[w.steps[1].ID].Reason != "only he can sign in to the bank" {
		t.Errorf("the marks kept: %+v, err %v", marked, err)
	}
}

func TestAStoryWithAMarkedStepIsAHandsWaitInTheReport(t *testing.T) {
	tracker, files := apptest.NewFakeTracker(), vault.New(t.TempDir())
	openStepsRows(t, tracker, files)
	raw, _ := json.Marshal(application.GovernorsStep{Story: "os-3", Step: "mol.2", Reason: "he signs in", At: "2026-10-17T09:00:00Z"})
	if err := tracker.SetNote(context.Background(), application.GovernorsStepKey("os-3", "mol.2"), string(raw)); err != nil {
		t.Fatal(err)
	}

	_, printed := runOpenStepsReport(t, tracker, files, tracker)

	if line := "open-steps 5: left open 4 · hands waiting on him 1"; !strings.Contains(printed, line) {
		t.Errorf("the report does not say %q:\n%s", line, printed)
	}
}
