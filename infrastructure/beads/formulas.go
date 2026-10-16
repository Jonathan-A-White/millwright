package beads

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// isFormula says whether a bead is one of a poured formula's: a root, typed
// molecule, or a step, whose id bd gives the -mol- infix.
func isFormula(b bead) bool {
	return b.Type == "molecule" || strings.Contains(b.ID, "-mol-")
}

// OpenFormulas implements application.TidyFormulas (mw-gq6.438). One list of
// every bead not closed, mail and events left out, holds both the formula beads
// still open and the stories that record a molecule. A formula's story is read
// off its steps' titles; when none of its open steps names it, and no live
// story records the formula as its own, its steps are read again, closed ones
// too, for the one that does.
func (g *Gateway) OpenFormulas(ctx context.Context) ([]application.PouredFormula, error) {
	out, err := g.call(ctx, "list", "--limit", "0", "--brief", "--exclude-type", "mail,event", "--json")
	if err != nil {
		return nil, err
	}
	open, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading the open beads: %w", err)
	}

	live := map[string]string{}
	steps := map[string][]bead{}
	rootOpen := map[string]bool{}
	var roots []string
	root := func(id string) {
		if _, seen := steps[id]; !seen {
			steps[id] = nil
			roots = append(roots, id)
		}
	}
	for _, b := range open {
		if !isFormula(b) {
			if molecule := b.pathMetadata()[application.MoleculeField]; molecule != "" {
				live[molecule] = b.ID
			}
			continue
		}
		if b.Type == "molecule" || !strings.Contains(b.Parent, "-mol-") {
			root(b.ID)
			rootOpen[b.ID] = true
			continue
		}
		root(b.Parent)
		steps[b.Parent] = append(steps[b.Parent], b)
	}
	sort.Strings(roots)

	poured := make([]application.PouredFormula, 0, len(roots))
	for _, id := range roots {
		formula := application.PouredFormula{Root: id, RootOpen: rootOpen[id], LiveStory: live[id]}
		for _, step := range inWorkedOrder(steps[id]) {
			formula.Steps = append(formula.Steps, step.ID)
			if formula.Story == "" {
				formula.Story = application.FormulaStoryOf(step.Title)
			}
		}
		if formula.Story == "" && formula.LiveStory == "" {
			if formula.Story, err = g.formulaStory(ctx, id); err != nil {
				return nil, err
			}
		}
		poured = append(poured, formula)
	}
	return poured, nil
}

// formulaStory reads every step of a formula, closed ones too, for the story
// one of their titles names.
func (g *Gateway) formulaStory(ctx context.Context, root string) (string, error) {
	out, err := g.call(ctx, "list", "--parent", root, "--all", "--limit", "0", "--brief", "--json")
	if err != nil {
		return "", err
	}
	all, err := decodeBeads(out)
	if err != nil {
		return "", fmt.Errorf("reading the steps of %s: %w", root, err)
	}
	for _, step := range inWorkedOrder(all) {
		if story := application.FormulaStoryOf(step.Title); story != "" {
			return story, nil
		}
	}
	return "", nil
}

// CloseFormulaBead implements application.TidyFormulas. Only an id bd gave a
// formula's bead, with its -mol- infix, is closed; any other is refused without
// asking bd.
func (g *Gateway) CloseFormulaBead(ctx context.Context, id, reason string) error {
	if !strings.Contains(id, "-mol-") {
		return fmt.Errorf("%s is not a bead of a poured formula, so it was not closed", id)
	}
	return g.CloseStory(ctx, id, reason)
}
