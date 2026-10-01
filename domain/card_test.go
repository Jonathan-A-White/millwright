package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDeriveExpectationMapsTheFourAsks(t *testing.T) {
	for _, tc := range []struct {
		text  string
		links []string
		want  Expectation
	}{
		{"VERIFIED on mw-a.1", nil, Expectation{Bead: "mw-a.1", State: ExpectVerified}},
		{"Looks good on mw-b", nil, Expectation{Bead: "mw-b", State: ExpectClosed}},
		{"Approve mw-c.2: the plan for push-to-talk", nil, Expectation{Bead: "mw-c.2", State: ExpectAnswered}},
		{"Answer mw-d about the backend", nil, Expectation{Bead: "mw-d", State: ExpectAnswered}},
		{"Release mw-e.3.1", nil, Expectation{Bead: "mw-e.3.1", State: ExpectOpen}},
		{"verified on mw-f, then close it", nil, Expectation{Bead: "mw-f", State: ExpectVerified}},
		{"Approve the follow-up plan", []string{"mw-g.4"}, Expectation{Bead: "mw-g.4", State: ExpectAnswered}},
		{"Approve follow-up mw-h", []string{"mw-z", "mw-h"}, Expectation{Bead: "mw-h", State: ExpectAnswered}},
	} {
		got := DeriveExpectation(tc.text, tc.links)
		if got == nil || *got != tc.want {
			t.Errorf("DeriveExpectation(%q, %v) = %+v, want %+v", tc.text, tc.links, got, tc.want)
		}
	}
}

func TestDeriveExpectationIsNoneWithoutAnAskOrABead(t *testing.T) {
	for _, text := range []string{"Read the brief on mw-a", "Approve the plan", "Releases are weekly", ""} {
		if got := DeriveExpectation(text, nil); got != nil {
			t.Errorf("DeriveExpectation(%q) = %+v, want none", text, got)
		}
	}
}

func TestParseCardItemReadsTextLinksAndExpectation(t *testing.T) {
	item, err := ParseCardItem("Check the map|mw-a.1, mw-b|mw-a.1:landed")
	if err != nil {
		t.Fatal(err)
	}
	if item.N != 0 || item.Text != "Check the map" || strings.Join(item.Links, " ") != "mw-a.1 mw-b" ||
		item.Expect == nil || *item.Expect != (Expectation{Bead: "mw-a.1", State: ExpectLanded}) {
		t.Fatalf("got %+v (expect %+v)", item, item.Expect)
	}

	item, err = ParseCardItem("4. Release mw-e")
	if err != nil {
		t.Fatal(err)
	}
	if item.N != 4 || item.Text != "Release mw-e" || len(item.Links) != 0 ||
		item.Expect == nil || *item.Expect != (Expectation{Bead: "mw-e", State: ExpectOpen}) {
		t.Fatalf("expected item 4 with its expectation derived, got %+v (expect %+v)", item, item.Expect)
	}

	item, err = ParseCardItem("Read the brief|mw-c|")
	if err != nil || item.Expect != nil {
		t.Fatalf("expected an item with no ask to carry no expectation, got %+v, %v", item, err)
	}
}

func TestParseCardItemRefusesABadExpectation(t *testing.T) {
	for spec, want := range map[string]string{
		"Check it|mw-x|mw-x:done":  "open, landed, verified, closed or answered",
		"Check it|mw-x|mw-x":       "<bead>:<state>",
		"Check it|mw-x|:landed":    "<bead>:<state>",
		"   |mw-x|mw-x:landed":     "no text",
		"a|b|c|d":                  "<text>|<links>|<bead>:<state>",
		"Check it|mw x|mw-x:open":  "not a bead id",
		"Check it||mw x:verified":  "not a bead id",
		"0. Check it||mw-x:closed": "from 1",
	} {
		_, err := ParseCardItem(spec)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseCardItem(%q): expected a refusal saying %q, got %v", spec, want, err)
		}
	}
}

func TestNewCardNumbersItemsAndDerivesWhatItSubscribesTo(t *testing.T) {
	var items []CardItem
	for _, spec := range []string{"Approve mw-a|mw-a|", "Read the brief|mw-b|", "VERIFIED on mw-c|mw-c,mw-a|", "Release mw-a"} {
		item, err := ParseCardItem(spec)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	card, err := NewCard("  Top 5  ", items)
	if err != nil {
		t.Fatal(err)
	}
	if card.Title != "Top 5" {
		t.Errorf("expected the title trimmed, got %q", card.Title)
	}
	for i, item := range card.Items {
		if item.N != i+1 {
			t.Errorf("expected item %d numbered %d, got %d", i, i+1, item.N)
		}
	}
	if got := strings.Join(card.Subscribe.Kinds, " "); got != "card_answered bead_changed" {
		t.Errorf("expected kinds card_answered bead_changed, got %q", got)
	}
	if got := strings.Join(card.Subscribe.Beads, " "); got != "mw-a mw-b mw-c" {
		t.Errorf("expected beads mw-a mw-b mw-c, got %q", got)
	}
}

func TestNewCardRefusesNoTitleNoItemsAndATwiceGivenNumber(t *testing.T) {
	item := CardItem{Text: "Read it"}
	if _, err := NewCard(" ", []CardItem{item}); err == nil || !strings.Contains(err.Error(), "title") {
		t.Errorf("expected a card with no title refused, got %v", err)
	}
	if _, err := NewCard("Top 5", nil); err == nil || !strings.Contains(err.Error(), "item") {
		t.Errorf("expected a card with no items refused, got %v", err)
	}
	if _, err := NewCard("Top 5", []CardItem{{N: 2, Text: "a"}, {Text: "b"}}); err == nil || !strings.Contains(err.Error(), "item 2 twice") {
		t.Errorf("expected item 2 given twice refused, got %v", err)
	}
	if _, err := NewCard("Top 5", []CardItem{{Text: "a", Expect: &Expectation{Bead: "mw-a", State: "done"}}}); err == nil {
		t.Errorf("expected a bad expectation state refused")
	}
}

// TestCardJSONIsTheRecordShape pins the plaintext a card record seals: the
// app reads these names.
func TestCardJSONIsTheRecordShape(t *testing.T) {
	item, err := ParseCardItem("Approve mw-a|mw-a|")
	if err != nil {
		t.Fatal(err)
	}
	card, err := NewCard("Top 5", []CardItem{item})
	if err != nil {
		t.Fatal(err)
	}
	card.ID, card.Prompt, card.Thread = "direct:abc", "top5", &CardThread{Bead: "mw-x"}
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"title":"Top 5","prompt":"top5","thread":{"bead":"mw-x"},"items":[{"n":1,"text":"Approve mw-a","links":["mw-a"],"expect":{"bead":"mw-a","state":"answered"}}],"subscribe":{"kinds":["card_answered"],"beads":["mw-a"]}}`
	if string(raw) != want {
		t.Fatalf("expected\n%s\ngot\n%s", want, raw)
	}

	bare, err := NewCard("Nothing to watch", []CardItem{{Text: "Read it"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(bare)
	if !strings.Contains(string(raw), `"subscribe":{"kinds":[],"beads":[]}`) {
		t.Fatalf("expected an empty subscribe to be empty lists, got %s", raw)
	}
}

func TestNewCardUpdateCarriesReItemsLinksAndTicks(t *testing.T) {
	item, err := ParseCardItem("6. Answer mw-f")
	if err != nil {
		t.Fatal(err)
	}
	update, err := NewCardUpdate(" direct:abc ", []CardItem{item}, []string{"2:mw-b", "2:mw-b.1", "3:mw-c"}, []int{1, 4})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"re":"direct:abc","items":[{"n":6,"text":"Answer mw-f","expect":{"bead":"mw-f","state":"answered"}}],"links":{"2":["mw-b","mw-b.1"],"3":["mw-c"]},"tick":[1,4]}`
	if string(raw) != want {
		t.Fatalf("expected\n%s\ngot\n%s", want, raw)
	}
}

func TestNewCardUpdateRefusesWhatCannotBeAnUpdate(t *testing.T) {
	numbered := CardItem{N: 1, Text: "a"}
	for _, tc := range []struct {
		re    string
		items []CardItem
		links []string
		tick  []int
		want  string
	}{
		{"", []CardItem{numbered}, nil, nil, "which card"},
		{"direct:abc", nil, nil, nil, "nothing to change"},
		{"direct:abc", []CardItem{{Text: "a"}}, nil, nil, "<n>. <text>"},
		{"direct:abc", nil, []string{"x:mw-a"}, nil, "<n>:<bead>"},
		{"direct:abc", nil, []string{"2:mw a"}, nil, "not a bead id"},
		{"direct:abc", nil, nil, []int{0}, "from 1"},
		{"direct:abc", []CardItem{numbered, numbered}, nil, nil, "item 1 twice"},
	} {
		_, err := NewCardUpdate(tc.re, tc.items, tc.links, tc.tick)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("NewCardUpdate(%q, %+v, %v, %v): expected a refusal saying %q, got %v", tc.re, tc.items, tc.links, tc.tick, tc.want, err)
		}
	}
}
