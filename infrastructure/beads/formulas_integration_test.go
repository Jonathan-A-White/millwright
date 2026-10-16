//go:build beads_integration

package beads_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// A real bd's poured formula, read the way mw tidy reads it (mw-gq6.438): a
// closed story's formula, whose first step its session closed, is found with
// the story read off that closed step; a running story's formula is named by
// the story that records it; and closing what tidy closes leaves nothing open.
func TestOpenFormulasReadsAPouredFormulasStoryAndClosesIt(t *testing.T) {
	t.Parallel()
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	installFormula(t, vault, "tdd-feature")
	gateway := beads.New(vault)

	ended := bdRun(t, vault, beads.Program, "create", "An ended story", "--silent")
	running := bdRun(t, vault, beads.Program, "create", "A running story", "--silent")
	pour := func(story string) application.Molecule {
		molecule, err := gateway.PourFormula(ctx, "tdd-feature", story, "A story", false)
		if err != nil {
			t.Fatalf("pouring for %s: %v", story, err)
		}
		if err := gateway.SetStoryMetadata(ctx, story, map[string]string{application.MoleculeField: molecule.RootID}); err != nil {
			t.Fatalf("recording the molecule of %s: %v", story, err)
		}
		return molecule
	}
	endedFormula, runningFormula := pour(ended), pour(running)
	bdRun(t, vault, beads.Program, "close", endedFormula.Steps[0].ID, "--reason", "understood")
	bdRun(t, vault, beads.Program, "close", ended, "--reason", "landed")

	open, err := gateway.OpenFormulas(ctx)
	if err != nil {
		t.Fatalf("reading the open formulas: %v", err)
	}
	found := map[string]application.PouredFormula{}
	for _, formula := range open {
		found[formula.Root] = formula
	}
	if got := found[endedFormula.RootID]; got.Story != ended || got.LiveStory != "" || !got.RootOpen || len(got.Steps) != len(endedFormula.Steps)-1 {
		t.Fatalf("the ended story's formula read as %+v, want its story %s, its root and %d steps open", got, ended, len(endedFormula.Steps)-1)
	}
	if got := found[runningFormula.RootID]; got.LiveStory != running || len(got.Steps) != len(runningFormula.Steps) {
		t.Fatalf("the running story's formula read as %+v, want it recorded by %s with every step open", got, running)
	}

	for _, id := range append(found[endedFormula.RootID].Steps, endedFormula.RootID) {
		if err := gateway.CloseFormulaBead(ctx, id, "Tidied by mw tidy: its story is closed"); err != nil {
			t.Fatalf("closing %s: %v", id, err)
		}
	}
	if err := gateway.CloseFormulaBead(ctx, running, "no"); err == nil {
		t.Errorf("expected the running story's own id to be refused")
	}
	left, err := gateway.OpenMolecule(ctx, endedFormula.RootID)
	if err != nil || left.RootID != "" {
		t.Errorf("expected nothing of the ended story's formula left open, got %+v (%v)", left, err)
	}
}
