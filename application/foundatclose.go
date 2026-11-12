package application

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
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
var foundSectionEnders = []string{RigMemoryMarker, CommonErrorsMarker, "What's new:", "HOW TO CHECK IT", "Internal:", DocsMarker}

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

// foundPathPattern finds a repo path in a finding: segments joined by '/', a
// last one with an extension, then an optional ':line' or ':line-line'.
var foundPathPattern = regexp.MustCompile(`(?:[A-Za-z0-9_.\-]+/)*[A-Za-z0-9_\-]+\.[a-z][a-z0-9]{0,5}\b(?::\d+(?:-\d+)?)?`)

// foundBareExtensions are the extensions a name with no directory must have to
// be read as a file: 'e.g.' and 'application.Next' name none.
var foundBareExtensions = map[string]bool{
	"go": true, "md": true, "feature": true, "json": true, "yml": true, "yaml": true, "sh": true, "toml": true,
	"sql": true, "txt": true, "mod": true, "sum": true, "js": true, "ts": true, "tsx": true, "css": true,
	"html": true, "py": true, "service": true, "timer": true,
}

// foundPaths are the files text names, each once, in order: path:line is read as
// path, and a leading './' is dropped.
func foundPaths(text string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, match := range foundPathPattern.FindAllString(text, -1) {
		path, _, _ := strings.Cut(match, ":")
		path = strings.TrimPrefix(path, "./")
		dot := strings.LastIndex(path, ".")
		if dot < 0 || (!strings.Contains(path, "/") && !foundBareExtensions[path[dot+1:]]) || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

// foundFiles is the files key of a bug filed for lines: the files they name,
// else finder, the files key of the story that found it.
func foundFiles(lines []string, finder string) string {
	var paths []string
	seen := map[string]bool{}
	for _, line := range lines {
		for _, path := range foundPaths(line) {
			if !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}
	if len(paths) == 0 {
		return finder
	}
	return strings.Join(paths, ",")
}

// foundRef is a file a finding names, with the lines it names in it; from and to
// are 0 when it names no line.
type foundRef struct {
	path     string
	from, to int
}

var foundLinePattern = regexp.MustCompile(`:(\d+)(?:-(\d+))?$`)

// foundRefs are the files text names with their lines, each ref once, in order.
func foundRefs(text string) []foundRef {
	var refs []foundRef
	seen := map[foundRef]bool{}
	for _, match := range foundPathPattern.FindAllString(text, -1) {
		ref := foundRef{}
		if m := foundLinePattern.FindStringSubmatch(match); m != nil {
			ref.from, _ = strconv.Atoi(m[1])
			ref.to = ref.from
			if m[2] != "" {
				ref.to, _ = strconv.Atoi(m[2])
			}
		}
		path, _, _ := strings.Cut(match, ":")
		path = strings.TrimPrefix(path, "./")
		dot := strings.LastIndex(path, ".")
		if dot < 0 || (!strings.Contains(path, "/") && !foundBareExtensions[path[dot+1:]]) {
			continue
		}
		ref.path = path
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	return refs
}

var (
	foundBacktickPattern = regexp.MustCompile("`([^`]+)`")
	foundTokenPattern    = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*(?:\(\))?`)
	foundWordPattern     = regexp.MustCompile(`[a-z]+`)
)

// foundSymbols are the code names text mentions: what sits between backticks,
// and any name with an inner capital, an underscore, a dot between names or a
// '()' after it. A plain capitalised word (Job) is a symbol only in backticks.
// A file the text names is not one.
func foundSymbols(text string) map[string]bool {
	files := map[string]bool{}
	for _, path := range foundPaths(text) {
		files[path] = true
		files[path[strings.LastIndex(path, "/")+1:]] = true
	}
	symbols := map[string]bool{}
	add := func(token string) {
		token = strings.TrimSuffix(token, "()")
		if token != "" && !files[token] && !foundFileLike(token) {
			symbols[token] = true
		}
	}
	for _, m := range foundBacktickPattern.FindAllStringSubmatch(text, -1) {
		if tokens := foundTokenPattern.FindAllString(m[1], -1); len(tokens) == 1 && !strings.ContainsAny(m[1], " \t") {
			add(tokens[0])
		}
	}
	stripped := foundBacktickPattern.ReplaceAllString(text, " ")
	for _, path := range foundPaths(stripped) {
		stripped = strings.ReplaceAll(stripped, path, " ")
	}
	for _, token := range foundTokenPattern.FindAllString(stripped, -1) {
		innerCapital := false
		for i := 1; i < len(token); i++ {
			if token[i] >= 'A' && token[i] <= 'Z' && token[i-1] >= 'a' && token[i-1] <= 'z' {
				innerCapital = true
			}
		}
		if innerCapital || strings.ContainsAny(token, "_.") || strings.HasSuffix(token, "()") {
			add(token)
		}
	}
	return symbols
}

// foundFileLike says whether token is a file name: a last part that is a known
// extension.
func foundFileLike(token string) bool {
	dot := strings.LastIndex(token, ".")
	return dot >= 0 && foundBareExtensions[token[dot+1:]]
}

var foundStopWords = map[string]bool{
	"that": true, "this": true, "with": true, "from": true, "into": true, "when": true, "then": true, "than": true,
	"which": true, "while": true, "where": true, "there": true, "their": true, "these": true, "those": true,
	"have": true, "does": true, "doesn": true, "been": true, "being": true, "were": true, "will": true, "would": true,
	"should": true, "could": true, "also": true, "only": true, "such": true, "same": true, "each": true, "because": true,
	"about": true, "after": true, "before": true, "says": true, "said": true, "file": true,
	"line": true, "lines": true, "bug": true, "found": true, "fixed": true, "not": true,
}

// foundFaultWords are the plain words text uses to say what is wrong, with the
// files and the symbols left out.
func foundFaultWords(text string) map[string]bool {
	text = foundBacktickPattern.ReplaceAllString(text, " ")
	for _, path := range foundPaths(text) {
		text = strings.ReplaceAll(text, path, " ")
	}
	words := map[string]bool{}
	for _, token := range foundTokenPattern.FindAllString(text, -1) {
		if strings.ContainsAny(token, "_.") || strings.HasSuffix(token, "()") {
			continue
		}
		for _, word := range foundWordPattern.FindAllString(strings.ToLower(token), -1) {
			if len(word) >= 4 && !foundStopWords[word] {
				words[word] = true
			}
		}
	}
	return words
}

// foundSameWords says whether a and b say the same thing: three or more words
// in common, and at least half of the shorter's.
func foundSameWords(a, b map[string]bool) bool {
	shared := 0
	for word := range a {
		if b[word] {
			shared++
		}
	}
	return shared >= 3 && shared*2 >= min(len(a), len(b))
}

// foundOverlap says whether two refs name a line in common of one file.
func foundOverlap(a, b foundRef) bool {
	return a.path == b.path && a.from > 0 && b.from > 0 && a.from <= b.to && b.from <= a.to
}

// foundBeadText is the words of an open bead that say what is wrong: its title
// and description, where the lines mw next wrote to say who found it, and the
// request to word it, are not read.
func foundBeadText(d StoryDetail) string {
	text := []string{strings.TrimPrefix(d.Story.Title, "[bug] ")}
	for _, line := range strings.Split(d.Description, "\n") {
		if !strings.Contains(line, " landed, and its closing comment lists ") && !strings.HasPrefix(line, "Seen by:") &&
			!strings.HasPrefix(line, "Source:") && !strings.HasPrefix(line, "The Builder saw it and did not fix it") {
			text = append(text, strings.TrimPrefix(line, "> "))
		}
	}
	return strings.Join(text, "\n")
}

// foundMatch is the open found-at-close beads a finding meets: same is the one
// that names the same fault, the same file:line or the same symbol and the same
// fault words; near is one that shares only a file or a symbol with it, which is
// no reason to file nothing. A bead another close-out of the same story filed is
// neither: a story's own findings may share a file (mw-gq6.1068). A shared file
// or function alone never makes a match (mw-gq6.1103).
func foundMatch(bug foundBug, beads []StoryDetail, finder string) (same, near string) {
	text := strings.Join(bug.lines, "\n")
	refs, symbols, words := foundRefs(text), foundSymbols(text), foundFaultWords(text)
	if len(refs) == 0 && len(symbols) == 0 {
		return "", ""
	}
	for _, d := range beads {
		if d.Closed() || !slices.Contains(d.Labels, LabelFoundAtClose) || strings.Contains(d.Description, "Seen by: "+finder+"\n") {
			continue
		}
		other := foundBeadText(d)
		otherRefs, otherSymbols := foundRefs(other), foundSymbols(other)
		sameSymbol, sharesFile := false, false
		for _, ref := range refs {
			for _, o := range otherRefs {
				if foundOverlap(ref, o) {
					return d.Story.ID, near
				}
				sharesFile = sharesFile || ref.path == o.path
			}
		}
		for symbol := range symbols {
			sameSymbol = sameSymbol || otherSymbols[symbol]
		}
		if sameSymbol && foundSameWords(words, foundFaultWords(other)) {
			return d.Story.ID, near
		}
		if near == "" && (sameSymbol || sharesFile) {
			near = d.Story.ID
		}
	}
	return "", near
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
// (mw-t5shinh.24); so is a bead not closed that another story's finding filed
// for the same fault, worded as it likes (mw-gq6.1068); a bead that shares only a
// file or symbol is not, and the bug filed names it as one it may overlap
// (mw-gq6.1103). A bug's files key is the
// files its lines name, else the finder's.
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
	var beads []StoryDetail
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
		beads = stories
	}

	for i, bug := range bugs {
		id, there := filed[bug.title]
		near := ""
		if !there {
			id, near = foundMatch(bug, beads, c.id)
			there = id != ""
		}
		if there {
			report.Notes = append(report.Notes, fmt.Sprintf("%s already files the finding of %s: %s", id, c.id, bug.title))
			block = append(block, fmt.Sprintf("Already filed: %s. %s", id, strings.Join(bug.lines, " ")))
			continue
		}
		overrides := c.path
		overrides.Files = foundFiles(bug.lines, c.path.Files)
		story := NewStory{
			EpicID:      c.detail.EpicID,
			Standalone:  c.detail.EpicID == "",
			Title:       bug.title,
			Description: foundDescription(c, bug, n.Host),
			Priority:    2,
			Overrides:   overrides,
			Labels:      []string{LabelBug, LabelFoundAtClose},
		}
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
		if near != "" {
			report.Notes = append(report.Notes, fmt.Sprintf("%s may overlap %s, which names the same file or symbol but not the same fault", id, near))
			block = append(block, fmt.Sprintf("Filed: %s, held (may overlap %s). %s", id, near, strings.Join(bug.lines, " ")))
		} else {
			block = append(block, fmt.Sprintf("Filed: %s, held. %s", id, strings.Join(bug.lines, " ")))
		}
	}
	return strings.Join(block, "\n")
}
