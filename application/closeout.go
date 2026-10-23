package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// CloseOutMark is the word a running close-out (mw next, not --heartbeat)
// leaves on its host that it is at work on a story: the session is over, but
// the story is not quiet — it is being landed, or waiting its turn or for the
// host to calm before the rig's tests.
type CloseOutMark struct {
	Story string
	// Since is when the close-out began.
	Since time.Time
	// Calming says the close-out is waiting for a busy host to calm.
	Calming bool
}

// CloseOutMarks is where a close-out says it is running, outside the tracker:
// a mark only this host reads, written by mw next and read straight by mw
// status and the quiet alarm. The adapter is a file per story in this host's
// state directory, and a mark whose process has gone is no mark.
type CloseOutMarks interface {
	// Write records mark, replacing whatever mark the story already had.
	Write(ctx context.Context, mark CloseOutMark) error

	// Read is the marks of the close-outs running now. Marks left by a process
	// that is gone are not among them.
	Read(ctx context.Context) ([]CloseOutMark, error)

	// Clear removes the story's mark. One that is not there is already what was
	// asked for, and is not an error.
	Clear(ctx context.Context, story string) error
}

// closingOut reads the close-outs running on this host by story. A nil
// marker, or marks that cannot be read, is none: a story is then judged as it
// was before close-outs left marks.
func closingOut(ctx context.Context, marks CloseOutMarks) map[string]CloseOutMark {
	running := map[string]CloseOutMark{}
	if marks == nil {
		return running
	}
	list, err := marks.Read(ctx)
	if err != nil {
		return running
	}
	for _, m := range list {
		running[m.Story] = m
	}
	return running
}

// since is how mw status words the mark: "closing out 21m", and "waiting for
// calm" beside it when the close-out is waiting for the host to calm.
func (m CloseOutMark) since(now time.Time) string {
	s := fmt.Sprintf("closing out %dm", int(now.Sub(m.Since)/time.Minute))
	if m.Calming {
		s += ", waiting for calm"
	}
	return s
}

// markCloseOut writes the close-out's mark for storyID; a mark that cannot be
// written changes nothing about the close-out.
func (n Next) markCloseOut(ctx context.Context, storyID string, since time.Time, calming bool) {
	if n.CloseOuts != nil {
		_ = n.CloseOuts.Write(ctx, CloseOutMark{Story: storyID, Since: since, Calming: calming})
	}
}

// clearCloseOut takes the close-out's mark back out, whatever became of it. It
// uses a context of its own: the close-out may have ended because its did.
func (n Next) clearCloseOut(storyID string) {
	if n.CloseOuts != nil {
		_ = n.CloseOuts.Clear(context.Background(), storyID)
	}
}

// UnmetPhrase is a way a Builder's closing comment says an acceptance
// criterion was not met, and where it was seen.
type UnmetPhrase struct {
	Phrase string
	Source string
}

// UnmetCriterionPhrases are the words a closing comment uses for an acceptance
// criterion it did not meet (mw-gq6.711). The list is from a sweep on
// 2026-10-23 of the comments of the last 200 closed stories: every sentence
// naming an AC beside one of these was a criterion left unmet, and no sentence
// that kept the phrase from an AC was a false match. A phrase is matched
// whole, in any case.
var UnmetCriterionPhrases = []UnmetPhrase{
	{"only in part", "mw-79en7t.9: 'AC2 and AC4 hold only in small part', landed as Landed 2026-10-22"},
	{"only in small part", "mw-79en7t.9"},
	{"held in part", "mw-5r3p30.731: 'held in part'; the sweep's wording for a part-held criterion"},
	{"holds in part", "the same wording in the present tense; no sentence of the sweep, kept with held in part"},
	{"not met", "mw-gq6.600, mw-gq6.608, mw-gq6.621: 'AC 3 NOT MET'"},
	{"not run", "mw-5r3p30.731: 'AC3 (mw grist eval 3 of 3 on today-drill): NOT RUN', landed 2026-10-23; mw-gq6.517, mw-pxji7y.3, mw-pxji7y.4, mw-fm7wmn.20"},
	{"not switched", "mw-gq6.711 and the Mayor's note on it: a flag left off is a criterion unmet; no sentence of the sweep"},
	{"not done", "mw-cvwm1u.3: 'AC4 (live proof) not done', mw-gq6.608, mw-gq6.613, mw-s6387p.1"},
	{"not verified", "mw-gq6.605: 'AC 3 NOT verified', mw-gq6.600"},
}

var (
	criterionRef    = regexp.MustCompile(`(?i)\bAC ?\d+\b|\bacceptance criteri`)
	unmetPhraseExpr = regexp.MustCompile(`(?i)\b(` + unmetPhraseAlternatives() + `)\b`)
	sentenceEnd     = regexp.MustCompile(`[.!?;]\s+|\n`)
)

func unmetPhraseAlternatives() string {
	var words []string
	for _, p := range UnmetCriterionPhrases {
		words = append(words, regexp.QuoteMeta(p.Phrase))
	}
	return strings.Join(words, "|")
}

// UnmetCriteria is the sentences of a closing comment that name an acceptance
// criterion (AC3, 'AC 4', 'acceptance criterion') beside a phrase of
// UnmetCriterionPhrases, trimmed and in the order written. A phrase in a
// sentence that names no criterion is not one: 'the test was not run red' says
// nothing of the story's criteria.
func UnmetCriteria(comment string) []string {
	var found []string
	for _, sentence := range sentenceEnd.Split(comment, -1) {
		sentence = strings.Join(strings.Fields(sentence), " ")
		if criterionRef.MatchString(sentence) && unmetPhraseExpr.MatchString(sentence) {
			found = append(found, strings.Trim(sentence, " •-:"))
		}
	}
	return found
}
