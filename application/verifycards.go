package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// VerifyKindCard says a candidate of mw verify match is a standing question
// card, not a check.
const VerifyKindCard = "card"

// verifyCardWordsBytes cuts his words where a proposal and the mail quote them.
const verifyCardWordsBytes = 300

// VerifyCardBeads is what mw verify match reads to find the standing question
// cards: the postern.question.<bead> notes, and the status of each bead.
type VerifyCardBeads interface {
	NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error)
	ShowBeads(ctx context.Context, ids []string) ([]StoryDetail, error)
}

// VerifyCard is a standing question card on an open bead that has a message of
// his written after it was asked, in the bead's thread or in general: a card he
// may have answered in his own words.
type VerifyCard struct {
	Bead string
	// Txid is the card's own txid, as its note records it.
	Txid        string
	Text        string
	Recommended string
	Options     []string
	AskedAt     time.Time
	// Earlier is true when the bead holds overtaken cards beside this one, so
	// clearing it must name its txid.
	Earlier bool
	// Answers are his messages after the card was asked, oldest first.
	Answers []VerifyCardAnswer
}

// VerifyCardAnswer is one message of his that may answer a card.
type VerifyCardAnswer struct {
	Txid  string
	At    time.Time
	Words string
}

// ReadVerifyCards reads the standing cards of open beads that have a later
// message of his among evidence, by bead. The question of a note that does not
// carry it is read from the bead's QUESTION comment. A card whose asking time is
// not known has no message known to come after it and is left out.
func ReadVerifyCards(ctx context.Context, beads VerifyBeads, source VerifyCardBeads, evidence []VerifyEvidence) ([]VerifyCard, error) {
	if source == nil || len(evidence) == 0 {
		return nil, nil
	}
	notes, err := source.NotesWithPrefix(ctx, PosternQuestionKey(""))
	if err != nil {
		return nil, fmt.Errorf("reading the standing cards: %w", err)
	}
	var ids []string
	for _, key := range sortedKeys(notes) {
		if bead, ok := strings.CutPrefix(key, PosternQuestionKey("")); ok && bead != "" && strings.TrimSpace(notes[key]) != "" {
			ids = append(ids, bead)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	details, err := source.ShowBeads(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("reading the beads of the standing cards: %w", err)
	}
	open := map[string]bool{}
	for _, d := range details {
		open[d.Story.ID] = !strings.EqualFold(d.Status, StatusClosed)
	}
	var maybe []string
	for _, id := range ids {
		if open[id] {
			maybe = append(maybe, id)
		}
	}
	messages := keptMessagesOf(evidence)
	var cards []VerifyCard
	var comments map[string][]Comment
	for _, bead := range maybe {
		value := notes[PosternQuestionKey(bead)]
		card := VerifyCard{Bead: bead, Txid: strings.TrimSpace(value)}
		var note posternQuestionNote
		if json.Unmarshal([]byte(value), &note) == nil {
			card.Txid, card.Earlier = note.Txid, len(note.Earlier) > 0
		}
		facts, ok := questionFromNote(value)
		if !ok {
			if comments == nil {
				if comments, err = beads.StoriesComments(ctx, maybe); err != nil {
					return nil, fmt.Errorf("reading the comments of the standing cards: %w", err)
				}
			}
			facts, ok = questionFromComments(comments[bead])
		}
		if !ok || facts.AskedAt.IsZero() {
			continue
		}
		card.Text, card.Recommended, card.Options, card.AskedAt = facts.Text, facts.Recommended, facts.Options, facts.AskedAt
		for _, m := range messages {
			if m.at.After(card.AskedAt) && (m.thread == bead || m.thread == PosternGeneralThread) && m.txid != card.Txid {
				card.Answers = append(card.Answers, VerifyCardAnswer{Txid: m.txid, At: m.at, Words: m.words})
			}
		}
		if len(card.Answers) > 0 {
			cards = append(cards, card)
		}
	}
	return cards, nil
}

// keptOfEvidence is one saved message of his with the thread and the time the
// inbox kept for it.
type keptOfEvidence struct {
	txid, thread, words string
	at                  time.Time
}

// keptMessagesOf reads the whole message the inbox kept beside each evidence
// file (<txid>.message.json), oldest first. Evidence with none kept has no known
// thread and is left out.
func keptMessagesOf(evidence []VerifyEvidence) []keptOfEvidence {
	var out []keptOfEvidence
	for _, e := range evidence {
		if len(e.Files) == 0 {
			continue
		}
		raw, err := os.ReadFile(PosternKeptFile(filepath.Dir(e.Files[0]), e.Txid))
		if err != nil {
			continue
		}
		var kept keptMessage
		if json.Unmarshal(raw, &kept) != nil || strings.TrimSpace(kept.Thread) == "" {
			continue
		}
		at := kept.Ts.UTC()
		if kept.Ts.IsZero() {
			at = e.At
		}
		out = append(out, keptOfEvidence{txid: e.Txid, thread: kept.Thread, words: strings.TrimSpace(kept.Text), at: at})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].at.Before(out[b].at) })
	return out
}

// cardFileName is the file in the agent's dir that holds the card of bead.
func cardFileName(bead string) string { return "card-" + bead + ".txt" }

// cardFile is the text the agent reads for a card.
func cardFile(c VerifyCard) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "bead: %s\nkind: card\nasked: %s\nquestion: %s\n", c.Bead, c.AskedAt.Format(verifyTimeFormat), c.Text)
	if c.Recommended != "" {
		fmt.Fprintf(&b, "recommended: %s\n", c.Recommended)
	}
	b.WriteString("options:\n")
	for _, o := range c.Options {
		fmt.Fprintf(&b, "- %s\n", o)
	}
	if len(c.Options) == 0 {
		b.WriteString("(none: an open question)\n")
	}
	b.WriteString("\nhis messages written after it was asked, in this bead's thread or in general (read each file; a txid is the one named):\n")
	for _, a := range c.Answers {
		fmt.Fprintf(&b, "- %s, written %s\n", a.Txid, a.At.Format(verifyTimeFormat))
	}
	return []byte(b.String())
}

// optionOf is the card's option the agent's option names: the whole option
// text, or its leading word or letter ("B" for "B: Drop it"). ok is false when
// the card offers options and none is named; a card with no options takes the
// agent's words as they are.
func optionOf(c VerifyCard, said string) (string, bool) {
	said = strings.TrimSpace(said)
	if len(c.Options) == 0 {
		return said, true
	}
	for _, o := range c.Options {
		if strings.EqualFold(strings.TrimSpace(o), said) {
			return o, true
		}
	}
	lead := strings.ToLower(strings.Trim(said, " .:)("))
	if lead == "" {
		return "", false
	}
	for _, o := range c.Options {
		first := strings.ToLower(strings.Trim(strings.FieldsFunc(o, func(r rune) bool { return r == ':' || r == ' ' || r == '.' || r == ')' })[0], " .:)("))
		if first == lead {
			return o, true
		}
	}
	return "", false
}

// quotedWords is his words as a proposal and its mail quote them: on one line,
// cut, and with the marks a shell would read taken out, so the clear line can be
// run as it stands.
func quotedWords(words string) string {
	words = strings.Join(strings.Fields(words), " ")
	words = strings.NewReplacer(`"`, "'", "`", "'", `\`, "/", "$", "S").Replace(words)
	if len(words) > verifyCardWordsBytes {
		n := verifyCardWordsBytes
		for n > 0 && words[n]&0xC0 == 0x80 {
			n--
		}
		words = strings.TrimSpace(words[:n]) + "..."
	}
	return words
}

// cardClearLine is the line the Mayor runs to clear a card he answered.
func cardClearLine(p VerifyProposal) string {
	why := fmt.Sprintf("he answered %s in his own words (%s, %s): '%s'", p.Option, p.Txid, p.At.Format(verifyTimeFormat), p.Words)
	line := fmt.Sprintf(`mw postern clear %s --why "%s"`, p.Bead, why)
	if p.CardEarlier {
		line += " --txid " + p.CardTxid
	}
	return line
}

// proposeCards writes the PROPOSED comment of each card answer (a match with an
// option, or one on a bead that is a card and no check) at or over the
// confidence that names one of the card's own later messages and an option it
// offers, unless the card is already proposed for that message. It clears
// nothing and changes no note.
func (w verifyProposer) proposeCards(ctx context.Context, cards []VerifyCard, matches []verifyMatchLine) ([]VerifyProposal, []verifyMatchRecord, error) {
	byBead := map[string]VerifyCard{}
	var ids []string
	for _, c := range cards {
		byBead[c.Bead] = c
		ids = append(ids, c.Bead)
	}
	comments, err := w.Beads.StoriesComments(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the comments before proposing: %w", err)
	}
	var proposals []VerifyProposal
	var records []verifyMatchRecord
	var errs []error
	done := map[string]bool{}
	for _, m := range matches {
		m.Shows = strings.Join(strings.Fields(m.Shows), " ")
		record := verifyMatchRecord{verifyMatchLine: m}
		card, known := byBead[m.Bead]
		var answer VerifyCardAnswer
		for _, a := range card.Answers {
			if a.Txid == m.Txid {
				answer = a
			}
		}
		option, optionOK := optionOf(card, m.Option)
		switch {
		case !known:
			record.Outcome = "left out: no such card"
		case answer.Txid == "":
			record.Outcome = "left out: not a message of his after the card, in its thread or in general"
		case m.Confidence < w.Confidence:
			record.Outcome = fmt.Sprintf("left out: confidence under %.2f", w.Confidence)
		case !optionOK:
			record.Outcome = "left out: not an option the card offers"
		case done[m.Bead+" "+m.Txid] || proposedFor(comments[m.Bead], m.Txid):
			record.Outcome = "left out: already proposed for this txid"
		default:
			done[m.Bead+" "+m.Txid] = true
			words := quotedWords(answer.Words)
			text := fmt.Sprintf("%s CLEARING the card %s (asked %s): he answered %s in his own words, from %s (%s): \"%s\" (confidence %.2f, %s, run %s)",
				VerifyProposedWord, card.Txid, card.AskedAt.Format(verifyTimeFormat), option, answer.Txid, answer.At.Format(verifyTimeFormat),
				words, m.Confidence, w.Model, w.Run)
			if err := w.Beads.CommentOnStory(ctx, m.Bead, text); err != nil {
				record.Outcome = "not written: " + err.Error()
				errs = append(errs, fmt.Errorf("commenting on %s: %w", m.Bead, err))
			} else {
				record.Outcome = verifyProposedOutcome
				proposals = append(proposals, VerifyProposal{
					Bead: m.Bead, Txid: answer.Txid, At: answer.At, Shows: m.Shows, Confidence: m.Confidence,
					Card: true, Option: option, Words: words, CardTxid: card.Txid, CardEarlier: card.Earlier,
				})
			}
		}
		records = append(records, record)
	}
	return proposals, records, errors.Join(errs...)
}
