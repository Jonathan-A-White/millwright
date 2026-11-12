package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// The comments that mark a card held back by the review-queue limit
// (mw-t5shinh.26): a question that waits for Monday's digest, and a card sent
// over the limit on the Mayor's say-so.
const (
	QuestionForMondayPrefix = "QUESTION for Monday:"
	SentOverLimitPrefix     = "SENT OVER THE LIMIT:"
)

// questionForMondayOptions heads the options in a QUESTION for Monday comment;
// the digest cuts the question off at it.
const questionForMondayOptions = "\n\nOptions:"

// isCardAskedComment says the comment is the record of a card sent to him
// ("QUESTION <time> asked by postern ..."). A QUESTION for Monday is a question
// held back, not a card sent, and is not one.
func isCardAskedComment(text string) bool {
	return strings.HasPrefix(text, "QUESTION ") && !strings.HasPrefix(text, QuestionForMondayPrefix)
}

// ReviewQueueReader counts his review queue the way the DAMPERS review line
// does, and says the limit it is held to.
type ReviewQueueReader interface {
	ReviewQueue(ctx context.Context) (size, limit int, err error)
}

// CloudMakeReader says the cloud make alarms, a line for each. A ReviewQueueReader
// that is also one has them said by the hold when it counts the queue.
type CloudMakeReader interface {
	CloudMakeLines(ctx context.Context, now time.Time) []string
}

// ReviewQueue is the size of his review queue (open cards, open demos and unrun
// hands steps) and the limit it is held to, as ReadDampers counts them.
func (s DamperSurvey) ReviewQueue(ctx context.Context) (size, limit int, err error) {
	facts := DamperFacts{Notes: map[string]string{}}
	if s.Tracker != nil {
		if facts.Work, err = s.Tracker.WorkInHand(ctx); err != nil {
			return 0, 0, fmt.Errorf("reading what is in hand: %w", err)
		}
	}
	if s.Notes != nil {
		for _, prefix := range []string{PosternQuestionKey(""), HandsStepsKey("")} {
			kept, err := s.Notes.NotesWithPrefix(ctx, prefix)
			if err != nil {
				return 0, 0, fmt.Errorf("reading the notes under %s: %w", prefix, err)
			}
			for key, value := range kept {
				facts.Notes[key] = value
			}
		}
	}
	if s.Graph != nil {
		if facts.Graph, err = s.Graph.BeadGraph(ctx); err != nil {
			return 0, 0, fmt.Errorf("reading the beads: %w", err)
		}
	}
	cards, demos, hands := reviewQueue(facts)
	return len(cards) + len(demos) + len(hands), s.Limits.orDefaults().ReviewQueue, nil
}

// ReviewHold keeps a new decision card from a queue already over its limit:
// the question is written on its bead as a QUESTION for Monday, for Monday's
// digest to carry, unless the Mayor gives a reason it cannot wait.
type ReviewHold struct {
	Queue   ReviewQueueReader
	Tracker WorkTracker
	// Out is where the line naming the held bead is said; Err where a queue
	// that could not be counted is.
	Out io.Writer
	Err io.Writer
	// Sender is the command that asks, named in what the hold says ("mw grill
	// --ask"); empty is "mw postern send".
	Sender string
}

// sender is the name the hold's lines open with.
func (h ReviewHold) sender() string {
	if h.Sender == "" {
		return "mw postern send"
	}
	return h.Sender
}

// ReviewVerdict is what the queue said about one request.
type ReviewVerdict struct {
	// Over is whether the request is a card asked of a bead while the queue is
	// over its limit; Size and Limit are what it was counted against.
	Over        bool
	Size, Limit int
}

// Judge counts the queue for a card asked of a bead (a decision-needed request
// with Bead set); any other request is not held. A queue that cannot be counted
// holds nothing, and is said in Err: a question is not lost to a read.
func (h ReviewHold) Judge(ctx context.Context, req PosternSendRequest) ReviewVerdict {
	if h.Queue == nil || req.Class != "decision-needed" || !req.asksQuestion() {
		return ReviewVerdict{}
	}
	h.sayCloudMakes(ctx)
	size, limit, err := h.Queue.ReviewQueue(ctx)
	if err != nil {
		if h.Err != nil {
			fmt.Fprintf(h.Err, "%s: the review queue could not be counted, so the card is sent: %v\n", h.sender(), err)
		}
		return ReviewVerdict{}
	}
	return ReviewVerdict{Over: size > limit, Size: size, Limit: limit}
}

// sayCloudMakes writes, in Err, the cloud make alarms of the queue's own survey:
// a card is about to go out, and a box made for too long is a thing he and the
// Mayor should see with it (mw-gq6.1128).
func (h ReviewHold) sayCloudMakes(ctx context.Context) {
	reader, ok := h.Queue.(CloudMakeReader)
	if !ok || h.Err == nil {
		return
	}
	for _, line := range reader.CloudMakeLines(ctx, time.Now()) {
		fmt.Fprintf(h.Err, "%s: %s\n", h.sender(), line)
	}
}

// Hold writes the question on its bead as a QUESTION for Monday and says so; no
// card is sent.
func (h ReviewHold) Hold(ctx context.Context, req PosternSendRequest, v ReviewVerdict) error {
	if err := h.Tracker.CommentOnStory(ctx, req.Bead, HeldQuestionComment(req)); err != nil {
		return fmt.Errorf("%s: holding the question on %s for Monday: %w", h.sender(), req.Bead, err)
	}
	if h.Out != nil {
		fmt.Fprintf(h.Out, "held %s for Monday: his review queue is %d, over the limit of %d; no card sent, the question is on the bead and Monday's digest carries it (--why-now \"<reason>\" sends it anyway)\n",
			req.Bead, v.Size, v.Limit)
	}
	return nil
}

// Overrode writes, on the bead, that the card went over the limit and why.
func (h ReviewHold) Overrode(ctx context.Context, req PosternSendRequest, v ReviewVerdict, why, txid string) error {
	comment := fmt.Sprintf("%s the review queue was %d, over the limit of %d; sent as %s because: %s",
		SentOverLimitPrefix, v.Size, v.Limit, txid, strings.TrimSpace(why))
	if err := h.Tracker.CommentOnStory(ctx, req.Bead, comment); err != nil {
		return fmt.Errorf("%s: sent as %s, but recording on %s that it went over the limit failed: %w", h.sender(), txid, req.Bead, err)
	}
	return nil
}

// HeldQuestionComment is the comment a held card is written as: the question,
// then every option as given and the one recommended.
func HeldQuestionComment(req PosternSendRequest) string {
	var b strings.Builder
	b.WriteString(QuestionForMondayPrefix + " " + strings.TrimSpace(req.Text))
	b.WriteString(questionForMondayOptions)
	for _, option := range req.Options {
		b.WriteString("\n- " + option)
	}
	if recommend := strings.TrimSpace(req.Recommend); recommend != "" {
		b.WriteString("\nRecommended: " + recommend)
	}
	return b.String()
}

// DigestHeldQuestionClip is how many characters of a held question Monday's
// digest quotes.
const DigestHeldQuestionClip = 200

// HeldQuestion is a question held for Monday that no later answer or card has
// met.
type HeldQuestion struct {
	Bead     string
	Question string
}

// heldQuestionOf reads a bead's comments (oldest first): the question of its
// newest QUESTION for Monday, when no ANSWER and no card sent comes after it.
func heldQuestionOf(comments []Comment) (string, bool) {
	held := -1
	for i, c := range comments {
		if strings.HasPrefix(c.Text, QuestionForMondayPrefix) {
			held = i
		}
	}
	if held < 0 {
		return "", false
	}
	for _, c := range comments[held+1:] {
		if strings.HasPrefix(c.Text, "ANSWER ") || isCardAskedComment(c.Text) {
			return "", false
		}
	}
	question, _, _ := strings.Cut(strings.TrimPrefix(comments[held].Text, QuestionForMondayPrefix), questionForMondayOptions)
	return clipRunes(strings.Join(strings.Fields(question), " "), DigestHeldQuestionClip), true
}
