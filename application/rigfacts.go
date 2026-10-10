package application

import (
	"fmt"
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
// current are left out, because every byte of it is paid on every story. It has
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
			if fact.Kind == group.kind && fact.Status == FactCurrent {
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
			lines = append(lines, fmt.Sprintf("- [%s] %s (%s)", fact.Subject, fact.Sentence, fact.Source))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	return strings.Join(sections, "\n\n")
}
