package application

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FactKind is what sort of thing a fact records.
type FactKind string

const (
	// FactGotcha is a thing that cost a session time and would again.
	FactGotcha FactKind = "gotcha"
	// FactDecision is a choice already made, so that it is not made twice.
	FactDecision FactKind = "decision"
)

// FactStatus is where a fact stands. Only a current fact is ever read at boot.
type FactStatus string

const (
	FactCurrent    FactStatus = "current"
	FactRecheck    FactStatus = "recheck"
	FactSuperseded FactStatus = "superseded"
	FactRetired    FactStatus = "retired"
)

// FactsDir is the directory under a rig's own directory in a seat's rigs
// directory that holds its facts, one file each. A rig that has it is kept as
// facts; a rig without it is the one memory file it always was.
const FactsDir = "facts"

// AboutFile is a facts rig's own text: what the rig is, read before its facts.
const AboutFile = "about.md"

// FactDateLayout is how a fact's dates are written.
const FactDateLayout = "2006-01-02"

// RigFact is one thing a seat knows about one rig, kept in a file of its own
// (seats/<seat>/rigs/<rig>/facts/<slug>.md): a front matter of typed keys, then
// one sentence. The sentence is what a Builder reads; everything else is for
// the Mayor, who keeps the facts, and costs a Builder nothing at boot.
type RigFact struct {
	// Slug is the file's name without its extension. It is the fact's identity,
	// and what Supersedes and SupersededBy name.
	Slug     string
	Subject  string
	Kind     FactKind
	Status   FactStatus
	Source   string
	Since    string // YYYY-MM-DD
	Sentence string

	// The rest is optional.
	Supersedes   string
	SupersededBy string
	Retired      string // YYYY-MM-DD
	Reason       string
	// Unbooted keeps a current fact out of the boot render (front matter
	// boot: no). It is still a fact: query and list show it.
	Unbooted bool
}

const (
	factSubject      = "subject"
	factKind         = "kind"
	factStatus       = "status"
	factSource       = "source"
	factSince        = "since"
	factSupersedes   = "supersedes"
	factSupersededBy = "superseded-by"
	factRetired      = "retired"
	factReason       = "reason"
	factBoot         = "boot"
)

const frontMatterFence = "---"

// ParseRigFact reads one fact file. It refuses what is not a whole fact — a
// missing key, a kind or status that is not one of the known ones, a date that
// is not a date, no sentence or more than one line of it — because a fact the
// Mayor cannot trust the shape of is better named and skipped than guessed at.
func ParseRigFact(slug, text string) (RigFact, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != frontMatterFence {
		return RigFact{}, fmt.Errorf("no front matter: the file must open with %s", frontMatterFence)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == frontMatterFence {
			end = i
			break
		}
	}
	if end < 0 {
		return RigFact{}, fmt.Errorf("the front matter is never closed with %s", frontMatterFence)
	}

	fact := RigFact{Slug: slug}
	seen := map[string]bool{}
	for _, line := range lines[1:end] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, raw, ok := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return RigFact{}, fmt.Errorf("front matter line %q is not key: value", line)
		}
		if seen[key] {
			return RigFact{}, fmt.Errorf("the key %q is given twice", key)
		}
		seen[key] = true
		value, err := factValue(raw)
		if err != nil {
			return RigFact{}, fmt.Errorf("the value of %q: %w", key, err)
		}
		switch key {
		case factSubject:
			fact.Subject = value
		case factKind:
			fact.Kind = FactKind(value)
		case factStatus:
			fact.Status = FactStatus(value)
		case factSource:
			fact.Source = value
		case factSince:
			fact.Since = value
		case factSupersedes:
			fact.Supersedes = value
		case factSupersededBy:
			fact.SupersededBy = value
		case factRetired:
			fact.Retired = value
		case factReason:
			fact.Reason = value
		case factBoot:
			switch value {
			case "no":
				fact.Unbooted = true
			case "yes":
			default:
				return RigFact{}, fmt.Errorf("the value of %q: %s is not yes or no", key, value)
			}
		default:
			return RigFact{}, fmt.Errorf("unknown key %q", key)
		}
	}

	var sentences []string
	for _, line := range lines[end+1:] {
		if strings.TrimSpace(line) != "" {
			sentences = append(sentences, strings.TrimSpace(line))
		}
	}
	switch len(sentences) {
	case 0:
		return RigFact{}, fmt.Errorf("no sentence after the front matter")
	case 1:
		fact.Sentence = sentences[0]
	default:
		return RigFact{}, fmt.Errorf("%d lines after the front matter; a fact is one sentence", len(sentences))
	}

	if err := fact.validate(); err != nil {
		return RigFact{}, err
	}
	return fact, nil
}

// validate is what makes a fact whole: its required keys, its known kind and
// status, and its dates.
func (f RigFact) validate() error {
	for _, required := range [][2]string{{factSubject, f.Subject}, {factSource, f.Source}, {factSince, f.Since}} {
		if required[1] == "" {
			return fmt.Errorf("the key %q is missing", required[0])
		}
	}
	switch f.Kind {
	case FactGotcha, FactDecision:
	case "":
		return fmt.Errorf("the key %q is missing", factKind)
	default:
		return fmt.Errorf("unknown kind %q (gotcha or decision)", f.Kind)
	}
	switch f.Status {
	case FactCurrent, FactRecheck, FactSuperseded, FactRetired:
	case "":
		return fmt.Errorf("the key %q is missing", factStatus)
	default:
		return fmt.Errorf("unknown status %q (current, recheck, superseded or retired)", f.Status)
	}
	if _, err := time.Parse(FactDateLayout, f.Since); err != nil {
		return fmt.Errorf("since %q is not a YYYY-MM-DD date", f.Since)
	}
	if f.Retired != "" {
		if _, err := time.Parse(FactDateLayout, f.Retired); err != nil {
			return fmt.Errorf("retired %q is not a YYYY-MM-DD date", f.Retired)
		}
	}
	return nil
}

// factValue is a front matter value: the text after the colon, or, when that
// opens with a double quote, the Go-quoted string it holds.
func factValue(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, `"`) {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("a quoted value that does not close: %s", value)
		}
		return unquoted, nil
	}
	return value, nil
}

// writeFactValue is the inverse of factValue: a value that would not read back
// as it is gets quoted.
func writeFactValue(value string) string {
	if value != strings.TrimSpace(value) || strings.HasPrefix(value, `"`) || strings.ContainsAny(value, "\n\r") {
		return strconv.Quote(value)
	}
	return value
}

// String is the fact as its file holds it. ParseRigFact reads it back.
func (f RigFact) String() string {
	var b strings.Builder
	b.WriteString(frontMatterFence + "\n")
	for _, pair := range [][2]string{
		{factSubject, f.Subject}, {factKind, string(f.Kind)}, {factStatus, string(f.Status)},
		{factSource, f.Source}, {factSince, f.Since}, {factSupersedes, f.Supersedes},
		{factSupersededBy, f.SupersededBy}, {factRetired, f.Retired}, {factReason, f.Reason},
	} {
		if pair[1] != "" {
			fmt.Fprintf(&b, "%s: %s\n", pair[0], writeFactValue(pair[1]))
		}
	}
	if f.Unbooted {
		fmt.Fprintf(&b, "%s: no\n", factBoot)
	}
	b.WriteString(frontMatterFence + "\n\n")
	b.WriteString(f.Sentence + "\n")
	return b.String()
}

// LoadRigFacts parses the fact files of one rig, by file name. A file that is
// not a whole fact is left out and named, with why, in the second result: a bad
// fact never fails a boot. Both results are in file-name order.
func LoadRigFacts(files map[string]string) (facts []RigFact, skipped []string) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fact, err := ParseRigFact(strings.TrimSuffix(name, MemoryExt), files[name])
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		facts = append(facts, fact)
	}
	return facts, skipped
}

// RenderRigMemory is what a Builder reads of a rig kept as facts: the about
// text, then the current decisions, then the current gotchas grouped by
// subject, one line each. Front matter, file names, and every fact that is not
// current, and a current one with boot: no, are left out, because every byte of
// it is paid on every story. It has
// no trailing newline, and its length is the size mw status holds to the budget.
func RenderRigMemory(about string, facts []RigFact) string {
	var sections []string
	if about = strings.TrimSpace(about); about != "" {
		sections = append(sections, about)
	}
	for _, group := range []struct {
		heading string
		kind    FactKind
	}{{"## Decisions", FactDecision}, {"## Gotchas", FactGotcha}} {
		var current []RigFact
		for _, fact := range facts {
			if fact.Kind == group.kind && fact.Status == FactCurrent && !fact.Unbooted {
				current = append(current, fact)
			}
		}
		if len(current) == 0 {
			continue
		}
		sort.SliceStable(current, func(i, j int) bool {
			if current[i].Subject != current[j].Subject {
				return current[i].Subject < current[j].Subject
			}
			return current[i].Slug < current[j].Slug
		})
		lines := []string{group.heading, ""}
		for _, fact := range current {
			lines = append(lines, renderedFact(fact))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	return strings.Join(sections, "\n\n")
}

// renderedFact is how a render shows one fact: its subject, its sentence and where
// it came from.
func renderedFact(fact RigFact) string {
	return fmt.Sprintf("- [%s] %s (%s)", fact.Subject, fact.Sentence, fact.Source)
}

// PathFactsCap is the most bytes the section of facts about a story's files may
// take at boot, its heading included (mw-t5shinh.4).
const PathFactsCap = 3000

// PathFactsHeading opens the boot's memory section for a story whose files
// have facts.
const PathFactsHeading = "## For the files this story names"

// RenderBootMemory is what a Builder of a story reads of a rig kept as facts:
// when the story's files key names files that current facts are about, the
// section of those facts first, then the general render without them, then one
// line naming what was left out to make room. The whole is never larger than
// RenderRigMemory alone: booted gotchas the story's files do not match leave
// first, oldest since first, never a decision and never a fact that names the
// Governor; if that does not free enough, the section of path facts is cut.
// With no files, or no fact about them, it is RenderRigMemory itself.
func RenderBootMemory(rig, about string, facts []RigFact, files string) string {
	general := RenderRigMemory(about, facts)
	matched := factsForFiles(facts, files)
	if len(matched) == 0 {
		return general
	}

	// Gotchas that may leave, oldest first.
	named := map[string]bool{}
	for _, fact := range matched {
		named[fact.Slug] = true
	}
	var removable []RigFact
	for _, fact := range facts {
		if fact.Kind == FactGotcha && fact.Status == FactCurrent && !fact.Unbooted && !named[fact.Slug] && !namesTheGovernor(fact) {
			removable = append(removable, fact)
		}
	}
	sort.SliceStable(removable, func(i, j int) bool {
		if removable[i].Since != removable[j].Since {
			return removable[i].Since < removable[j].Since
		}
		return removable[i].Slug < removable[j].Slug
	})

	// The most path facts the cap takes, in order.
	size := len(PathFactsHeading) + 2
	shownMost := 0
	for _, fact := range matched {
		size += len(renderedFact(fact)) + 1
		if size-1 > PathFactsCap {
			break
		}
		shownMost++
	}

	for shown := shownMost; shown >= 0; shown-- {
		for left := 0; left <= len(removable); left++ {
			gone := map[string]bool{}
			var section []string
			if shown > 0 {
				section = []string{PathFactsHeading, ""}
			}
			for _, fact := range matched[:shown] {
				gone[fact.Slug] = true
				section = append(section, renderedFact(fact))
			}
			for _, fact := range removable[:left] {
				gone[fact.Slug] = true
			}
			unshown := 0
			for _, fact := range matched[shown:] {
				if fact.Unbooted {
					unshown++
				}
			}
			var rest []RigFact
			for _, fact := range facts {
				if !gone[fact.Slug] {
					rest = append(rest, fact)
				}
			}

			var parts []string
			if len(section) > 0 {
				parts = append(parts, strings.Join(section, "\n"))
			}
			if render := RenderRigMemory(about, rest); render != "" {
				parts = append(parts, render)
			}
			if line := leftOutLine(rig, removable[:left], unshown); line != "" {
				parts = append(parts, line)
			}
			if render := strings.Join(parts, "\n\n"); len(render) <= len(general) {
				return render
			}
		}
	}
	return general
}

// leftOutLine names what a boot left out to make room for the facts about the
// story's files: the subjects of the gotchas that left, how many facts about
// the files were not shown, and the command that reads them. Empty when nothing
// was left out.
func leftOutLine(rig string, gotchas []RigFact, unshown int) string {
	var what []string
	if len(gotchas) > 0 {
		subjects := make([]string, len(gotchas))
		for i, fact := range gotchas {
			subjects[i] = "[" + fact.Subject + "]"
		}
		what = append(what, "the gotchas "+strings.Join(subjects, ", "))
	}
	if unshown > 0 {
		noun := "facts"
		if unshown == 1 {
			noun = "fact"
		}
		what = append(what, fmt.Sprintf("%d %s about this story's files that did not fit", unshown, noun))
	}
	if len(what) == 0 {
		return ""
	}
	return fmt.Sprintf("Left out of this boot to make room: %s. Read them with mw memory query %s <term>.", strings.Join(what, "; and "), rig)
}

// factsForFiles is the current facts, booted or not, whose subject is a file the
// story's files key names, a folder above one, or a path under a named folder or
// glob: an exact file or glob match first, then the deepest folder, newest first.
func factsForFiles(facts []RigFact, files string) []RigFact {
	var patterns []string
	for _, pattern := range strings.Split(files, ",") {
		pattern = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(pattern), "./"), "/")
		if pattern != "" {
			patterns = append(patterns, pattern)
		}
	}
	if len(patterns) == 0 {
		return nil
	}

	type found struct {
		fact  RigFact
		exact bool
		depth int
	}
	var matched []found
	for _, fact := range facts {
		if fact.Status != FactCurrent {
			continue
		}
		subject := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(fact.Subject), "./"), "/")
		best, hit := found{fact: fact}, false
		for _, pattern := range patterns {
			exact, ok := subjectMatches(subject, pattern)
			if ok && (!hit || (exact && !best.exact)) {
				best.exact, hit = exact, true
			}
		}
		if hit {
			best.depth = strings.Count(subject, "/")
			matched = append(matched, best)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if a.exact != b.exact {
			return a.exact
		}
		if a.depth != b.depth {
			return a.depth > b.depth
		}
		if a.fact.Since != b.fact.Since {
			return a.fact.Since > b.fact.Since
		}
		return a.fact.Slug < b.fact.Slug
	})
	out := make([]RigFact, len(matched))
	for i, m := range matched {
		out[i] = m.fact
	}
	return out
}

// subjectMatches reports whether a fact's subject is about a story's file or
// glob, and whether it is the file itself (or a path the glob matches) rather
// than a folder above it or a path under a named folder.
func subjectMatches(subject, pattern string) (exact, ok bool) {
	if subject == "" {
		return false, false
	}
	// A ** ends the literal part: everything under what comes before it.
	if at := strings.Index(pattern, "**"); at >= 0 {
		pattern = strings.TrimSuffix(pattern[:at], "/")
		if pattern == "" {
			return false, false
		}
	}
	if !strings.ContainsAny(pattern, "*?[") {
		switch {
		case subject == pattern:
			return true, true
		case strings.HasPrefix(pattern, subject+"/"), strings.HasPrefix(subject, pattern+"/"):
			return false, true
		}
		return false, false
	}

	if matched, err := path.Match(pattern, subject); err == nil && matched {
		return true, true
	}
	for above := path.Dir(subject); above != "." && above != "/"; above = path.Dir(above) {
		if matched, err := path.Match(pattern, above); err == nil && matched {
			return false, true
		}
	}
	// The folder above the glob: its literal leading folders.
	base := ""
	for _, part := range strings.Split(pattern, "/") {
		if strings.ContainsAny(part, "*?[") {
			break
		}
		base = path.Join(base, part)
	}
	if base != "" && (subject == base || strings.HasPrefix(base, subject+"/")) {
		return false, true
	}
	return false, false
}
