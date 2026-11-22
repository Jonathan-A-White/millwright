package application

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
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
	foundAtCloseCap = 2
	// foundBugPriority is the priority a bug filed from a finding has: P3, so the
	// Mayor raises the ones he wants worked first (mw-6ww.119.10).
	foundBugPriority = 3
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
// (mw-gq6.726). One pair of backticks wrapping the whole line is stripped
// first, as unfenced does for the other sections, so a line written as code is
// read as the line inside it (mw-gq6.1400); a backtick pair around the heading
// alone, with text after it, is still a mention.
func headingRest(line, heading string) (rest string, ok bool) {
	text := strings.TrimLeft(unfenced(domain.TrimListMarker(strings.TrimSpace(line))), "*_# \t")
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
	// said is true once the section has said something, a finding or the word
	// nothing; the next blank line then ends it (mw-gq6.1186).
	said := false
	take := func(raw string) {
		text := strings.Join(strings.Fields(domain.TrimListMarker(raw)), " ")
		if text == "" || isBuilderNote(text) {
			return
		}
		said = true
		if strings.EqualFold(strings.Trim(text, " .*_`"), "nothing") || seen[text] {
			return
		}
		seen[text] = true
		found.Lines = append(found.Lines, text)
	}
	take(first)
	for _, raw := range lines[start+1:] {
		if strings.TrimSpace(raw) == "" {
			if said {
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

// isBuilderNote says whether text, a line of the section already trimmed, is a
// note of the Builder's and no finding: it starts with '(' and the parenthesis
// that closes that one is the line's last character, as in '(I did not run the
// tests named above.)' (mw-gq6.1297). A line that holds a parenthesis, or starts
// with one and goes on after its closing one, is a finding. A note is no
// finding and not something said: it ends no section and starts none.
func isBuilderNote(text string) bool {
	if !strings.HasPrefix(text, "(") {
		return false
	}
	depth := 0
	for i, r := range text {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i == len(text)-1
			}
		}
	}
	return false
}

func endsFoundSection(line string) bool {
	for _, heading := range foundSectionEnders {
		if _, ok := headingRest(line, heading); ok {
			return true
		}
	}
	return false
}

// closingFoundNotFixed reads the story's closing comment: the newest that has
// both a 'For the rig memory:' section and the 'Found, not fixed:' heading, else
// the newest that has the heading. A newer comment with a rig-memory line and no
// Found section (the Mayor's re-scope) answers the acceptance and does not
// withdraw the Builder's finds (mw-gq6.1547).
func closingFoundNotFixed(comments []Comment) FoundNotFixed {
	for i := len(comments) - 1; i >= 0; i-- {
		if ParseFactProposals(comments[i].Text).Found {
			if found := ParseFoundNotFixed(comments[i].Text); found.Found {
				return found
			}
		}
	}
	for i := len(comments) - 1; i >= 0; i-- {
		if found := ParseFoundNotFixed(comments[i].Text); found.Found {
			return found
		}
	}
	return FoundNotFixed{}
}

// NoACReason is the reason a bug filed from a finding with no AC is held for
// (mw-gq6.1124): it is written as a HELD comment on the bug.
const NoACReason = "no AC from the Builder"

// FoundItem is one finding line read for what the formula asks of it: what is
// wrong, then an AC checkable by running something, a Sweep line, a size guess
// and whether the code says it is by design, each marked inside the line as
// 'AC: ...', 'Sweep: ...', 'Size: S' and 'By design? yes'.
type FoundItem struct {
	// What is the finding itself, the text before the first marker (the whole line
	// when it has none).
	What string
	// AC is the one line checkable by running something; empty when the line has none.
	AC string
	// Sweep is where else the same fault may be; empty when the line has none.
	Sweep string
	// Size is the Builder's guess, empty when it gave none or not one of domain.Sizes.
	Size domain.Size
	// ByDesign is 'yes' or 'no' as the line says, else empty.
	ByDesign string
}

var foundMarkerPattern = regexp.MustCompile(`(?i)(?:^|[\s.;])(ac:|sweep:|size:|by design\?:?)`)

// foundTrimEnd drops spaces and one closing '.' or ';' from a part of a line; a
// '...' (go vet ./...) is kept whole.
func foundTrimEnd(text string) string {
	text = strings.TrimRight(text, " \t;")
	if strings.HasSuffix(text, ".") && !strings.HasSuffix(text, "..") {
		text = strings.TrimRight(text[:len(text)-1], " \t;")
	}
	return text
}

// ParseFoundItem reads the markers of one finding line. A line with none is
// only What.
func ParseFoundItem(line string) FoundItem {
	line = strings.TrimSpace(line)
	marks := foundMarkerPattern.FindAllStringSubmatchIndex(line, -1)
	if len(marks) == 0 || marks[0][2] == 0 {
		return FoundItem{What: line}
	}
	item := FoundItem{What: foundTrimEnd(line[:marks[0][2]])}
	for i, m := range marks {
		end := len(line)
		if i+1 < len(marks) {
			end = marks[i+1][2]
		}
		key := strings.ToLower(strings.TrimRight(line[m[2]:m[3]], ":"))
		value := foundTrimEnd(strings.TrimSpace(line[m[3]:end]))
		switch {
		case key == "ac" && item.AC == "":
			item.AC = value
		case key == "sweep" && item.Sweep == "":
			item.Sweep = value
		case key == "size":
			if size := domain.Size(strings.ToUpper(strings.Fields(value + " -")[0])); domain.KnownSize(size) {
				item.Size = size
			}
		case strings.HasPrefix(key, "by design"):
			if word := strings.ToLower(strings.Trim(strings.Fields(value + " -")[0], ".,;")); word == "yes" || word == "no" {
				item.ByDesign = word
			}
		}
	}
	return item
}

var foundNumberedPattern = regexp.MustCompile(`^\d+[.)]\s`)

// Acceptance is the acceptance a bug filed from the item has: the AC, numbered,
// and its Sweep line; empty when the item has no AC.
func (i FoundItem) Acceptance() string {
	if i.AC == "" {
		return ""
	}
	ac := i.AC
	if !foundNumberedPattern.MatchString(ac) {
		ac = "1. " + ac
	}
	if i.Sweep != "" {
		ac += "\nSweep: " + i.Sweep
	}
	return ac
}

// foundBug is one bug to file for lines of the section: its title, and the
// lines it quotes (one, or the ones past the cap). A bug of one line has that
// line read as item; the bug that lists the lines past the cap has none, and
// quotes each line whole; its acceptance is read from those lines all the same
// (acceptance, mw-gq6.1627).
type foundBug struct {
	title string
	lines []string
	item  *FoundItem
}

// said is the lines of the bug as the finding words them: the finding alone when
// it was read as an item.
func (b foundBug) said() []string {
	if b.item != nil {
		return []string{b.item.What}
	}
	return b.lines
}

// acceptance is the acceptance the bug is filed with: the item's AC and Sweep,
// or, for the bug that lists the lines past the cap, the AC and Sweep of each
// line that gave an AC, numbered and labelled with its finding, the Sweep on a
// line of its own. Empty when no line gave an AC (mw-gq6.1627).
func (b foundBug) acceptance() string {
	if b.item != nil {
		return b.item.Acceptance()
	}
	var parts []string
	for _, line := range b.lines {
		item := ParseFoundItem(line)
		if item.AC == "" {
			continue
		}
		ac := foundNumberedPattern.ReplaceAllString(item.AC, "")
		part := fmt.Sprintf("%d. %s: %s", len(parts)+1, item.What, ac)
		if item.Sweep != "" {
			part += "\nSweep: " + item.Sweep
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "\n")
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
		item := ParseFoundItem(line)
		title := "[bug] " + firstCharacters(item.What, foundTitleFirst)
		if titles[title] {
			continue
		}
		titles[title] = true
		if len(bugs) < foundAtCloseCap {
			bugs = append(bugs, foundBug{title: title, lines: []string{line}, item: &item})
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
// last one with an extension (its name may hold dots: attachment-viewer.spec.ts),
// then an optional ':line' or ':line-line'.
var foundPathPattern = regexp.MustCompile(`(?:[A-Za-z0-9_.\-]+/)*[A-Za-z0-9_\-]+(?:\.[A-Za-z0-9_\-]+)*\.[a-z][a-z0-9]{0,5}\b(?::\d+(?:-\d+)?)?`)

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
	return foundDropBare(paths)
}

// foundDropBare takes out of paths each bare name (no folder) that another path
// of the list, one with a folder, ends with: the second mention of a file
// (features/steps/a.tsx, then a.tsx:246) is that path, not a file at the root. A
// bare name no path ends with stays.
func foundDropBare(paths []string) []string {
	kept := make([]string, 0, len(paths))
	for _, path := range paths {
		bare := false
		if !strings.Contains(path, "/") {
			for _, other := range paths {
				if strings.HasSuffix(other, "/"+path) {
					bare = true
					break
				}
			}
		}
		if !bare {
			kept = append(kept, path)
		}
	}
	return kept
}

// foundLinesPaths are the files the lines name, each once, in order, with a bare
// name dropped when a path of the lines ends with it.
func foundLinesPaths(lines []string) []string {
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
	return foundDropBare(paths)
}

// foundFiles is the files key of a bug filed for lines: the files they name,
// else finder, the files key of the story that found it.
func foundFiles(lines []string, finder string) string {
	paths := foundLinesPaths(lines)
	if len(paths) == 0 {
		return finder
	}
	return strings.Join(paths, ",")
}

// foundPlacing is where a finding's bug is pathed when it is about another rig.
type foundPlacing struct {
	// elsewhere says the finding is not about the finder's own rig; path is then
	// the bug's whole path (the finder's keys that hold for any rig, the rig's
	// own defaults and its files), and its Rig is empty when the rig cannot be
	// told.
	elsewhere bool
	path      domain.Path
}

// foundPlace reads which rig the finding lines are about. Its files name
// another rig when they lead with that rig's directory (Cairn/vite.config.ts),
// and the prefix is then dropped from the file; failing that, its words name
// one other rig and not the finder's own (a name after an article, as in "the
// legend", is a common word and does not count). Files of two rigs, of another rig
// and of the finder's own, or words naming two other rigs, say no rig. The
// rig's defaults are those of its newest story among beads: branch, formula and
// the harness, model and effort the rig's stories use; the finder's host, size,
// bump, callers_ok, host_why and max_turns are never copied. With no story to
// read them from, the branch and formula are left unset, for mw path show to
// warn of, and the finder's harness, model and effort stay.
func (n Next) foundPlace(lines []string, finder domain.Path, beads []StoryDetail) foundPlacing {
	byName := map[string]string{}
	add := func(name string) {
		if name = strings.TrimSpace(name); name != "" && name != finder.Rig {
			byName[strings.ToLower(name)] = name
		}
	}
	for name, dir := range n.Rigs {
		add(name)
		if base := filepath.Base(strings.TrimRight(dir, "/")); base != "." && base != "/" {
			if name != finder.Rig {
				byName[strings.ToLower(base)] = name
			}
		}
	}
	newest := map[string]domain.Path{}
	newestAt := map[string]time.Time{}
	for _, d := range beads {
		merged := d.Merged()
		if merged.Rig == "" || merged.Branch == "" || merged.Formula == "" {
			continue
		}
		if at, seen := newestAt[merged.Rig]; !seen || !d.Created.Before(at) {
			newest[merged.Rig], newestAt[merged.Rig] = merged, d.Created
		}
		add(merged.Rig)
	}

	paths := foundLinesPaths(lines)
	rigs := map[string]bool{}
	own := 0
	stripped := make([]string, len(paths))
	for i, path := range paths {
		stripped[i] = path
		if lead, rest, found := strings.Cut(path, "/"); found {
			if rig, other := byName[strings.ToLower(lead)]; other {
				rigs[rig], stripped[i] = true, rest
				continue
			}
		}
		own++
	}
	switch {
	case len(rigs) == 1 && own == 0:
		for rig := range rigs {
			return foundRigPlace(rig, strings.Join(stripped, ","), finder, newest)
		}
	case len(rigs) > 0:
		return foundPlacing{elsewhere: true, path: foundNoRigPlace(strings.Join(paths, ","), finder)}
	}

	// No file leads with another rig's directory: the words may name one.
	text := strings.Join(lines, " ")
	named := map[string]bool{}
	for lower, rig := range byName {
		if foundNamesRig(text, lower) {
			named[rig] = true
		}
	}
	if finder.Rig != "" && foundNamesRig(text, strings.ToLower(finder.Rig)) {
		return foundPlacing{}
	}
	switch len(named) {
	case 0:
		return foundPlacing{}
	case 1:
		for rig := range named {
			return foundRigPlace(rig, foundFiles(lines, ""), finder, newest)
		}
	}
	return foundPlacing{elsewhere: true, path: foundNoRigPlace(foundFiles(lines, ""), finder)}
}

var (
	foundArticleBefore = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_\-])(?:the|a|an|this|that|these|those|its|our|their)\s+$`)
	foundRigNounAfter  = regexp.MustCompile(`(?i)^\s+(?:rig|repo|repository)\b`)
)

// foundNamesRig says text names the rig, in any case, as a word of its own. A
// name right after an article or a demonstrative (the legend, a legend, this
// legend) is the common word and not the rig, unless the words go on to say rig
// or repo (the legend rig).
func foundNamesRig(text, lowerName string) bool {
	pattern := regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_\-])(` + regexp.QuoteMeta(lowerName) + `)(?:$|[^A-Za-z0-9_\-])`)
	for _, m := range pattern.FindAllStringSubmatchIndex(text, -1) {
		if foundArticleBefore.MatchString(text[:m[2]]) && !foundRigNounAfter.MatchString(text[m[3]:]) {
			continue
		}
		return true
	}
	return false
}

// foundNoRigPlace is the path of a bug whose rig cannot be told: no rig, branch
// or formula, which are a rig's, and the finder's harness, which is about the
// work and not the repo; its model and effort are the default route's.
func foundNoRigPlace(files string, finder domain.Path) domain.Path {
	return domain.Path{Files: files, Host: domain.HostAuto, Harness: finder.Harness, Model: foundBugModel, Effort: foundBugEffort}
}

// foundRigPlace is the path of a bug filed in rig with files, from the rig's
// newest story's defaults when one is known.
func foundRigPlace(rig, files string, finder domain.Path, newest map[string]domain.Path) foundPlacing {
	path := domain.Path{Rig: rig, Files: files, Host: domain.HostAuto, Harness: finder.Harness, Model: foundBugModel, Effort: foundBugEffort}
	if known, ok := newest[rig]; ok {
		path.Branch, path.Harness, path.Model, path.Effort, path.Formula = known.Branch, known.Harness, known.Model, known.Effort, known.Formula
	}
	return foundPlacing{elsewhere: true, path: path}
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
func foundMatch(bug foundBug, beads []StoryDetail, finder string) (same, near, why string) {
	text := strings.Join(bug.said(), "\n")
	refs, symbols, words := foundRefs(text), foundSymbols(text), foundFaultWords(text)
	if len(refs) == 0 && len(symbols) == 0 {
		return "", "", ""
	}
	nearWhy := ""
	for _, d := range beads {
		if d.Closed() || !slices.Contains(d.Labels, LabelFoundAtClose) || strings.Contains(d.Description, "Seen by: "+finder+"\n") {
			continue
		}
		other := foundBeadText(d)
		otherRefs, otherSymbols := foundRefs(other), foundSymbols(other)
		sameSymbol, sharesFile := false, false
		sharedSymbol, sharedFile := "", ""
		for _, ref := range refs {
			for _, o := range otherRefs {
				if foundOverlap(ref, o) {
					return d.Story.ID, near, "the same line of " + foundRefText(ref) + " as " + foundRefText(o)
				}
				if ref.path == o.path && !sharesFile {
					sharesFile, sharedFile = true, ref.path
				}
			}
		}
		for _, symbol := range slices.Sorted(maps.Keys(symbols)) {
			if otherSymbols[symbol] && !sameSymbol {
				sameSymbol, sharedSymbol = true, symbol
			}
		}
		if sameSymbol && foundSameWords(words, foundFaultWords(other)) {
			return d.Story.ID, near, fmt.Sprintf("the symbol %s and the fault words %s", sharedSymbol, strings.Join(foundSharedWords(words, foundFaultWords(other)), ", "))
		}
		if near == "" && (sameSymbol || sharesFile) {
			near = d.Story.ID
			if sameSymbol {
				nearWhy = "near match: shares only the symbol " + sharedSymbol
			} else {
				nearWhy = "near match: shares only the file " + sharedFile
			}
		}
	}
	return "", near, nearWhy
}

// foundClosedSince is the bead closed after forked, when the finder's branch was
// made, that holds the fault of the finding: the finder's branch saw the fault
// unfixed, and a bead closed since (landed, or closed as superseded) has fixed it
// or carries it on, so filing it again files a second bug for a fault no longer
// there (mw-gq6.1638). It names the same fault as foundMatch does, the same line
// of a file or the same symbol and the same fault words, and needs no found-at-close
// label, since it is the story that fixed the fault that holds it. A bead closed
// before the branch forked, or when it is not known, is not it: the finder saw the
// fault with that bead's work in its branch. A bead closed as superseded by another
// bead is the other bead's to carry: id is that bead's then. why says what was shared.
func foundClosedSince(bug foundBug, beads []StoryDetail, finder string, forked time.Time) (id, why string) {
	if forked.IsZero() {
		return "", ""
	}
	text := strings.Join(bug.said(), "\n")
	refs, symbols, words := foundRefs(text), foundSymbols(text), foundFaultWords(text)
	if len(refs) == 0 && len(symbols) == 0 {
		return "", ""
	}
	for _, d := range beads {
		if !d.Closed() || d.IsParent() || d.Story.ID == finder || d.ClosedAt.IsZero() || !d.ClosedAt.After(forked) || strings.Contains(d.Description, "Seen by: "+finder+"\n") {
			continue
		}
		other := foundBeadText(d)
		otherRefs, otherSymbols, otherWords := foundRefs(other), foundSymbols(other), foundFaultWords(other)
		holds := ""
		for _, ref := range refs {
			for _, o := range otherRefs {
				if foundOverlap(ref, o) && holds == "" {
					holds = "the same line of " + foundRefText(ref) + " as " + foundRefText(o)
				}
			}
		}
		for _, symbol := range slices.Sorted(maps.Keys(symbols)) {
			if holds == "" && otherSymbols[symbol] && foundSameWords(words, otherWords) {
				holds = fmt.Sprintf("the symbol %s and the fault words %s", symbol, strings.Join(foundSharedWords(words, otherWords), ", "))
			}
		}
		if holds == "" {
			continue
		}
		why = fmt.Sprintf("%s, closed %s after the branch forked at %s", holds, d.ClosedAt.UTC().Format("15:04Z"), forked.UTC().Format("15:04Z"))
		if strings.Contains(strings.ToLower(d.CloseReason), "supersed") {
			for _, carrier := range beads {
				if carrier.Story.ID != d.Story.ID && carrier.Story.ID != finder && foundNamesBead(d.CloseReason, carrier.Story.ID) {
					return carrier.Story.ID, why + "; " + d.Story.ID + " was closed as superseded by it"
				}
			}
		}
		return d.Story.ID, why
	}
	return "", ""
}

// foundNamesBead says whether text names the bead id as a whole id: mw-6ww.185.3
// is not named by a text that names mw-6ww.185.33.
func foundNamesBead(text, id string) bool {
	idChar := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
	}
	for start := 0; ; {
		at := strings.Index(text[start:], id)
		if at < 0 {
			return false
		}
		end := start + at + len(id)
		// A '.' that ends a sentence closes the id; one that goes on to a digit or a
		// letter is a longer id.
		if end == len(text) || (!idChar(text[end]) && (text[end] != '.' || end+1 == len(text) || !idChar(text[end+1]))) {
			return true
		}
		start = end
	}
}

// foundInFlight is the story of the finding's own rig that is being worked now
// (in progress, so claimed) and names the same file or symbol as the finding and
// says the same fault in its title or description: the bug filed for the finding
// may duplicate it, and is made to wait on it (mw-gq6.1440). A story of another
// rig, a closed or unclaimed one, the finder itself, and one that shares only a
// word or only a file are not it. why says what was shared.
func foundInFlight(bug foundBug, beads []StoryDetail, rig, finder string) (id, why string) {
	if rig == "" {
		return "", ""
	}
	text := strings.Join(bug.said(), "\n")
	refs, symbols, words := foundRefs(text), foundSymbols(text), foundFaultWords(text)
	if len(refs) == 0 && len(symbols) == 0 {
		return "", ""
	}
	for _, d := range beads {
		if d.Story.ID == finder || d.IsParent() || !strings.EqualFold(strings.TrimSpace(d.Status), StatusInProgress) || d.Merged().Rig != rig {
			continue
		}
		other := foundBeadText(d)
		otherWords := foundFaultWords(other)
		if !foundSameWords(words, otherWords) {
			continue
		}
		shared := ""
		otherRefs, otherSymbols := foundRefs(other), foundSymbols(other)
		for _, file := range strings.Split(d.Merged().Files, ",") {
			otherRefs = append(otherRefs, foundRef{path: strings.TrimSpace(file)})
		}
		for _, ref := range refs {
			for _, o := range otherRefs {
				if ref.path != "" && ref.path == o.path {
					shared = "the file " + ref.path
				}
			}
		}
		for _, symbol := range slices.Sorted(maps.Keys(symbols)) {
			if shared == "" && otherSymbols[symbol] {
				shared = "the symbol " + symbol
			}
		}
		if shared != "" {
			return d.Story.ID, shared + " and the fault words " + strings.Join(foundSharedWords(words, otherWords), ", ")
		}
	}
	return "", ""
}

// foundRefText is a ref as a person reads it: path:line, path:from-to or the
// bare path.
func foundRefText(r foundRef) string {
	switch {
	case r.from <= 0:
		return r.path
	case r.to > r.from:
		return fmt.Sprintf("%s:%d-%d", r.path, r.from, r.to)
	}
	return fmt.Sprintf("%s:%d", r.path, r.from)
}

// foundSharedWords are the fault words a and b have in common, in order.
func foundSharedWords(a, b map[string]bool) []string {
	var shared []string
	for word := range a {
		if b[word] {
			shared = append(shared, word)
		}
	}
	slices.Sort(shared)
	return shared
}

// keepJoin says in the story's close-out log, runs/<story>/next-<n>.log, what a
// close-out joined to an open bead and by which rule, so that a wrong join can be
// found afterwards: NextReport.Notes is shown once and kept nowhere (mw-gq6.1107).
// The line is added to what the log holds, which the session's own command line
// is also appending to (tee -a); a log that cannot be written is a note.
func (n Next) keepJoin(ctx context.Context, c *closeOut, report *NextReport, line string) {
	if n.Vault == nil {
		return
	}
	name := NextLogNameForAttempt(c.attempt())
	kept, err := n.Vault.ReadRunFile(ctx, c.id, name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		report.Notes = append(report.Notes, fmt.Sprintf("the join %q could not be kept in runs/%s/%s: %v", line, c.id, name, err))
		return
	}
	if kept != "" && !strings.HasSuffix(kept, "\n") {
		kept += "\n"
	}
	if _, err := n.Vault.PutRunFile(ctx, c.id, name, kept+line+"\n"); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the join %q could not be kept in runs/%s/%s: %v", line, c.id, name, err))
	}
}

// FindingLine is a file a finding names on main, with the line it names there
// (0 for none).
type FindingLine struct {
	Path string
	Line int
}

// FindingAnswer is what main showed of a finding: the commit it was read at,
// the run of the finding's command when one was asked for, and how many lines
// each named file has there (a file that is not on main has no entry).
type FindingAnswer struct {
	Sha    string
	Ran    bool
	Passed bool
	Tail   string
	Lines  map[string]int
}

// FindingRechecker reads a finding against the head of a rig's origin as it is
// merged now (Suite.RecheckFinding): it runs command there when it is not empty
// and counts the lines of each named file. An error is a read that could not be
// made, and the finding is filed as it would have been.
type FindingRechecker interface {
	RecheckFinding(ctx context.Context, rig, command string, files []FindingLine) (FindingAnswer, error)
}

var (
	foundPassWords    = regexp.MustCompile(`(?i)\s+(?:passes|pass|exits 0|exits zero|succeeds|is green|reports ok|says ok)\b`)
	foundSafeWord     = regexp.MustCompile(`^[A-Za-z0-9_./=,:^$-]+$`)
	foundNoTestsToRun = regexp.MustCompile(`no tests to run|no test files`)
)

// foundCheckCommand is the command a finding's AC names that is cheap and
// deterministic enough to run on main before the bug is filed, or empty: the
// words before 'passes' (or a backticked span of them), and only make lint, go
// test with -run and no './...', or a script under scripts/, or a grep read by
// its exit (foundGrepCommand), or a web rig's npm test or npx vitest run of up to
// three test files (the install is Suite.RecheckFinding's). Anything with a pipe, redirect or the like is not
// run, nor is a whole suite (make test, go test ./...): it takes minutes under
// the host's gate lock for one finding, so such a finding is filed as before.
func foundCheckCommand(ac string) string {
	ac = foundNumberedPattern.ReplaceAllString(strings.TrimSpace(ac), "")
	if strings.HasPrefix(ac, "grep ") || strings.HasPrefix(ac, "`grep ") {
		return foundGrepCommand(ac)
	}
	at := foundPassWords.FindStringIndex(ac)
	if at == nil {
		return ""
	}
	command := strings.TrimSpace(ac[:at[0]])
	if span := foundBacktickPattern.FindStringSubmatch(command); span != nil {
		command = strings.TrimSpace(span[1])
	}
	words := strings.Fields(command)
	for _, word := range words {
		if !foundSafeWord.MatchString(word) || strings.Contains(word, "..") && word != "./..." {
			return ""
		}
	}
	switch {
	case len(words) == 2 && words[0] == "make" && words[1] == "lint":
	case len(words) >= 3 && words[0] == "go" && words[1] == "test" && slices.Contains(words, "-run") && !slices.Contains(words, "./..."):
	case len(words) == 2 && words[0] == "npm" && words[1] == "test":
	case foundVitestFiles(words):
	case len(words) >= 1 && foundScriptWord(words[0]):
	case len(words) >= 2 && (words[0] == "sh" || words[0] == "bash") && foundScriptWord(words[1]):
	default:
		return ""
	}
	return strings.Join(words, " ")
}

var (
	foundGrepFlag     = regexp.MustCompile(`^-[nciqEFwxrRlvH]+$`)
	foundGrepVerb     = regexp.MustCompile(`^(?:prints|shows|finds|lists|names|matches|exits|succeeds|passes|is)$`)
	foundGrepMatches  = regexp.MustCompile(`(?i)^(?:(?:prints|shows|finds|lists)\s+(?:a|an|at least one|one or more)\s+(?:line|lines|match|matches|row|rows)\b|exits\s+(?:0|zero)\b|succeeds\b|passes\b)`)
	foundGrepNoMatch  = regexp.MustCompile(`(?i)^(?:(?:prints|finds|shows|lists|matches)\s+(?:nothing|no\s+(?:lines?|matches|match|output))\b|is empty\b|exits\s+1\b)`)
	foundGrepUnsafeDQ = "$`\\!\"'"
)

// foundGrepCommand is the command line of an AC that is a grep and says what its
// exit means, or empty: 'grep -n "text" docs/a.md prints a line' (a match is the
// pass: exit 0) or '... prints nothing' (no match is the pass, and a grep that
// could not read its file, exit 2, is not). The pattern is one quoted or plain
// word with none of the shell's own characters, written back in single quotes,
// the flags only those that read, and the files plain relative paths; a pipe, a
// redirect, a variable or a result a person must read ('prints only lines that
// say') is not run.
func foundGrepCommand(ac string) string {
	if strings.HasPrefix(ac, "`") {
		end := strings.Index(ac[1:], "`")
		if end < 0 {
			return ""
		}
		ac = ac[1:1+end] + " " + strings.TrimSpace(ac[end+2:])
	}
	var words []string
	rest := strings.TrimSpace(ac)
	for rest != "" {
		var word string
		quoted := false
		switch rest[0] {
		case '\'', '"':
			end := strings.IndexByte(rest[1:], rest[0])
			if end < 0 {
				return ""
			}
			word, quoted = rest[1:1+end], true
			if rest[0] == '"' && strings.ContainsAny(word, foundGrepUnsafeDQ) || strings.ContainsAny(word, "'\x00\n\r\t") {
				return ""
			}
			rest = strings.TrimSpace(rest[end+2:])
		default:
			word, _, _ = strings.Cut(rest, " ")
			if len(words) > 0 && foundGrepVerb.MatchString(word) {
				return foundGrepRead(words, rest)
			}
			if !foundSafeWord.MatchString(word) || strings.Contains(word, "$") {
				return ""
			}
			rest = strings.TrimSpace(rest[len(word):])
		}
		if quoted {
			word = "\x00" + word
		}
		words = append(words, word)
	}
	return ""
}

// foundGrepRead is foundGrepCommand once its words and the phrase after them are
// split: a quoted word begins with a NUL here, so that it is never read as a flag.
func foundGrepRead(words []string, phrase string) string {
	if words[0] != "grep" {
		return ""
	}
	var flags, files []string
	pattern, have := "", false
	for _, word := range words[1:] {
		quoted := strings.HasPrefix(word, "\x00")
		word = strings.TrimPrefix(word, "\x00")
		switch {
		case !have && !quoted && foundGrepFlag.MatchString(word):
			flags = append(flags, word)
		case !have:
			if strings.HasPrefix(word, "-") || word == "" {
				return ""
			}
			pattern, have = word, true
		default:
			if quoted || strings.HasPrefix(word, "/") || strings.HasPrefix(word, "-") || word == ".." || strings.Contains(word, "..") {
				return ""
			}
			files = append(files, word)
		}
	}
	if !have || len(files) == 0 {
		return ""
	}
	line := strings.Join(append(append([]string{"grep"}, flags...), "'"+pattern+"'"), " ") + " " + strings.Join(files, " ")
	switch {
	case foundGrepMatches.MatchString(phrase):
		return line
	case foundGrepNoMatch.MatchString(phrase):
		return line + "; test $? -eq 1"
	}
	return ""
}

// foundVitestMostFiles is the most test files one finding's vitest run may name:
// the run is made in a tree with a fresh install, under the host's gate lock.
const foundVitestMostFiles = 3

var foundTestFile = regexp.MustCompile(`\.(?:test|spec)\.[cm]?[jt]sx?$`)

// foundVitestFiles says words are 'npx vitest run' and one to three test files
// by plain relative paths, with no flag, name filter or watch mode (mw-gq6.1293).
func foundVitestFiles(words []string) bool {
	if len(words) < 4 || len(words) > 3+foundVitestMostFiles || words[0] != "npx" || words[1] != "vitest" || words[2] != "run" {
		return false
	}
	for _, file := range words[3:] {
		if strings.HasPrefix(file, "-") || strings.HasPrefix(file, "/") || strings.Contains(file, "..") || !foundTestFile.MatchString(file) {
			return false
		}
	}
	return true
}

func foundScriptWord(word string) bool {
	return strings.HasPrefix(strings.TrimPrefix(word, "./"), "scripts/")
}

// foundSpecAsk is a SpecRechecker for one finding, so that the read of main is
// made under the host's gate lock as a bug's own re-run is (rerunUnderGate).
type foundSpecAsk struct {
	checks  FindingRechecker
	command string
	files   []FindingLine
	answer  FindingAnswer
}

func (a *foundSpecAsk) Recheck(ctx context.Context, rig, _ string) (SpecRecheck, error) {
	var err error
	a.answer, err = a.checks.RecheckFinding(ctx, rig, a.command, a.files)
	return SpecRecheck{Sha: a.answer.Sha}, err
}

// foundFixedOnMain is the verdict on a finding read against main: fixed when
// the finding's command ran there and passed (a test that main does not have is
// no pass), else, when it named no command, when every line it names is past the
// end of a file main has. A file main does not have is not fixed: it may have
// moved. how says what was seen.
func foundFixedOnMain(command string, lines []FindingLine, a FindingAnswer) (fixed bool, how string) {
	if command != "" {
		if !a.Ran {
			return false, ""
		}
		ran := !foundNoTestsToRun.MatchString(a.Tail) || !strings.HasPrefix(command, "go test")
		return a.Passed && ran, fmt.Sprintf("%s passes at %s", command, short(a.Sha))
	}
	if len(lines) == 0 {
		return false, ""
	}
	for _, l := range lines {
		if n, there := a.Lines[l.Path]; !there || l.Line <= n {
			return false, ""
		}
	}
	return true, fmt.Sprintf("%s has fewer lines at %s than the finding names", lines[0].Path, short(a.Sha))
}

// findingOnMain reads one finding against the rig's origin as merged before its
// bug is filed (mw-gq6.1245): its AC command when that is cheap and
// deterministic, else the file and line it names. fixed says main passes, and
// the bug is not filed; a finding that cannot be checked, or whose read could
// not be made (a note says why), is filed as it was.
func (n Next) findingOnMain(ctx context.Context, c *closeOut, report *NextReport, bug foundBug) (fixed bool, how string) {
	if n.FindingChecks == nil || bug.item == nil || ctx.Err() != nil {
		return false, ""
	}
	ask := &foundSpecAsk{checks: n.FindingChecks, command: foundCheckCommand(bug.item.AC)}
	for _, ref := range foundRefs(bug.item.What) {
		if ref.from > 0 {
			ask.files = append(ask.files, FindingLine{Path: ref.path, Line: ref.from})
		}
	}
	if ask.command == "" && len(ask.files) == 0 {
		return false, ""
	}
	askCtx, cancel := context.WithTimeout(ctx, foundCheckBound)
	defer cancel()
	_, err := rerunUnderGate(askCtx, ask, n.GateLock, DispatchGateWait, n.Host, c.id, c.path.Rig, "", func(note string) {
		report.Notes = append(report.Notes, note)
	})
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the finding of %s was not read against main, so its bug is filed: %s: %s", c.id, bug.item.What, firstLine(err.Error())))
		return false, ""
	}
	return foundFixedOnMain(ask.command, ask.files, ask.answer)
}

// foundCheckBound is the longest a read of main for one finding may take.
const foundCheckBound = 5 * time.Minute

func foundDescription(c *closeOut, bug foundBug, host string, filed domain.Path) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) landed, and its closing comment lists %s under 'Found, not fixed:':\n\n", c.id, c.detail.Story.Title, thisOrThese(len(bug.said())))
	for _, line := range bug.said() {
		fmt.Fprintf(&b, "> %s\n", line)
	}
	if bug.item != nil && bug.item.ByDesign != "" {
		fmt.Fprintf(&b, "\nBy design: %s (the Builder's answer to whether the code's own comment says it is deliberate).\n", bug.item.ByDesign)
	}
	fmt.Fprintf(&b, "\nFinder %s ran on %s/%s; the bug runs on the rig default, %s/%s; the Mayor may raise it with a reason.\n", c.id, c.path.Model, c.path.Effort, filed.Model, filed.Effort)
	fmt.Fprintf(&b, "\nSeen by: %s\n\nThe Builder saw it and did not fix it, and nobody has checked it since. Read it, word what is to be done and its acceptance (the Builder's, if it gave one, is in the acceptance field), and release this bug, or close it if it is not one.\n\nSource: mw next on %s (mw-t5shinh.24).\n", c.id, host)
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
// (mw-gq6.1103). A bead closed since the finder's branch forked that holds the
// fault is the one already filed too (foundClosedSince, mw-gq6.1638). A bug's files key is the
// files its lines name, else the finder's.
func (n Next) filedFromFoundNotFixed(ctx context.Context, c *closeOut, report *NextReport, again bool, comments []Comment) string {
	section := closingFoundNotFixed(comments)
	if len(section.Lines) == 0 {
		return ""
	}
	// A finding the Builder says is by design files nothing: the mail names it,
	// and it does not use up a place of the cap.
	var filable, byDesign []string
	for _, line := range section.Lines {
		if ParseFoundItem(line).ByDesign == "yes" {
			byDesign = append(byDesign, line)
		} else {
			filable = append(filable, line)
		}
	}
	bugs := foundBugs(c.id, filable)
	block := []string{"Found, not fixed, said in the closing comment:"}
	for _, line := range byDesign {
		report.Notes = append(report.Notes, fmt.Sprintf("the finding of %s is by design, so no bug is filed: %s", c.id, line))
		block = append(block, "By design, not filed: "+line)
	}

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
		near, why := "", "the same title"
		if !there {
			id, near, why = foundMatch(bug, beads, c.id)
			there = id != ""
			if !there {
				// A bead closed since the finder's branch forked has fixed the fault the
				// branch saw unfixed (mw-gq6.1638).
				closedID, closedWhy := foundClosedSince(bug, beads, c.id, c.detail.ClaimStarted())
				if closedID != "" {
					id = closedID
					n.keepJoin(ctx, c, report, fmt.Sprintf("joined the finding %q of %s to %s, closed since its branch forked, by %s", strings.Join(bug.said(), " "), c.id, id, closedWhy))
					report.Notes = append(report.Notes, fmt.Sprintf("already filed as %s, closed since the branch of %s forked: %s", id, c.id, bug.title))
					block = append(block, fmt.Sprintf("Already filed: %s (closed since the branch forked). %s", id, strings.Join(bug.lines, " ")))
					continue
				}
			}
		}
		if there {
			n.keepJoin(ctx, c, report, fmt.Sprintf("joined the finding %q of %s to %s by %s", strings.Join(bug.said(), " "), c.id, id, why))
			report.Notes = append(report.Notes, fmt.Sprintf("%s already files the finding of %s: %s", id, c.id, bug.title))
			block = append(block, fmt.Sprintf("Already filed: %s. %s", id, strings.Join(bug.lines, " ")))
			continue
		}
		if fixed, how := n.findingOnMain(ctx, c, report, bug); fixed {
			said := strings.Join(bug.said(), " ")
			dropped := fmt.Sprintf("already fixed on main: %s (%s; %s's finding, so no bug is filed)", said, how, c.id)
			n.keepJoin(ctx, c, report, dropped)
			report.Notes = append(report.Notes, dropped)
			block = append(block, "already fixed on main: "+said)
			continue
		}
		// The finder's size, bump and callers_ok are about the finder's own files
		// and change, so none is copied, as mw path set --from copies none
		// (mw-gq6.672, mw-gq6.1195); the Builder's size for the finding is set below.
		overrides := FiledPath(c.path, "", TesterKindBug)
		overrides.Files = foundFiles(bug.said(), c.path.Files)
		// The finder's model and effort are its own too: the bug runs on the
		// default route, and the description says what the finder ran on
		// (mw-gq6.1426, mw-gq6.1640).
		// The finder's host is about the finder's work, so the bug goes to any host
		// (mw-gq6.1255).
		overrides.Host = domain.HostAuto
		// Unless its epic says why its host stays: the bug is then worked there
		// too, and the epic's host is its own by inheritance (mw-gq6.1288).
		if host, _ := EpicKeepsHost(c.detail.Defaults); host != "" {
			overrides.Host = ""
		}
		// A finding about another rig is that rig's bug, not the finder's: it is
		// pathed to that rig with that rig's own defaults, or to none when the rig
		// cannot be told, and filed under no epic of the finder's, whose defaults
		// would otherwise give it the finder's rig back (mw-gq6.1317).
		epicID, standalone := c.detail.EpicID, c.detail.EpicID == ""
		if place := n.foundPlace(bug.said(), c.path, beads); place.elsewhere {
			overrides = place.path
			epicID, standalone = "", true
		}
		acceptance := bug.acceptance()
		if bug.item != nil && bug.item.Size != "" {
			overrides.Size = bug.item.Size
		}
		story := NewStory{
			EpicID:      epicID,
			Standalone:  standalone,
			Title:       bug.title,
			Description: foundDescription(c, bug, n.Host, overrides),
			Acceptance:  acceptance,
			Priority:    foundBugPriority,
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
		if acceptance == "" {
			// A bug with no AC from the Builder is held for that reason, written where
			// mw stuck and the Mayor read a hold's reason (mw-gq6.1124).
			reason := HeldCommentPrefix + " " + NoACReason
			if err := writeRetrying(ctx, n, report, "the hold reason of a bug for a finding not fixed", func() error {
				return n.Tracker.CommentOnStory(ctx, id, reason)
			}); err != nil {
				report.Notes = append(report.Notes, fmt.Sprintf("%s was filed held, but its reason (%s) could not be written: %v", id, NoACReason, err))
			}
		}
		// A story of the same rig being worked now that names the same file and
		// fault may be landing this very fix: the bug waits on it, and is checked
		// after it lands (mw-gq6.1440).
		if inFlight, flightWhy := foundInFlight(bug, beads, overrides.Rig, c.id); inFlight != "" {
			n.keepJoin(ctx, c, report, fmt.Sprintf("filed %s for the finding %q of %s as a possible duplicate of %s, in progress, by %s", id, strings.Join(bug.said(), " "), c.id, inFlight, flightWhy))
			if err := writeRetrying(ctx, n, report, "the wait of a bug for a finding not fixed", func() error {
				return n.Tracker.AddBlocker(ctx, id, inFlight)
			}); err != nil {
				report.Notes = append(report.Notes, fmt.Sprintf("%s could not be made to wait on %s: %v", id, inFlight, err))
			}
			note := fmt.Sprintf("may duplicate %s, a story of the same rig in progress that names %s; this bug waits on it, to be checked after it lands.", inFlight, flightWhy)
			if err := writeRetrying(ctx, n, report, "the duplicate note of a bug for a finding not fixed", func() error {
				return n.Tracker.CommentOnStory(ctx, id, note)
			}); err != nil {
				report.Notes = append(report.Notes, fmt.Sprintf("%s was filed held, but its note (%s) could not be written: %v", id, note, err))
			}
			report.Notes = append(report.Notes, fmt.Sprintf("%s may duplicate %s, a story of the same rig in progress", id, inFlight))
			block = append(block, fmt.Sprintf("Filed: %s, held (may duplicate %s, in progress; waits on it). %s", id, inFlight, strings.Join(bug.lines, " ")))
			continue
		}
		if near != "" {
			n.keepJoin(ctx, c, report, fmt.Sprintf("filed %s for the finding %q of %s as a %s with %s", id, strings.Join(bug.said(), " "), c.id, why, near))
			report.Notes = append(report.Notes, fmt.Sprintf("%s may overlap %s, which names the same file or symbol but not the same fault", id, near))
			block = append(block, fmt.Sprintf("Filed: %s, held (may overlap %s). %s", id, near, strings.Join(bug.lines, " ")))
		} else {
			block = append(block, fmt.Sprintf("Filed: %s, held. %s", id, strings.Join(bug.lines, " ")))
		}
	}
	return strings.Join(block, "\n")
}
