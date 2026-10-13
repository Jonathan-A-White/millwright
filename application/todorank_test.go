package application

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

func card(id, rank string) StoryDetail {
	return StoryDetail{Story: domain.Story{ID: id}, TodoRank: rank}
}

func ids(cards []StoryDetail) []string {
	var out []string
	for _, c := range cards {
		out = append(out, c.Story.ID)
	}
	return out
}

func TestInTodoOrderPutsRankedCardsFirstByRankAndTheRestInFiledOrder(t *testing.T) {
	filed := []StoryDetail{card("a", ""), card("b", "10"), card("c", "-1"), card("d", "not a number"), card("e", "2.5")}
	got := ids(InTodoOrder(filed))
	if want := []string{"c", "e", "b", "a", "d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
	if ids(filed)[0] != "a" {
		t.Error("InTodoOrder reordered its argument")
	}
}

func TestTodoRankForFallsBetweenNeighboursAndNumbersTheListOnlyWhenItMust(t *testing.T) {
	cases := []struct {
		name       string
		cards      []StoryDetail
		index      int
		rank       string
		renumbered map[string]string
	}{
		{"empty list", nil, 0, "1", nil},
		{"head", []StoryDetail{card("a", "1")}, 0, "0", nil},
		{"tail", []StoryDetail{card("a", "1")}, 1, "2", nil},
		{"between", []StoryDetail{card("a", "1"), card("b", "2")}, 1, "1.5", nil},
		{"head of unranked", []StoryDetail{card("a", ""), card("b", "")}, 0, "1", nil},
		{"tail after ranked then unranked", []StoryDetail{card("a", "4"), card("b", "")}, 1, "5", nil},
		{"after an unranked card", []StoryDetail{card("a", ""), card("b", "")}, 1, "2",
			map[string]string{"a": "1", "b": "3"}},
		{"no number between", []StoryDetail{card("a", "1"), card("b", "1")}, 1, "2",
			map[string]string{"a": "1", "b": "3"}},
	}
	for _, c := range cases {
		rank, renumbered := todoRankFor(c.cards, c.index)
		if rank != c.rank || !reflect.DeepEqual(renumbered, c.renumbered) {
			t.Errorf("%s: expected %s and %v, got %s and %v", c.name, c.rank, c.renumbered, rank, renumbered)
		}
	}
}
