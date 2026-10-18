package application

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LabelFriction tags the bug a landing files for an acceptance criterion its
// closing comment says is not met.
const LabelFriction = "friction"

// acNotMetFirst is how many characters of the unmet criterion the filed bug's
// title carries.
const acNotMetFirst = 80

// acNotMetLine finds 'AC 3 NOT met', 'AC 2 and 4 NOT done', 'AC 1, 3 and 4 NOT
// met' in a line: the numbers right before the marker, which is case-sensitive,
// so 'AC 1, 3 and 4 done; AC 2 NOT done' names AC 2 only and prose saying 'not
// met' names none (mw-gq6.602).
var acNotMetLine = regexp.MustCompile(`\bACs?\s+(\d+(?:\s*(?:,\s*and|,|and|&)\s*\d+)*)\s*:?\s+(?:(?:is|are|was|were)\s+)?NOT (?:met|done)\b`)

var acNumber = regexp.MustCompile(`\d+`)

// ACNotMet is what a closing comment says of the criteria it did not meet.
type ACNotMet struct {
	// ACs are the criteria numbers named, ascending, each once.
	ACs []int
	// Lines are the comment's lines that said so, trimmed, in order.
	Lines []string
}

// Found says whether the comment named any unmet criterion.
func (a ACNotMet) Found() bool { return len(a.ACs) > 0 }

// Words is the numbers as a title says them: '3', '2 and 4', '1, 2 and 5'.
func (a ACNotMet) Words() string {
	parts := make([]string, len(a.ACs))
	for i, n := range a.ACs {
		parts[i] = strconv.Itoa(n)
	}
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// Subject is the Landed mail's verdict for a story that landed with criteria
// unmet: 'Landed, AC 3 NOT met'.
func (a ACNotMet) Subject() string { return MailLanded + ", AC " + a.Words() + " NOT met" }

// ReadACNotMet reads the lines of a closing comment that say an acceptance
// criterion is not met.
func ReadACNotMet(text string) ACNotMet {
	var found ACNotMet
	seen := map[int]bool{}
	for _, line := range strings.Split(text, "\n") {
		matches := acNotMetLine.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			continue
		}
		found.Lines = append(found.Lines, strings.TrimSpace(line))
		for _, m := range matches {
			for _, digits := range acNumber.FindAllString(m[1], -1) {
				if n, err := strconv.Atoi(digits); err == nil && !seen[n] {
					seen[n] = true
					found.ACs = append(found.ACs, n)
				}
			}
		}
	}
	sort.Ints(found.ACs)
	return found
}

// closingACNotMet reads the story's closing comment: the newest that has a 'For
// the rig memory:' section, else every comment, newest first.
func closingACNotMet(comments []Comment) ACNotMet {
	for i := len(comments) - 1; i >= 0; i-- {
		if ParseFactProposals(comments[i].Text).Found {
			return ReadACNotMet(comments[i].Text)
		}
	}
	for i := len(comments) - 1; i >= 0; i-- {
		if found := ReadACNotMet(comments[i].Text); found.Found() {
			return found
		}
	}
	return ACNotMet{}
}

var acNumbered = regexp.MustCompile(`^\s*(\d+)[.)]\s+(.*)$`)

// acceptanceCriterion is criterion number n of an acceptance field written as a
// numbered list, its continuation lines joined; ok is false when it has none.
func acceptanceCriterion(acceptance string, n int) (string, bool) {
	var text []string
	in := false
	for _, line := range strings.Split(acceptance, "\n") {
		if m := acNumbered.FindStringSubmatch(line); m != nil {
			if in {
				break
			}
			if got, _ := strconv.Atoi(m[1]); got == n {
				in = true
				text = append(text, strings.TrimSpace(m[2]))
			}
			continue
		}
		if in {
			text = append(text, strings.TrimSpace(line))
		}
	}
	if !in {
		return "", false
	}
	return strings.Join(strings.Fields(strings.Join(text, " ")), " "), true
}

func firstCharacters(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return strings.TrimSpace(string(runes[:n]))
}

func acNotMetTitle(id string, found ACNotMet, acceptance string) string {
	title := fmt.Sprintf("[bug] %s landed without its AC %s", id, found.Words())
	if first, ok := acceptanceCriterion(acceptance, found.ACs[0]); ok && first != "" {
		title += ": " + firstCharacters(first, acNotMetFirst)
	}
	return title
}

func acNotMetDescription(c *closeOut, found ACNotMet, host string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mw next landed %s (%s) although its closing comment says an acceptance criterion is not met.\n\n", c.id, c.detail.Story.Title)
	b.WriteString("The closing comment says:\n")
	for _, line := range found.Lines {
		fmt.Fprintf(&b, "> %s\n", line)
	}
	b.WriteString("\nThe criteria:\n")
	for _, n := range found.ACs {
		if text, ok := acceptanceCriterion(c.detail.Acceptance, n); ok {
			fmt.Fprintf(&b, "%d. %s\n", n, text)
		} else {
			fmt.Fprintf(&b, "%d. (not found in %s's acceptance field)\n", n, c.id)
		}
	}
	fmt.Fprintf(&b, "\nThe landing stands: the rest of the work is good. Read the comment, word what is left, and release this bug.\n\nSource: mw next on %s (mw-gq6.602).\n", host)
	return b.String()
}

// landedWithoutACs is what a landing does about a closing comment that says an
// acceptance criterion is not met: it files one held bug under the story's
// epic, and gives the verdict the Landed mail is sent under and the block it
// carries. Without such a line the verdict is MailLanded and the block empty.
// The landing is never undone for it: a bug that cannot be filed is a note and
// a mail to the Mayor. A bug of the same title, open or closed, is the one
// already filed, so a close-out run again files nothing (mw-gq6.602).
func (n Next) landedWithoutACs(ctx context.Context, c *closeOut, report *NextReport, again bool, comments []Comment) (verdict, block string) {
	found := closingACNotMet(comments)
	if !found.Found() {
		return MailLanded, ""
	}
	verdict = found.Subject()
	title := acNotMetTitle(c.id, found, c.detail.Acceptance)
	block = fmt.Sprintf("AC %s NOT met, said in the closing comment:\n%s", found.Words(), strings.Join(found.Lines, "\n"))

	lost := func(err error) {
		what := "the bug for the unmet AC " + found.Words() + " of " + c.id + " could not be filed"
		report.Notes = append(report.Notes, fmt.Sprintf("%s: %v", what, err))
		n.mailLostWrite(ctx, c, report, what, fmt.Sprintf("%s landed with AC %s NOT met and nothing asks for it to be done; file a held [bug] titled %q: %v", c.id, found.Words(), title, err))
		block += "\nThe bug for it could not be filed (see the lost write mail)."
	}
	lister, ok := n.Tracker.(TesterStories)
	if !ok && again {
		return verdict, block
	}
	if ok {
		stories, err := readRetrying(ctx, n, report, "the stories", func() ([]StoryDetail, error) { return lister.StoriesSince(ctx, time.Time{}) })
		if err != nil {
			lost(err)
			return verdict, block
		}
		for _, d := range stories {
			if d.Story.Title == title {
				report.Notes = append(report.Notes, fmt.Sprintf("%s already files the unmet AC %s of %s", d.Story.ID, found.Words(), c.id))
				return verdict, block + "\nFiled: " + d.Story.ID + ", held."
			}
		}
	}

	story := NewStory{
		EpicID:      c.detail.EpicID,
		Standalone:  c.detail.EpicID == "",
		Title:       title,
		Description: acNotMetDescription(c, found, n.Host),
		Acceptance: fmt.Sprintf("1. The criterion %s of %s is met, shown by running what it names.\n"+
			"Sweep: none, because this bug is the one unmet criterion of one landing and mw next files it from the closing comment.", found.Words(), c.id),
		Priority:  2,
		Overrides: c.path,
		Labels:    []string{LabelBug, LabelFriction},
	}
	var id string
	err := writeRetrying(ctx, n, report, "the bug for an unmet AC", func() error {
		var err error
		id, err = n.Tracker.CreateStory(ctx, story)
		return err
	})
	if err != nil {
		lost(err)
		return verdict, block
	}
	report.Notes = append(report.Notes, fmt.Sprintf("filed %s, held: %s landed without its AC %s", id, c.id, found.Words()))
	return verdict, block + "\nFiled: " + id + ", held."
}
