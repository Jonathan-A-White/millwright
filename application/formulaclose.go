package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// A story's formula is closed when what became of the story is settled: mw
// next closes it once the story lands or is refused, mw retry once the story is
// given back for a fresh attempt, and mw tidy closes what is left open of the
// formula of any story already closed (mw-gq6.438). Every next attempt starts
// on a fresh branch, and dispatch pours it a fresh formula, so nothing an ended
// session left open is worked again.

// FormulaClosed is what closing a story's formula closed: the steps its
// session left open, and whether the root was closed after them.
type FormulaClosed struct {
	Root  string
	Steps []string
	// RootClosed says the root was open and was closed; a root closed already
	// had only its steps closed.
	RootClosed bool
}

// Any says something was closed.
func (f FormulaClosed) Any() bool { return f.RootClosed || len(f.Steps) > 0 }

// String is what a report prints of it.
func (f FormulaClosed) String() string {
	steps := counted(len(f.Steps), "step", "steps") + " left open"
	switch {
	case !f.RootClosed:
		return steps + " of " + f.Root
	case len(f.Steps) == 0:
		return f.Root
	}
	return f.Root + " and " + steps
}

// closeFormula closes what is still open of the formula poured as root: each
// step left open, in the order they are worked, and then the root itself, each
// with reason. An empty root, a story no formula was recorded for, has nothing
// to close; a root closed already has only its steps closed. It stops at the
// first close that fails, so that a root is never closed over a step still
// open, and says what it closed before it.
func closeFormula(ctx context.Context, beads WorkTracker, root, reason string) (FormulaClosed, error) {
	closed := FormulaClosed{Root: root}
	if strings.TrimSpace(root) == "" {
		return closed, nil
	}
	molecule, err := beads.OpenMolecule(ctx, root)
	if err != nil {
		return closed, err
	}
	steps, rootOpen := molecule.Steps, molecule.RootID != ""
	if !rootOpen {
		if steps, err = beads.OpenSteps(ctx, root); err != nil {
			return closed, fmt.Errorf("reading the steps of %s: %w", root, err)
		}
	}
	for _, step := range steps {
		if err := beads.CloseStory(ctx, step.ID, reason); err != nil {
			return closed, fmt.Errorf("closing the step %s: %w", step.ID, err)
		}
		closed.Steps = append(closed.Steps, step.ID)
	}
	if rootOpen {
		if err := beads.CloseStory(ctx, root, reason); err != nil {
			return closed, fmt.Errorf("closing the root %s: %w", root, err)
		}
		closed.RootClosed = true
	}
	return closed, nil
}

// FormulaClosedWhy is the reason a close-out or a retry closes a story's
// formula with: who closed it, on which host, and why.
func FormulaClosedWhy(command, host, storyID, what string) string {
	return fmt.Sprintf("Closed by %s on %s: %s %s", command, host, storyID, what)
}

// formulaStory reads the story a step's title names: every formula's first
// step is titled "<Verb> <story>: <title>" ("Understand {{story}}: {{title}}",
// "Do {{story}}: {{title}}").
var formulaStory = regexp.MustCompile(`^[A-Z][a-z]+ ([a-z0-9]+-[A-Za-z0-9.-]+): `)

// FormulaStoryOf is the story a formula step's title names, or "" when it
// names none.
func FormulaStoryOf(title string) string {
	if m := formulaStory.FindStringSubmatch(title); m != nil && !strings.Contains(m[1], "-mol-") {
		return m[1]
	}
	return ""
}
