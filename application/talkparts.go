package application

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// DefaultTalkWaitPartsWait is how long after the first part of a turn sent in
// parts mw talk wait holds for the rest before it prints what came.
const DefaultTalkWaitPartsWait = 60 * time.Second

// inParts reports whether turn is one part of a turn sent in several records:
// a turn whose Part is within 1..Parts of more than one. A turn with no part
// fields, or part 1 of 1, is whole; so is a record with part fields that make
// no sense, so that its words are never lost for the want of a sensible count.
func (t *TalkTurn) inParts() bool {
	return t.Role == TalkRoleTurn && t.Parts > 1 && t.Part >= 1 && t.Part <= t.Parts
}

// talkParts is the parts of one turn that have come, whatever order they came in.
type talkParts struct {
	talkID string
	turn   int
	of     int
	got    map[int]*TalkTurn
	// first is the record of the first part to arrive: the turn is kept,
	// and its file named, by it.
	first PosternRecord
}

func newTalkParts(first PosternRecord, turn *TalkTurn) *talkParts {
	return &talkParts{talkID: turn.Talk.ID, turn: turn.Talk.Turn, of: turn.Parts, got: map[int]*TalkTurn{}, first: first}
}

// is reports whether turn is a part of the same turn as p's.
func (p *talkParts) is(turn *TalkTurn) bool {
	return turn.inParts() && turn.Talk.ID == p.talkID && turn.Talk.Turn == p.turn && turn.Parts == p.of
}

// add takes a part; one that comes twice is taken as its latest.
func (p *talkParts) add(turn *TalkTurn) { p.got[turn.Part] = turn }

func (p *talkParts) complete() bool { return len(p.got) == p.of }

// missing is the numbers of the parts that have not come, in order.
func (p *talkParts) missing() []int {
	var none []int
	for k := 1; k <= p.of; k++ {
		if p.got[k] == nil {
			none = append(none, k)
		}
	}
	return none
}

// missingLines is a line to each part not come, "missing part k of n", each
// ending in a newline; "" when all have.
func (p *talkParts) missingLines() string {
	var out strings.Builder
	for _, k := range p.missing() {
		fmt.Fprintf(&out, "missing part %d of %d\n", k, p.of)
	}
	return out.String()
}

// joined is the turn the parts that came make: their texts one after another,
// in order, with no separator (a part ends where the cap fell, in a word as
// likely as not). Its picture, model and about come from the first part that
// carries one, it is cut if any part says so, and it has no part fields.
func (p *talkParts) joined() *TalkTurn {
	var order []int
	for k := range p.got {
		order = append(order, k)
	}
	sort.Ints(order)
	var whole TalkTurn
	var text strings.Builder
	for n, k := range order {
		part := p.got[k]
		if n == 0 {
			whole = *part
			whole.Links = nil
		}
		text.WriteString(part.Text)
		whole.Cut = whole.Cut || part.Cut
		if whole.Model == "" {
			whole.Model = part.Model
		}
		if whole.About == nil {
			whole.About = part.About
		}
		if whole.Attachment == nil {
			whole.Attachment = part.Attachment
		}
		if len(whole.Attachments) == 0 {
			whole.Attachments = part.Attachments
		}
		for _, link := range part.Links {
			if !slices.Contains(whole.Links, link) {
				whole.Links = append(whole.Links, link)
			}
		}
	}
	whole.Text = text.String()
	whole.Part, whole.Parts = 0, 0
	return &whole
}
