package application

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// SleepNeedsYouFile is the part of the bundle that holds his review queue, item
// by item, with the evidence of the stale ones (mw-gq6.674).
const SleepNeedsYouFile = "needs-you.txt"

// ideasOnlyCardPrefix opens the title of the card that gives his ideas back.
const ideasOnlyCardPrefix = "Your ideas from Ideas only ("

// NeedsYouText is a text that may name an idea's txid, and where it is from: a
// bead's description, a handoff.
type NeedsYouText struct {
	Source string
	Text   string
}

// NeedsYouFacts is everything needs-you.txt is built from, all read already. The
// queue is DamperFacts': the one mw stuck --dampers counts, so that the sweep and
// his phone count the same things.
type NeedsYouFacts struct {
	DamperFacts
	// Comments are the comments of the beads in the queue, oldest first.
	Comments map[string][]Comment
	// Versions is each rig's current version, by rig name; a rig that is not
	// there has none known.
	Versions map[string]string
	// Cards are the cards and card updates sent, oldest first: the live Ideas
	// only cards are read from them.
	Cards []domain.CardRecord
	// Released are the ideas of Ideas only that were given back, as they were held.
	Released []IdeasOnlyHeld
	// Evidence reads the texts an idea's txid may be named in, when an unticked
	// Ideas only item needs them; nil has none.
	Evidence func() []NeedsYouText
	// Limit is the [dampers] review_queue limit, for the heading; zero reads the default.
	Limit int
	// Missing says each source that could not be read, which the part tells.
	Missing []string
}

// needsYouItem is one item of his queue, or one handled item of an Ideas only
// card, as the agent reads it.
type needsYouItem struct {
	// Bead is the bead's id, or for an Ideas only item the card and item.
	Bead, Kind, Title string
	Filed             time.Time
	// Ask is the card's question, the demo's description or the step.
	Ask      string
	Parent   string
	Needs    []string
	Version  string
	Evidence []string
	Action   string
	Comments []Comment
}

// CommentBeads are the beads of the queue, whose comments the part holds.
func (f NeedsYouFacts) CommentBeads() []string {
	cards, demos, hands := reviewQueue(f.DamperFacts)
	return uniqueStrings(append(append(cards, demos...), hands...))
}

// QueueSize is how many items the review queue holds, as DamperCounts'
// ReviewQueue counts them.
func (f NeedsYouFacts) QueueSize() int {
	cards, demos, hands := reviewQueue(f.DamperFacts)
	return len(cards) + len(demos) + len(hands)
}

// Text is needs-you.txt, cut to budget bytes.
func (f NeedsYouFacts) Text(budget int) string {
	text, _ := f.TextCut(budget)
	return text
}

// TextCut is needs-you.txt cut to budget bytes, and how it was cut, "" when it
// was not. The stale candidates come first, in full; the other items follow, the
// oldest first, each on a line of its own when they do not fit in full, and the
// youngest are left out last.
func (f NeedsYouFacts) TextCut(budget int) (text, note string) {
	items := f.items()
	for _, compact := range []bool{false, true} {
		text = f.render(items, compact)
		if len(text) <= budget {
			if compact {
				return text, "each item that is not a stale candidate is on one line, without its comments"
			}
			return text, ""
		}
	}
	return cutLines(text, budget)
}

func (f NeedsYouFacts) render(items []needsYouItem, compact bool) string {
	var stale, rest []needsYouItem
	for _, item := range items {
		if len(item.Evidence) > 0 {
			stale = append(stale, item)
		} else {
			rest = append(rest, item)
		}
	}
	cards, demos, hands := reviewQueue(f.DamperFacts)
	limit := DamperLimits{ReviewQueue: f.Limit}.orDefaults().ReviewQueue
	var out strings.Builder
	fmt.Fprintf(&out, "NEEDS YOU: %d items: %d %s, %d %s, %d %s (his review queue, as mw stuck --dampers counts it; limit %d). Today is %s.\n",
		len(cards)+len(demos)+len(hands), len(cards), plural(len(cards), "card", "cards"), len(demos), plural(len(demos), "demo", "demos"),
		len(hands), plural(len(hands), "hands step", "hands steps"), limit, f.Now.UTC().Format(time.DateOnly))
	for _, missing := range f.Missing {
		out.WriteString("(" + missing + ")\n")
	}
	out.WriteString("A stale candidate carries mw's own evidence from the beads, cards and notes; it is a lead and not a verdict: judge it against its comments.\n")
	fmt.Fprintf(&out, "\nSTALE CANDIDATES (%d)\n", len(stale))
	if len(stale) == 0 {
		out.WriteString("(none)\n")
	}
	for _, item := range stale {
		out.WriteString(item.block(f.Now, false))
	}
	fmt.Fprintf(&out, "\nOTHER ITEMS (%d), the oldest first\n", len(rest))
	for _, item := range rest {
		out.WriteString(item.block(f.Now, compact))
	}
	return out.String()
}

// block is the item as the agent reads it. Compact is one line, without the
// comments.
func (i needsYouItem) block(now time.Time, compact bool) string {
	age := ""
	if !i.Filed.IsZero() {
		age = fmt.Sprintf(" %d days", wholeDays(now, i.Filed))
	}
	head := fmt.Sprintf("- %s [%s]%s: %s\n", i.Bead, i.Kind, age, i.Title)
	if compact {
		return fmt.Sprintf("- %s [%s]%s: %s | %s\n", i.Bead, i.Kind, age, i.Title, clipRunes(oneLine(i.Ask), 140))
	}
	var b strings.Builder
	b.WriteString(head)
	if i.Ask != "" {
		b.WriteString("    asks: " + clipRunes(oneLine(i.Ask), 400) + "\n")
	}
	if i.Parent != "" || len(i.Needs) > 0 {
		needs := "none"
		if len(i.Needs) > 0 {
			needs = strings.Join(i.Needs, ", ")
		}
		parent := i.Parent
		if parent == "" {
			parent = "none"
		}
		b.WriteString("    epic: " + parent + "; needs: " + needs + "\n")
	}
	if i.Version != "" {
		b.WriteString("    version: " + i.Version + "\n")
	}
	for _, line := range i.Evidence {
		b.WriteString("    evidence: " + line + "\n")
	}
	if i.Action != "" {
		b.WriteString("    action: " + i.Action + "\n")
	}
	if len(i.Comments) > 0 {
		var said []string
		for _, c := range i.Comments {
			when := ""
			if !c.Created.IsZero() {
				when = c.Created.UTC().Format(time.DateOnly) + " "
			}
			said = append(said, fmt.Sprintf("%s%s: %s", when, c.Author, clipRunes(oneLine(c.Text), 220)))
		}
		b.WriteString("    last comments: " + strings.Join(said, " | ") + "\n")
	}
	return b.String()
}

// items are the queue's items and the handled items of the Ideas only cards,
// the stale ones first in the order they are rendered: oldest first.
func (f NeedsYouFacts) items() []needsYouItem {
	byID := make(map[string]domain.GraphBead, len(f.Graph))
	for _, b := range f.Graph {
		byID[b.ID] = b
	}
	cards, demos, hands := reviewQueue(f.DamperFacts)
	var items []needsYouItem
	for _, bead := range cards {
		var note posternQuestionNote
		_ = json.Unmarshal([]byte(f.Notes[PosternQuestionKey(bead)]), &note)
		ask := note.Q
		if note.Rec != "" {
			ask += " (recommended " + note.Rec + ")"
		}
		filed, _ := time.Parse(time.RFC3339, note.Asked)
		item := f.base(byID, bead, "card", filed, ask)
		item.Evidence = append(item.Evidence, f.parkedCard(byID, bead)...)
		items = append(items, f.finish(byID, item))
	}
	for _, id := range demos {
		for _, d := range liveStories(f.Work) {
			if d.Story.ID != id {
				continue
			}
			item := f.base(byID, id, "demo", firstKnown(d.Created, d.Updated), d.Description)
			if item.Title == "" || item.Title == id {
				item.Title = d.Story.Title
			}
			item.Filed = firstKnown(item.Filed, byID[id].Created)
			if named := versionIn(d.Description); named != "" {
				rig := d.Merged().Rig
				if now := f.Versions[rig]; now != "" {
					item.Version = fmt.Sprintf("names v%s, %s is at %s", named, rig, now)
					if named != strings.TrimPrefix(now, "v") {
						item.Evidence = append(item.Evidence, fmt.Sprintf("the demo names v%s and %s is now at %s", named, rig, now))
					}
				}
			}
			items = append(items, f.finish(byID, item))
			break
		}
	}
	done := map[string]bool{}
	for _, bead := range hands {
		if done[bead] {
			continue
		}
		done[bead] = true
		for _, step := range viewHandsSteps(bead, f.Notes) {
			if step.Ran != nil || step.SupersededBy != "" || step.Retired != "" {
				continue
			}
			ask := fmt.Sprintf("step %s on %s as %s: %s (way back: %s)", step.ID, step.Host, step.As, step.Run, step.WayBack)
			item := f.base(byID, bead, "hands step", firstKnown(addedAt(f.Notes[HandsStepsKey(bead)], step.ID), byID[bead].Created), ask)
			items = append(items, f.finish(byID, item))
		}
	}
	items = append(items, f.staleIdeas()...)
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].Filed.IsZero() != items[b].Filed.IsZero() {
			return !items[a].Filed.IsZero()
		}
		return items[a].Filed.Before(items[b].Filed)
	})
	return items
}

// base is the item of bead with what the graph and the comments say of it.
func (f NeedsYouFacts) base(byID map[string]domain.GraphBead, bead, kind string, filed time.Time, ask string) needsYouItem {
	b := byID[bead]
	title := b.Title
	if title == "" {
		title = bead
	}
	item := needsYouItem{Bead: bead, Kind: kind, Title: title, Filed: firstKnown(filed, b.Created), Ask: strings.TrimSpace(ask)}
	for _, edge := range b.Edges {
		switch edge.Kind {
		case domain.EdgeParentChild:
			item.Parent = stateOf(byID, edge.To)
		case domain.EdgeBlocks:
			item.Needs = append(item.Needs, stateOf(byID, edge.To))
		}
	}
	if said := f.Comments[bead]; len(said) > 2 {
		item.Comments = said[len(said)-2:]
	} else {
		item.Comments = said
	}
	return item
}

// finish adds the evidence every kind of item can carry: its epic closed, its
// replacement closed.
func (f NeedsYouFacts) finish(byID map[string]domain.GraphBead, item needsYouItem) needsYouItem {
	b := byID[item.Bead]
	for _, edge := range b.Edges {
		if edge.Kind == domain.EdgeParentChild && strings.EqualFold(byID[edge.To].Status, StatusClosed) {
			item.Evidence = append(item.Evidence, fmt.Sprintf("its epic %s is closed", edge.To))
		}
	}
	for _, replacement := range f.replacements(byID, item.Bead) {
		if strings.EqualFold(byID[replacement.id].Status, StatusClosed) {
			item.Evidence = append(item.Evidence, fmt.Sprintf("its replacement %s is closed (%s)", replacement.id, replacement.how))
		}
	}
	return item
}

// parkedCard is the evidence that the Mayor or he parked the bead a card was
// asked on: a grilling or a bug that is held, an icebox bead, or one whose epic is
// held.
func (f NeedsYouFacts) parkedCard(byID map[string]domain.GraphBead, bead string) []string {
	b := byID[bead]
	var evidence []string
	held := strings.EqualFold(b.Status, StatusHeld)
	switch {
	case held && b.IsGrilling():
		evidence = append(evidence, fmt.Sprintf("%s is parked: a grilling that is held", bead))
	case held && strings.EqualFold(b.Type, LabelBug):
		evidence = append(evidence, fmt.Sprintf("%s is parked: a bug that is held", bead))
	case hasLabel(b.Labels, LabelIcebox):
		evidence = append(evidence, fmt.Sprintf("%s is parked: it is in the icebox", bead))
	}
	for _, edge := range b.Edges {
		if edge.Kind == domain.EdgeParentChild && strings.EqualFold(byID[edge.To].Status, StatusHeld) && (held || b.IsGrilling()) {
			evidence = append(evidence, fmt.Sprintf("its epic %s is parked (held)", edge.To))
		}
	}
	for _, c := range f.Comments[bead] {
		if strings.HasPrefix(strings.TrimSpace(c.Text), HeldCommentPrefix) {
			evidence = append(evidence, "the Mayor wrote HELD on it: "+clipRunes(oneLine(c.Text), 120))
			break
		}
	}
	return evidence
}

// replacement is a bead that took the place of another, and how that is known.
type replacement struct{ id, how string }

var replacedBy = regexp.MustCompile(`(?i)\b(?:supersed(?:ed|es)|replaced|overtaken|covered|done)\s+(?:in|by|with)\s+([A-Za-z][A-Za-z0-9]*-[A-Za-z0-9._-]*[A-Za-z0-9])`)

// replacements are the beads that took the place of bead: a link of kind
// supersedes or replaces that points at it, and a bead a comment of its names as
// having superseded it.
func (f NeedsYouFacts) replacements(byID map[string]domain.GraphBead, bead string) []replacement {
	var found []replacement
	for _, b := range f.Graph {
		for _, edge := range b.Edges {
			if edge.To == bead && (edge.Kind == "supersedes" || edge.Kind == "replaces") {
				found = append(found, replacement{b.ID, "it links to " + bead + " as " + edge.Kind})
			}
		}
	}
	for _, c := range f.Comments[bead] {
		for _, m := range replacedBy.FindAllStringSubmatch(c.Text, -1) {
			if _, known := byID[m[1]]; known && m[1] != bead {
				found = append(found, replacement{m[1], "a comment says " + strings.TrimSpace(m[0])})
			}
		}
	}
	return found
}

// staleIdeas are the unticked items of the live Ideas only cards whose idea the
// Mayor has handled: a bead or a handoff names its txid.
func (f NeedsYouFacts) staleIdeas() []needsYouItem {
	ticked := map[string]map[int]bool{}
	for _, record := range f.Cards {
		if record.Re == "" {
			continue
		}
		if ticked[record.Re] == nil {
			ticked[record.Re] = map[int]bool{}
		}
		for _, n := range record.Tick {
			ticked[record.Re][n] = true
		}
	}
	var evidence []NeedsYouText
	read := false
	var items []needsYouItem
	for _, card := range f.Cards {
		if card.Re != "" || !strings.HasPrefix(card.Title, ideasOnlyCardPrefix) {
			continue
		}
		at, _ := time.Parse(time.RFC3339, card.At)
		for _, item := range card.Items {
			if ticked[card.Txid][item.N] {
				continue
			}
			idea, ok := heldIdeaOf(item.Text, f.Released)
			if !ok {
				continue
			}
			if !read && f.Evidence != nil {
				evidence, read = f.Evidence(), true
			}
			for _, text := range evidence {
				if namesTxid(text.Text, idea.Txid) {
					items = append(items, needsYouItem{
						Bead: "ideas card " + card.Txid + " item " + fmt.Sprint(item.N), Kind: "ideas-card item", Filed: at,
						Title:    clipRunes(oneLine(item.Text), 160),
						Evidence: []string{fmt.Sprintf("%s names its txid %s", text.Source, clipRunes(idea.Txid, 18))},
						Action:   fmt.Sprintf("tick-item (mw card update %s --tick %d)", card.Txid, item.N),
					})
					break
				}
			}
		}
	}
	return items
}

// heldIdeaOf is the released idea an Ideas only card's item is of: the item
// holds the idea's words as the card made them, '|' as '/' and a line break as a
// space, then where he sent it.
func heldIdeaOf(item string, released []IdeasOnlyHeld) (IdeasOnlyHeld, bool) {
	text := strings.NewReplacer("|", "/", "\n", " ")
	for _, idea := range released {
		if words := text.Replace(idea.Text); words != "" && strings.Contains(item, words+" ("+idea.Channel+",") {
			return idea, true
		}
	}
	return IdeasOnlyHeld{}, false
}

// namesTxid is whether text names the txid: in full, or by its first 11
// characters after 'direct:', which is how the Mayor shortens one.
func namesTxid(text, txid string) bool {
	if txid == "" {
		return false
	}
	if strings.Contains(text, txid) {
		return true
	}
	const shortest = 18
	return len(txid) >= shortest && strings.Contains(text, txid[:shortest])
}

var versionNamed = regexp.MustCompile(`\bv?(\d+\.\d+\.\d+)\b`)

// versionIn is the first version a demo's text names, without its v.
func versionIn(text string) string {
	if m := versionNamed.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// addedAt is when the Mayor added the step of the hands note, zero when it does
// not say.
func addedAt(raw, step string) time.Time {
	records, _ := parseHandsSteps(raw)
	for _, record := range records {
		if record.ID == step {
			at, _ := time.Parse(time.RFC3339, record.AddedAt)
			return at
		}
	}
	return time.Time{}
}

// stateOf is 'mw-a.1 (closed)': the bead and its status, or '(unknown)' for
// one the graph does not hold.
func stateOf(byID map[string]domain.GraphBead, id string) string {
	b, ok := byID[id]
	if !ok {
		return id + " (unknown)"
	}
	status := b.Status
	if strings.EqualFold(status, StatusHeld) {
		status = "deferred"
	}
	return id + " (" + status + ")"
}

// NeedsYouSurvey reads the facts needs-you.txt is built from out of the tracker,
// the card log, the ideas given back and the rigs' versions. A source it is not
// given adds nothing, and one that cannot be read costs only what it would have
// said.
type NeedsYouSurvey struct {
	Tracker WorkTracker
	Graph   BeadGraph
	Notes   DamperNotes
	// Comments reads the comments of the queue's beads.
	Comments interface {
		StoriesComments(ctx context.Context, ids []string) (map[string][]Comment, error)
	}
	// Cards is the log of the cards sent.
	Cards CardLog
	// Released reads the ideas given back, oldest first.
	Released func(ctx context.Context) ([]IdeasOnlyHeld, error)
	// Evidence reads the texts an idea's txid may be named in.
	Evidence func(ctx context.Context) ([]NeedsYouText, error)
	// Versions reads each rig's current version, by rig name.
	Versions func(ctx context.Context) map[string]string
	Limit    int
	Now      func() time.Time
}

// Read reads the facts. The queue itself must be read: its failure is the
// error; the rest is said to be missing by what it leaves out.
func (s NeedsYouSurvey) Read(ctx context.Context) (NeedsYouFacts, error) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	facts := NeedsYouFacts{DamperFacts: DamperFacts{Now: now, Notes: map[string]string{}}, Limit: s.Limit}
	var err error
	if facts.Work, err = s.Tracker.WorkInHand(ctx); err != nil {
		return facts, fmt.Errorf("reading what is in hand: %w", err)
	}
	if facts.Graph, err = s.Graph.BeadGraph(ctx); err != nil {
		return facts, fmt.Errorf("reading the beads: %w", err)
	}
	for _, prefix := range []string{PosternQuestionKey(""), HandsStepsKey("")} {
		kept, err := s.Notes.NotesWithPrefix(ctx, prefix)
		if err != nil {
			return facts, fmt.Errorf("reading the notes under %s: %w", prefix, err)
		}
		for key, value := range kept {
			facts.Notes[key] = value
		}
	}
	if s.Comments != nil {
		if ids := facts.CommentBeads(); len(ids) > 0 {
			if facts.Comments, err = s.Comments.StoriesComments(ctx, ids); err != nil {
				return facts, fmt.Errorf("reading the comments of the queue: %w", err)
			}
		}
	}
	if s.Versions != nil {
		facts.Versions = s.Versions(ctx)
	}
	if s.Cards != nil {
		if facts.Cards, err = s.Cards.List(ctx); err != nil {
			facts.Missing = append(facts.Missing, fmt.Sprintf("the cards sent could not be read: %v; the Ideas only cards are not swept", err))
		}
	}
	if s.Released != nil {
		if facts.Released, err = s.Released(ctx); err != nil {
			facts.Missing = append(facts.Missing, fmt.Sprintf("the ideas given back could not be read: %v; the Ideas only cards are not swept", err))
		}
	}
	if s.Evidence != nil {
		facts.Evidence = func() []NeedsYouText {
			texts, _ := s.Evidence(ctx)
			return texts
		}
	}
	return facts, nil
}
