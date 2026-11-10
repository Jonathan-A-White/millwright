package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// FoundNotFixedMarker heads the section of a Builder's closing comment that
// lists what it saw and did not fix, one finding to a line, before 'For common
// errors:'. A Builder files no bead (charter, 2026-09-18): mw next files each
// line as a held bug (mw-t5shinh.24).
const FoundNotFixedMarker = "Found, not fixed:"

// LabelFoundAtClose tags a bug that mw next filed from a line of a closing
// comment's 'Found, not fixed:' section.
const LabelFoundAtClose = "found-at-close"

const (
	// foundAtCloseCap is how many bugs one close-out files; the lines after the
	// cap go into one more bug that lists them.
	foundAtCloseCap = 5
	// foundTitleFirst is how many characters of the line the filed bug's title
	// carries.
	foundTitleFirst = 100
)

// foundSectionEnders are the headings that end the section when one starts a
// line after it, beside a blank line after a finding.
var foundSectionEnders = []string{RigMemoryMarker, CommonErrorsMarker, "What's new:", "HOW TO CHECK IT", "Internal:"}

// headingRest is the text after heading when line starts with it, case ignored,
// after any bullet, number or emphasis marks; ok is false when the line does
// not start with it. Only a line that starts with the heading is a heading: a
// mention inside a sentence, between quote marks or behind a '>' is not
// (mw-gq6.726).
func headingRest(line, heading string) (rest string, ok bool) {
	text := strings.TrimLeft(domain.TrimListMarker(strings.TrimSpace(line)), "*_# \t")
	if len(text) < len(heading) || !strings.EqualFold(text[:len(heading)], heading) {
		return "", false
	}
	return strings.TrimLeft(text[len(heading):], "*_ \t"), true
}

// isFoundNotFixedHeading says whether line starts the 'Found, not fixed:'
// section; ParseFactProposals and ParseCommonErrors stop at it, so a section
// written after theirs is not read as theirs.
func isFoundNotFixedHeading(line string) bool {
	_, ok := headingRest(line, FoundNotFixedMarker)
	return ok
}

// FoundNotFixed is what a closing comment's 'Found, not fixed:' section says.
// Found is false when the comment has no such section.
type FoundNotFixed struct {
	Found bool
	// Lines are the findings, each once, in order; empty when the section says
	// nothing.
	Lines []string
}

// ParseFoundNotFixed reads the 'Found, not fixed:' section of one closing
// comment: the text after the heading's colon and the lines after it, a bullet
// or number in front of a line ignored, to the first blank line after a
// finding or the heading of another section. The word nothing is no finding.
// When the comment holds the heading more than once, the last is the section.
func ParseFoundNotFixed(comment string) FoundNotFixed {
	lines := strings.Split(strings.ReplaceAll(comment, "\r\n", "\n"), "\n")
	start, first := -1, ""
	for i, line := range lines {
		if rest, ok := headingRest(line, FoundNotFixedMarker); ok {
			start, first = i, rest
		}
	}
	if start < 0 {
		return FoundNotFixed{}
	}
	found := FoundNotFixed{Found: true}
	seen := map[string]bool{}
	take := func(raw string) {
		text := strings.Join(strings.Fields(domain.TrimListMarker(raw)), " ")
		if text == "" || strings.EqualFold(strings.Trim(text, " .*_`"), "nothing") || seen[text] {
			return
		}
		seen[text] = true
		found.Lines = append(found.Lines, text)
	}
	take(first)
	for _, raw := range lines[start+1:] {
		if strings.TrimSpace(raw) == "" {
			if len(found.Lines) > 0 {
				break
			}
			continue
		}
		if endsFoundSection(raw) {
			break
		}
		take(raw)
	}
	return found
}

func endsFoundSection(line string) bool {
	for _, heading := range foundSectionEnders {
		if _, ok := headingRest(line, heading); ok {
			return true
		}
	}
	return false
}

// closingFoundNotFixed reads the story's closing comment: the newest that has a
// 'For the rig memory:' section, else the newest that has the heading.
func closingFoundNotFixed(comments []Comment) FoundNotFixed {
	for i := len(comments) - 1; i >= 0; i-- {
		if ParseFactProposals(comments[i].Text).Found {
			return ParseFoundNotFixed(comments[i].Text)
		}
	}
	for i := len(comments) - 1; i >= 0; i-- {
		if found := ParseFoundNotFixed(comments[i].Text); found.Found {
			return found
		}
	}
	return FoundNotFixed{}
}

// foundBug is one bug to file for lines of the section: its title, and the
// lines it quotes (one, or the ones past the cap).
type foundBug struct {
	title string
	lines []string
}

// foundBugs is the bugs a section files: one for each of the first
// foundAtCloseCap lines, and one that lists the others. The split is made on
// the whole section, not on what is not yet filed, so a close-out run again
// makes the same titles. A line whose title an earlier line has files nothing.
func foundBugs(id string, lines []string) []foundBug {
	var bugs []foundBug
	var rest []string
	titles := map[string]bool{}
	for _, line := range lines {
		title := "[bug] " + firstCharacters(line, foundTitleFirst)
		if titles[title] {
			continue
		}
		titles[title] = true
		if len(bugs) < foundAtCloseCap {
			bugs = append(bugs, foundBug{title: title, lines: []string{line}})
		} else {
			rest = append(rest, line)
		}
	}
	if len(rest) > 0 {
		bugs = append(bugs, foundBug{
			title: fmt.Sprintf("[bug] %s: %d more findings not fixed, past the first %d", id, len(rest), foundAtCloseCap),
			lines: rest,
		})
	}
	return bugs
}

func foundDescription(c *closeOut, bug foundBug, host string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) landed, and its closing comment lists %s under 'Found, not fixed:':\n\n", c.id, c.detail.Story.Title, thisOrThese(len(bug.lines)))
	for _, line := range bug.lines {
		fmt.Fprintf(&b, "> %s\n", line)
	}
	fmt.Fprintf(&b, "\nSeen by: %s\n\nThe Builder saw it and did not fix it, and nobody has checked it since. Read it, word what is to be done and its acceptance, and release this bug, or close it if it is not one.\n\nSource: mw next on %s (mw-t5shinh.24).\n", c.id, host)
	return b.String()
}

func thisOrThese(n int) string {
	if n == 1 {
		return "this finding"
	}
	return "these findings"
}

// filedFromFoundNotFixed is what a landing does about the 'Found, not fixed:'
// section of the story's closing comment: it files each line as a held bug
// under the story's epic, and gives the block the Landed mail carries, 'Filed:
// <id>, held.' for each. The landing is never undone for it: bugs that cannot
// be filed are a note and a mail to the Mayor. A bug of the same title, open or
// closed, is the one already filed, so a close-out run again files nothing
// (mw-t5shinh.24).
func (n Next) filedFromFoundNotFixed(ctx context.Context, c *closeOut, report *NextReport, again bool, comments []Comment) string {
	section := closingFoundNotFixed(comments)
	if len(section.Lines) == 0 {
		return ""
	}
	bugs := foundBugs(c.id, section.Lines)
	block := []string{"Found, not fixed, said in the closing comment:"}

	lost := func(err error, unfiled []foundBug) {
		var lines []string
		for _, bug := range unfiled {
			lines = append(lines, bug.lines...)
		}
		what := fmt.Sprintf("the bugs for %d findings not fixed in %s could not be filed", len(lines), c.id)
		report.Notes = append(report.Notes, fmt.Sprintf("%s: %v", what, err))
		n.mailLostWrite(ctx, c, report, what, fmt.Sprintf("%s landed with these lines under 'Found, not fixed:' and nothing asks for them to be looked at; "+
			"file each as a held [bug] labelled %s and %s, saying 'Seen by: %s': %v\n- %s", c.id, LabelBug, LabelFoundAtClose, c.id, err, strings.Join(lines, "\n- ")))
		block = append(block, "The bugs for them could not be filed (see the lost write mail).")
	}

	filed := map[string]string{}
	lister, ok := n.Tracker.(TesterStories)
	if !ok && again {
		return ""
	}
	if ok {
		stories, err := readRetrying(ctx, n, report, "the stories", func() ([]StoryDetail, error) { return lister.StoriesSince(ctx, time.Time{}) })
		if err != nil {
			lost(err, bugs)
			return strings.Join(block, "\n")
		}
		for _, d := range stories {
			filed[d.Story.Title] = d.Story.ID
		}
	}

	for i, bug := range bugs {
		if id, there := filed[bug.title]; there {
			report.Notes = append(report.Notes, fmt.Sprintf("%s already files the finding of %s: %s", id, c.id, bug.title))
			block = append(block, fmt.Sprintf("Already filed: %s. %s", id, strings.Join(bug.lines, " ")))
			continue
		}
		story := NewStory{
			EpicID:      c.detail.EpicID,
			Standalone:  c.detail.EpicID == "",
			Title:       bug.title,
			Description: foundDescription(c, bug, n.Host),
			Priority:    2,
			Overrides:   c.path,
			Labels:      []string{LabelBug, LabelFoundAtClose},
		}
		var id string
		err := writeRetrying(ctx, n, report, "the bug for a finding not fixed", func() error {
			var err error
			id, err = n.Tracker.CreateStory(ctx, story)
			return err
		})
		if err != nil {
			lost(err, bugs[i:])
			break
		}
		report.Notes = append(report.Notes, fmt.Sprintf("filed %s, held: %s found it and did not fix it", id, c.id))
		block = append(block, fmt.Sprintf("Filed: %s, held. %s", id, strings.Join(bug.lines, " ")))
	}
	return strings.Join(block, "\n")
}
