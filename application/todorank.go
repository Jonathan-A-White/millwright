package application

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

// TodoRankField is the metadata field that orders a hitl:todo card among the
// cards of its list: a number, written as text, that sorts the card ahead of
// those with a higher one. The Governor's phone moves a card by todo_move
// (postern's docs/protocol.md section 13), which sets it; a card the Mayor
// filed has none and follows the ranked ones, in the order filed.
const TodoRankField = "todo_rank"

// todoRank reads a rank as a number, false for none or for text that is not
// one.
func todoRank(text string) (float64, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	rank, err := strconv.ParseFloat(text, 64)
	return rank, err == nil
}

// formatTodoRank writes a rank as todo_rank holds it.
func formatTodoRank(rank float64) string { return strconv.FormatFloat(rank, 'g', -1, 64) }

// InTodoOrder is cards in the order a list shows them: those with a todo_rank
// by rank, then the rest as they came, which is the order filed. It leaves
// cards as it was.
func InTodoOrder(cards []StoryDetail) []StoryDetail {
	ordered := slices.Clone(cards)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, aRanked := todoRank(ordered[i].TodoRank)
		b, bRanked := todoRank(ordered[j].TodoRank)
		if aRanked != bRanked {
			return aRanked
		}
		return aRanked && a < b
	})
	return ordered
}

// todoRankFor is the rank to give a card put at index among cards, the cards
// of its list in order and without it. It is a rank between its neighbours
// when there is one, which changes no other card. When there is none — the
// card before it has no rank, or its neighbours' ranks leave no number between
// them — renumbers is every card of the list numbered 1, 2, 3 in the order it
// then stands, the moved card's number being rank; the cards' ids are its keys.
func todoRankFor(cards []StoryDetail, index int) (rank string, renumbers map[string]string) {
	var prev, next *float64
	prevRanked := true
	if index > 0 {
		r, has := todoRank(cards[index-1].TodoRank)
		prevRanked = has
		if has {
			prev = &r
		}
	}
	if index < len(cards) {
		if r, has := todoRank(cards[index].TodoRank); has {
			next = &r
		}
	}
	switch {
	case !prevRanked:
	case prev == nil && next == nil:
		return formatTodoRank(1), nil
	case prev == nil:
		return formatTodoRank(*next - 1), nil
	case next == nil:
		return formatTodoRank(*prev + 1), nil
	default:
		if mid := (*prev + *next) / 2; mid > *prev && mid < *next {
			return formatTodoRank(mid), nil
		}
	}
	renumbers = map[string]string{}
	for i, card := range cards {
		n := i + 1
		if i >= index {
			n++
		}
		renumbers[card.Story.ID] = formatTodoRank(float64(n))
	}
	return formatTodoRank(float64(index + 1)), renumbers
}
