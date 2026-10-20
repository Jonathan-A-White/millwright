package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/domain"
)

// todoWaitKinds are the kinds of link a To do card waits on another bead
// through, the same as the ones bd holds a bead back for (blockingKinds in
// infrastructure/beads).
var todoWaitKinds = []string{"blocks", "conditional-blocks", "waits-for"}

// todoWaiting is an open To do card that waits on a story not yet finished,
// with the ids of the stories it waits on.
type todoWaiting struct {
	card StoryDetail
	on   []string
}

// todoLeftOutOfNow is the why mw todo rank gives a card that stood in Do now
// and waits on unfinished stories.
func todoLeftOutOfNow(on []string) string {
	why := "left out of Now: waits on " + strings.Join(on, ", ")
	for len(on) > 1 && utf8.RuneCountInString(why) >= TodoRowWhyMax {
		on = on[:len(on)-1]
		why = "left out of Now: waits on " + strings.Join(on, ", ") + " and more"
	}
	return why
}

// unfinishedBlockers are the ids a bead waits on that are not closed, by the
// links of kind todoWaitKinds. A blocker the graph does not hold is no wait.
func unfinishedBlockers(byID map[string]domain.GraphBead, b domain.GraphBead) []string {
	var on []string
	for _, e := range b.Edges {
		if !slices.Contains(todoWaitKinds, e.Kind) || slices.Contains(on, e.To) {
			continue
		}
		if other, ok := byID[e.To]; ok && !strings.EqualFold(other.Status, StatusClosed) {
			on = append(on, e.To)
		}
	}
	return on
}

// splitWaiting divides the open cards the tracker lists as ready from those
// that wait on an unfinished story, and adds the waiting cards the ready list
// leaves out (splitWaitingCards).
func (p TodoRankPass) splitWaiting(ctx context.Context, ready []StoryDetail) (free []StoryDetail, waiting []todoWaiting, err error) {
	return splitWaitingCards(ctx, p.Tracker, p.Graph, ready)
}

// openTodoCards lists every open hitl:todo card, waiting or not: the cards the
// tracker lists as ready, and the ones it keeps out of that list because they
// wait on an unfinished story. It is the one way mw todo search and Postern's
// reading of his messages list the open to-dos (mw-gq6.663); a nil graph lists
// the ready ones only. What reads only the ready cards, because a card he
// cannot act on yet should not alarm him, calls ReadyWithLabel itself.
func openTodoCards(ctx context.Context, tracker WorkTracker, graph BeadGraph) ([]StoryDetail, error) {
	ready, err := tracker.ReadyWithLabel(ctx, LabelHitlTodo)
	if err != nil {
		return nil, err
	}
	if graph == nil {
		return ready, nil
	}
	free, waiting, err := splitWaitingCards(ctx, tracker, graph, ready)
	if err != nil {
		return nil, err
	}
	for _, w := range waiting {
		free = append(free, w.card)
	}
	return free, nil
}

// splitWaitingCards divides the open cards the tracker lists as ready from
// those that wait on an unfinished story, and adds the waiting cards the ready
// list leaves out, which it reads from the bead graph: the tracker keeps a
// blocked card out of the ready list, so a card that stood in Do now when its
// blocker was added would stay there. Without a graph, a ready card's own Needs
// are what it waits on.
func splitWaitingCards(ctx context.Context, tracker WorkTracker, g BeadGraph, ready []StoryDetail) (free []StoryDetail, waiting []todoWaiting, err error) {
	if g == nil {
		for _, card := range ready {
			if len(card.Needs) > 0 {
				waiting = append(waiting, todoWaiting{card: card, on: slices.Clone(card.Needs)})
			} else {
				free = append(free, card)
			}
		}
		return free, waiting, nil
	}
	graph, err := g.BeadGraph(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the bead graph for the cards that wait: %w", err)
	}
	byID := make(map[string]domain.GraphBead, len(graph))
	for _, b := range graph {
		byID[b.ID] = b
	}
	listed := map[string]bool{}
	for _, card := range ready {
		listed[card.Story.ID] = true
		if on := unfinishedBlockers(byID, byID[card.Story.ID]); len(on) > 0 {
			waiting = append(waiting, todoWaiting{card: card, on: on})
		} else {
			free = append(free, card)
		}
	}
	var ids []string
	waitsOn := map[string][]string{}
	for _, b := range graph {
		if listed[b.ID] || b.Type == "epic" || !strings.EqualFold(b.Status, StatusOpen) || !hasLabel(b.Labels, LabelHitlTodo) {
			continue
		}
		if on := unfinishedBlockers(byID, b); len(on) > 0 {
			ids = append(ids, b.ID)
			waitsOn[b.ID] = on
		}
	}
	if len(ids) == 0 {
		return free, waiting, nil
	}
	found, err := tracker.ShowBeads(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the cards that wait, %s: %w", strings.Join(ids, ", "), err)
	}
	for _, card := range found {
		if refuseNotACard(card) == "" {
			waiting = append(waiting, todoWaiting{card: card, on: waitsOn[card.Story.ID]})
		}
	}
	return free, waiting, nil
}

// heldOutOfNow is the row of each waiting card: later, with the why, for one
// that stands in now; the row it has for any other, which the pass leaves as
// it is.
func heldOutOfNow(waiting []todoWaiting) []TodoRankRow {
	rows := make([]TodoRankRow, len(waiting))
	for n, w := range waiting {
		rows[n] = TodoRankRow{Bead: w.card.Story.ID, Row: w.card.TodoRow, Why: w.card.TodoRowWhy}
		if w.card.TodoRow == TodoRowNow {
			rows[n].Row, rows[n].Why = TodoRowLater, todoLeftOutOfNow(w.on)
		}
	}
	return rows
}
