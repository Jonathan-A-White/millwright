package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// The formula a story's session works, poured into a fake tracker, as the
// scenarios of mw next, mw retry and mw tidy pour it (mw-gq6.438). The first
// step names the story, "Understand <story>: <title>", as every formula's does.

// pourFormula pours a three-step formula for a story, records it on the story
// as dispatch does, and closes the first closed steps, as a session working it
// would.
func pourFormula(tracker *apptest.FakeTracker, id string, closed int) (application.Molecule, error) {
	ctx := context.Background()
	tracker.AddFormula("tdd-feature",
		application.FormulaStep{Title: "Understand {{story}}: {{title}}"},
		application.FormulaStep{Title: "Implement until green"},
		application.FormulaStep{Title: "Commit"},
	)
	molecule, err := tracker.PourFormula(ctx, "tdd-feature", id, "The story "+id, false)
	if err != nil {
		return molecule, err
	}
	if err := tracker.SetStoryMetadata(ctx, id, map[string]string{application.MoleculeField: molecule.RootID}); err != nil {
		return molecule, err
	}
	for _, step := range molecule.Steps[:closed] {
		tracker.CloseStep(step.ID)
	}
	return molecule, nil
}

// formulaClosedSaying checks that nothing of the formula poured for a story is
// left open, and that its root and steps of it were closed with a reason saying
// text: the steps the session did not close, and none it did.
func formulaClosedSaying(tracker *apptest.FakeTracker, id string, steps int, text string) error {
	molecule, poured := tracker.Poured(id)
	if !poured {
		return fmt.Errorf("no formula was poured for %s", id)
	}
	ctx := context.Background()
	if open, err := tracker.OpenSteps(ctx, molecule.RootID); err != nil || len(open) > 0 {
		return fmt.Errorf("the formula %s of %s still has %d step(s) open (%v)", molecule.RootID, id, len(open), err)
	}
	if open, err := tracker.OpenMolecule(ctx, molecule.RootID); err != nil || open.RootID != "" {
		return fmt.Errorf("the root %s of the formula of %s is still open (%v)", molecule.RootID, id, err)
	}
	if reason := tracker.CloseReason(molecule.RootID); !strings.Contains(reason, text) {
		return fmt.Errorf("the root %s of the formula of %s was closed saying %q, want it to say %q", molecule.RootID, id, reason, text)
	}
	saying := 0
	for _, step := range molecule.Steps {
		if strings.Contains(tracker.CloseReason(step.ID), text) {
			saying++
		}
	}
	if saying != steps {
		return fmt.Errorf("%d step(s) of the formula of %s were closed saying %q, want %d", saying, id, text, steps)
	}
	return nil
}

// formulaUntouched checks that the formula poured for a story is as the session
// left it: its root open, and only the steps it closed closed.
func formulaUntouched(tracker *apptest.FakeTracker, id string, open int) error {
	molecule, poured := tracker.Poured(id)
	if !poured {
		return fmt.Errorf("no formula was poured for %s", id)
	}
	left, err := tracker.OpenMolecule(context.Background(), molecule.RootID)
	if err != nil {
		return err
	}
	if left.RootID == "" || len(left.Steps) != open {
		return fmt.Errorf("the formula %s of %s: root open is %t with %d step(s) open, want it open with %d",
			molecule.RootID, id, left.RootID != "", len(left.Steps), open)
	}
	return nil
}
