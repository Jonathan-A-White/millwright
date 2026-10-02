package application

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// PosternActedAnswer is what the reply under a card says, before the option it
// was answered with: his own acts did what that option asks.
const PosternActedAnswer = "Answered by your acts: "

// beadHolds reports whether bead is in state, one of domain.OptionExpectStates:
// open (open or being worked, not held, not finished), held (deferred), closed,
// landed (closed after mw next landed it) or verified (landed, and a comment
// begins VERIFIED, as the Governor's verified tap and the Mayor's check write
// it). comments is read only for verified.
func beadHolds(bead StoryDetail, state string, comments func() []Comment) bool {
	switch state {
	case domain.ExpectOpen:
		return workable(bead)
	case domain.ExpectHeld:
		return bead.Held()
	case domain.ExpectClosed:
		return bead.Closed()
	case domain.ExpectLanded:
		return bead.Closed() && hasLanded(bead)
	case domain.ExpectVerified:
		if !bead.Closed() || !hasLanded(bead) {
			return false
		}
		for _, c := range comments() {
			if commentMarksVerified(c.Text) {
				return true
			}
		}
	}
	return false
}

// actedBeads is the beads an act on bead changed the state of: bead, and the
// stories under it when it is an epic, which a release or hold of the epic
// reaches. A bead the tracker cannot show is just itself.
func (i PosternInbox) actedBeads(ctx context.Context, bead string) []string {
	acted := []string{bead}
	found, err := i.Tracker.ShowBeads(ctx, []string{bead})
	if err != nil || len(found) == 0 || !found[0].IsEpic {
		return acted
	}
	if epic, err := i.Tracker.ShowEpic(ctx, bead); err == nil {
		for _, story := range epic.Stories {
			acted = append(acted, story.Story.ID)
		}
	}
	return acted
}

// answerByActs, run once one of the Governor's acts has been applied to the
// beads in acted, answers every open card whose one option's expectations —
// each from mw postern send --option '<text>|<bead>:<state>,...' — all hold,
// and that names one of acted: his acts did what that option asks. The card's
// bead is commented ANSWER (by his acts), citing m, the act that completed it;
// its note is cleared; the Mayor is mailed; and a reply is posted under the
// card. Nothing is released or held on such an answer. A card with two options
// holding, or none, is left open, as one that names none of acted. The act
// is already applied, so a failure here is said and fails nothing.
func (i PosternInbox) answerByActs(ctx context.Context, m PosternInboxMessage, acted ...string) {
	if i.Tracker == nil || i.Memory == nil {
		return
	}
	notes, err := i.Memory.NotesWithPrefix(ctx, PosternQuestionKey(""))
	if err != nil {
		i.printf("mw postern inbox: reading the open questions failed, so none was checked against his acts: %v\n", err)
		return
	}
	keys := make([]string, 0, len(notes))
	for key := range notes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var note posternQuestionNote
		if err := json.Unmarshal([]byte(notes[key]), &note); err != nil || !note.expectsAny(acted) {
			continue
		}
		bead := strings.TrimPrefix(key, PosternQuestionKey(""))
		option, ok, err := i.optionHeld(ctx, note)
		if err != nil {
			i.printf("mw postern inbox: checking %s's question against his acts failed: %v\n", bead, err)
			continue
		}
		if ok {
			i.recordActedAnswer(ctx, m, bead, note, option)
		}
	}
}

// expectsAny reports whether an option of the note expects something of one of
// beads.
func (n posternQuestionNote) expectsAny(beads []string) bool {
	for _, expected := range n.Expect {
		for _, e := range expected {
			if slices.Contains(beads, e.Bead) {
				return true
			}
		}
	}
	return false
}

// optionHeld is the one option of note whose expectations all hold now; ok is
// false when none does, or two do.
func (i PosternInbox) optionHeld(ctx context.Context, note posternQuestionNote) (option string, ok bool, err error) {
	var ids []string
	for _, expected := range note.Expect {
		for _, e := range expected {
			if !slices.Contains(ids, e.Bead) {
				ids = append(ids, e.Bead)
			}
		}
	}
	found, err := i.Tracker.ShowBeads(ctx, ids)
	if err != nil {
		return "", false, err
	}
	beads := make(map[string]StoryDetail, len(found))
	for _, bead := range found {
		beads[bead.Story.ID] = bead
	}
	var readErr error
	commentsOf := func(id string) func() []Comment {
		return func() []Comment {
			comments, err := i.Tracker.StoryComments(ctx, id)
			if err != nil && readErr == nil {
				readErr = err
			}
			return comments
		}
	}
	holding := 0
	for _, text := range note.Options {
		expected := note.Expect[text]
		all := len(expected) > 0
		for _, e := range expected {
			bead, known := beads[e.Bead]
			if !known || !beadHolds(bead, e.State, commentsOf(e.Bead)) {
				all = false
				break
			}
		}
		if all {
			option, holding = text, holding+1
		}
	}
	if readErr != nil {
		return "", false, readErr
	}
	return option, holding == 1, nil
}

// recordActedAnswer answers bead's card with option, as recordAnswer answers
// one by a tap, but by his acts: m is the act that completed the card.
func (i PosternInbox) recordActedAnswer(ctx context.Context, m PosternInboxMessage, bead string, note posternQuestionNote, option string) {
	comment := fmt.Sprintf("ANSWER (by his acts) %s from %s, txid %s: %s", sentInFull(m.Ts), orUnknown(m.From), m.Txid, option)
	if err := i.Tracker.CommentOnStory(ctx, bead, comment); err != nil {
		i.printf("mw postern inbox: answering %s's question by his acts failed: %v\n", bead, err)
		return
	}
	if err := i.Memory.ClearNote(ctx, PosternQuestionKey(bead)); err != nil {
		i.printf("mw postern inbox: %s's question is answered by his acts, but its note was not cleared: %v\n", bead, err)
		return
	}
	subject := fmt.Sprintf("Answer: %s: %s (by his acts)", bead, clippedTo(strings.Join(strings.Fields(option), " "), PosternAnswerSubjectLimit))
	if err := i.mail(ctx, subject, comment); err != nil {
		i.printf("mw postern inbox: the answer to %s is recorded, but mailing the Mayor failed: %v\n", bead, err)
	}
	if i.Sender == nil || strings.TrimSpace(note.Txid) == "" {
		return
	}
	reply := PosternSendRequest{Class: "message", Text: PosternActedAnswer + option, Thread: bead, Re: note.Txid, Recorded: true}
	if _, err := i.Sender.Run(ctx, reply); err != nil {
		i.printf("mw postern inbox: %s's question is answered by his acts, but telling him under the card failed: %v\n", bead, err)
	}
}
