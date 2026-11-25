package application

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// MayorToRunMark opens the comment a landing writes for an acceptance criterion
// whose own text hands it to the Mayor and that the Builder said it handed over
// (mw-gq6.1578): 'MAYOR TO RUN: AC2: <the criterion>'. mw verify list shows it
// under the landing.
const MayorToRunMark = "MAYOR TO RUN:"

var (
	// handedToMayor is a line of a closing comment that says a criterion went to
	// the Mayor: 'handed to the Mayor', 'left for the Mayor', 'the Mayor runs it'.
	handedToMayor = regexp.MustCompile(`(?i)\b(hand(?:ed|s|ing)?|left|leave|leaves|pass(?:ed)?|for|to)\b[^.;]*\bthe mayor\b|\bthe mayor\b[^.;]*\b(?:runs?|will run|to run)\b`)
	// mayorRunsIt is a criterion that names the Mayor as the one who runs it.
	mayorRunsIt = regexp.MustCompile(`(?i)\bthe mayor\b[^.;]{0,40}\b(?:runs?|will run|to run)\b|\bmayor runs\b`)
	acRefNumber = regexp.MustCompile(`(?i)\bAC ?(\d+)\b`)
)

// MayorHanded are the acceptance criteria a closing comment hands to the Mayor
// where the criterion itself names him as its runner.
type MayorHanded struct {
	// ACs are the criteria numbers, ascending, each once.
	ACs []int
	// Texts are each criterion's text from the acceptance, by number.
	Texts map[int]string
}

// Found says whether any criterion was handed over.
func (h MayorHanded) Found() bool { return len(h.ACs) > 0 }

// Comment is the comment the landing writes: one MAYOR TO RUN: line for each
// criterion.
func (h MayorHanded) Comment() string {
	var lines []string
	for _, n := range h.ACs {
		lines = append(lines, fmt.Sprintf("%s AC%d: %s", MayorToRunMark, n, firstCharacters(h.Texts[n], 400)))
	}
	return strings.Join(lines, "\n")
}

// UnmetCriteriaAfterHandoff is UnmetCriteria of a closing comment with the lines
// left out that hand a criterion to the Mayor: a line that names criteria, each
// of whose text in acceptance names the Mayor as its runner, and says it was
// handed to him. A criterion that does not name him, one the acceptance does not
// have, or a line with no hand-over still stands unmet (mw-gq6.1578). A sentence
// that says only optional criteria are unmet ('Optional and preferred if small')
// is left out too: the comment says which was done (mw-gq6.1602).
func UnmetCriteriaAfterHandoff(comment, acceptance string) (unmet []string, handed MayorHanded) {
	handed.Texts = map[int]string{}
	seen := map[int]bool{}
	for _, line := range strings.Split(comment, "\n") {
		sentences := withoutOptionalUnmet(UnmetCriteria(line), acceptance)
		if len(sentences) == 0 {
			continue
		}
		numbers, ok := handedCriteria(line, acceptance, handed.Texts)
		if !ok {
			unmet = append(unmet, sentences...)
			continue
		}
		for _, n := range numbers {
			if !seen[n] {
				seen[n] = true
				handed.ACs = append(handed.ACs, n)
			}
		}
	}
	sortInts(handed.ACs)
	return unmet, handed
}

// handedCriteria is the criteria numbers a line hands to the Mayor; ok is false
// unless the line names at least one, says it handed it over, and every one it
// names is a criterion of acceptance that the Mayor runs.
func handedCriteria(line, acceptance string, texts map[int]string) (numbers []int, ok bool) {
	masked := maskQuoted(line)
	if !handedToMayor.MatchString(masked) {
		return nil, false
	}
	for _, m := range acRefNumber.FindAllStringSubmatch(masked, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, false
		}
		text, found := acceptanceCriterion(acceptance, n)
		if !found || !mayorRunsIt.MatchString(text) {
			return nil, false
		}
		texts[n] = text
		numbers = append(numbers, n)
	}
	return numbers, len(numbers) > 0
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// landedWithMayorToRun writes the MAYOR TO RUN: comment of a landing whose
// closing comment hands criteria to the Mayor, once, and gives the block the Landed
// mail carries. Nothing is written when the bead already has the comment, or
// when it cannot be written: a note, and the landing stands.
func (n Next) landedWithMayorToRun(ctx context.Context, c *closeOut, report *NextReport, comments []Comment) string {
	at := closingCommentAt(comments)
	if at < 0 {
		return ""
	}
	_, handed := UnmetCriteriaAfterHandoff(comments[at].Text, c.detail.Acceptance)
	if !handed.Found() {
		return ""
	}
	text := handed.Comment()
	for _, comment := range comments {
		if strings.TrimSpace(comment.Text) == text {
			return text
		}
	}
	if err := n.Tracker.CommentOnStory(ctx, c.id, text); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the MAYOR TO RUN line of %s could not be written: %v", c.id, err))
	} else {
		report.Notes = append(report.Notes, "wrote on "+c.id+": "+strings.ReplaceAll(text, "\n", "; "))
	}
	return text
}

// mayorToRunLines are the MAYOR TO RUN: lines a landing wrote on the bead that
// no MET-BY-MAYOR: comment of the Mayor's follows: what mw verify list shows
// under the landing (mw-gq6.1578). "" when there are none.
func mayorToRunLines(comments []Comment) string {
	var lines []string
	for _, comment := range comments {
		text := strings.TrimSpace(comment.Text)
		switch {
		case strings.HasPrefix(text, MayorToRunMark):
			lines = append(lines, text)
		case !builderAuthor(comment.Author) && strings.HasPrefix(text, MetByMayorMark):
			lines = nil
		}
	}
	return strings.Join(lines, "\n")
}

// withoutMayorsToRun is found less the criteria the closing comment hands to
// the Mayor (UnmetCriteriaAfterHandoff): 'AC 2 NOT met; handed to the Mayor' of a
// criterion the Mayor runs is not a bug to file.
func (n Next) withoutMayorsToRun(c *closeOut, comments []Comment, found ACNotMet) ACNotMet {
	at := closingCommentAt(comments)
	if at < 0 || !found.Found() {
		return found
	}
	_, handed := UnmetCriteriaAfterHandoff(comments[at].Text, c.detail.Acceptance)
	var kept []int
	for _, number := range found.ACs {
		if !slices.Contains(handed.ACs, number) {
			kept = append(kept, number)
		}
	}
	found.ACs = kept
	return found
}
