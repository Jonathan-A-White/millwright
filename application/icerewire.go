package application

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// BlockerWires is the tracker narrowed to the two writes that move a wait:
// the beads gateway and the fake tracker both hold them.
type BlockerWires interface {
	AddBlocker(ctx context.Context, blocked, blocker string) error
	RemoveBlocker(ctx context.Context, blocked, blocker string) error
}

// IceMove is one live bead moved off an iced one: it dropped the iced bead,
// took its open blockers, and the iced bead now waits on it.
type IceMove struct {
	Dependent string
	// Took are the iced bead's open blockers the dependent now waits on.
	Took []string
	// Waits is true when the iced bead now waits on the dependent. It is false
	// when the dependent reaches the iced bead through a closed bead: bd refuses
	// that wait as a cycle, so the dependent is freed and the iced bead is not
	// moved behind it.
	Waits bool
}

// IceRewire is what icing a bead moved. The rule: an iced bead never blocks a
// live one. For each live dependent X of the iced bead I, X drops I and takes
// I's open blockers, and I waits on X.
type IceRewire struct {
	Iced  string
	Moves []IceMove
	// Already is true when the bead was iced before this run, so that only the
	// rewiring ran.
	Already bool
}

// String says what was moved, one line a dependent, or "" when nothing was. For
// a bead that was iced already it opens by saying so, and says when nothing
// moved.
func (r IceRewire) String() string {
	var b strings.Builder
	if r.Already {
		fmt.Fprintf(&b, "%s is already iced.\n", r.Iced)
		if len(r.Moves) == 0 {
			fmt.Fprintf(&b, "Nothing moved: no live bead waits on %s.\n", r.Iced)
		}
	}
	for _, m := range r.Moves {
		fmt.Fprintf(&b, "%s no longer waits on %s", m.Dependent, r.Iced)
		if len(m.Took) > 0 {
			fmt.Fprintf(&b, ", waits on %s instead", strings.Join(m.Took, ", "))
		}
		if m.Waits {
			fmt.Fprintf(&b, "; %s now waits on it.\n", r.Iced)
		} else {
			fmt.Fprintf(&b, "; it reaches %s through a closed bead, so %s was not made to wait on it.\n", r.Iced, r.Iced)
		}
	}
	return b.String()
}

// iceLive is whether a bead is one the icebox must not hold up: work not
// finished and not itself iced. A held bead counts, so that releasing it later
// does not find it behind the iced one.
func iceLive(b domain.GraphBead) bool {
	if b.Type == "epic" || strings.EqualFold(b.Status, StatusClosed) {
		return false
	}
	return !slices.Contains(b.Labels, LabelIcebox)
}

// blockedOn lists what a bead waits on, by 'blocks' edges.
func blockedOn(b domain.GraphBead) []string {
	var ids []string
	for _, e := range b.Edges {
		if e.Kind == "blocks" {
			ids = append(ids, e.To)
		}
	}
	return ids
}

// IceRewireOf applies the rule to the iced bead id, reading the graph as it is
// now (the bead already iced or not) and writing through wires.
func IceRewireOf(ctx context.Context, graph BeadGraph, wires BlockerWires, id string) (IceRewire, error) {
	beads, err := graph.BeadGraph(ctx)
	if err != nil {
		return IceRewire{}, fmt.Errorf("reading the beads to move %s behind its dependents: %w", id, err)
	}
	byID := make(map[string]domain.GraphBead, len(beads))
	waits := make(map[string][]string, len(beads))
	for _, b := range beads {
		byID[b.ID] = b
		waits[b.ID] = blockedOn(b)
	}
	if _, known := byID[id]; !known {
		return IceRewire{}, nil
	}
	var openBlockers []string
	for _, blocker := range waits[id] {
		if b, ok := byID[blocker]; ok && !strings.EqualFold(b.Status, StatusClosed) {
			openBlockers = append(openBlockers, blocker)
		}
	}
	var dependents []string
	for _, b := range beads {
		if b.ID != id && iceLive(b) && slices.Contains(waits[b.ID], id) {
			dependents = append(dependents, b.ID)
		}
	}
	sort.Strings(dependents)

	report := IceRewire{Iced: id}
	// First every dependent lets go and takes the iced bead's blockers, then the
	// iced bead waits on each that can be waited on: so that one dependent
	// waiting on another through the iced bead is not mistaken for a cycle.
	for _, x := range dependents {
		move := IceMove{Dependent: x}
		for _, blocker := range openBlockers {
			if blocker == x || slices.Contains(waits[x], blocker) {
				continue
			}
			if err := wires.AddBlocker(ctx, x, blocker); err != nil {
				return report, fmt.Errorf("making %s wait on %s, a blocker of the iced %s: %w", x, blocker, id, err)
			}
			waits[x] = append(waits[x], blocker)
			move.Took = append(move.Took, blocker)
		}
		if err := wires.RemoveBlocker(ctx, x, id); err != nil {
			return report, fmt.Errorf("taking %s off the iced %s: %w", x, id, err)
		}
		waits[x] = slices.DeleteFunc(waits[x], func(w string) bool { return w == id })
		report.Moves = append(report.Moves, move)
	}
	for n := range report.Moves {
		x := report.Moves[n].Dependent
		if reaches(waits, x, id) {
			continue
		}
		if err := wires.AddBlocker(ctx, id, x); err != nil {
			return report, fmt.Errorf("making the iced %s wait on %s: %w", id, x, err)
		}
		waits[id] = append(waits[id], x)
		report.Moves[n].Waits = true
	}
	return report, nil
}
