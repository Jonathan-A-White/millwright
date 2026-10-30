package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// Criterion is one numbered item of a story's acceptance criteria.
type Criterion struct {
	Number int
	Text   string
}

// Mechanism is one kind of change a story's criteria ask for, with the criteria
// that ask for it.
type Mechanism struct {
	Name     string
	Criteria []Criterion
}

// mechanismRules are the kinds of change the heuristic tells apart (mw-gq6.852),
// in the order they are named: a new setting, a new component, a removal. They
// are the three that made mw-5r3p30.843 (an L story of three new mechanisms) run
// out of turns. A criterion is of a kind when its words match; one criterion may
// be of more than one. The match is on words, so it errs both ways, and only a
// warning is made of it.
var mechanismRules = []struct {
	name  string
	words *regexp.Regexp
}{
	{"a new setting", regexp.MustCompile(`\b(new|adds?|added)\b[^.;]{0,30}\b(setting|config key|config option|option|flag|knob|threshold|env var)s?\b`)},
	{"a new component", regexp.MustCompile(`\b(new|adds?|added)\b[^.;]{0,30}\b(component|screen|page|command|subcommand|package|port|adapter|mechanism|module|service|endpoint|alarm|view|tab|dialog|worker|job)s?\b`)},
	{"a removal", regexp.MustCompile(`\b(remove[sd]?|removal|delete[sd]?|deletion|retire[sd]?|drops?|dropped)\b`)},
}

var (
	// criterionMark is the number that begins an item: at the start of a line, or
	// after the end of the item before it when items run on in one line. A number
	// inside a sentence ("default 10.") is not one.
	criterionMark = regexp.MustCompile(`(?m)(?:^[ \t]*|[.;:][ \t]+)(\d+)[.)]\s+`)
	notACriterion = regexp.MustCompile(`(?i)^\s*(?:\d+\s*[.)]?\s*)?(?:sweep|expect):`)
)

// AcceptanceCriteria splits acceptance into its numbered items, leaving out the
// Sweep: and Expect: lines, which say how the story is judged and not what it
// does. Text with no numbers is one criterion, number 1.
func AcceptanceCriteria(acceptance string) []Criterion {
	var kept []string
	for _, line := range strings.Split(acceptance, "\n") {
		if !notACriterion.MatchString(line) {
			kept = append(kept, line)
		}
	}
	text := strings.Join(kept, "\n")
	marks := criterionMark.FindAllStringSubmatchIndex(text, -1)
	if len(marks) == 0 {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []Criterion{{Number: 1, Text: strings.TrimSpace(text)}}
	}
	var found []Criterion
	for i, m := range marks {
		end := len(text)
		if i+1 < len(marks) {
			end = marks[i+1][2]
		}
		var number int
		fmt.Sscanf(text[m[2]:m[3]], "%d", &number)
		found = append(found, Criterion{Number: number, Text: strings.TrimSpace(text[m[1]:end])})
	}
	return found
}

// AcceptanceMechanisms are the kinds of change the criteria ask for, each with
// the criteria that ask for it, in the order of mechanismRules.
func AcceptanceMechanisms(acceptance string) []Mechanism {
	criteria := AcceptanceCriteria(acceptance)
	var found []Mechanism
	for _, rule := range mechanismRules {
		mechanism := Mechanism{Name: rule.name}
		for _, criterion := range criteria {
			if rule.words.MatchString(strings.ToLower(criterion.Text)) {
				mechanism.Criteria = append(mechanism.Criteria, criterion)
			}
		}
		if len(mechanism.Criteria) > 0 {
			found = append(found, mechanism)
		}
	}
	return found
}

// MechanismWarning is what is said of an L or XL story whose criteria touch more
// than one mechanism, naming the criteria of each (mw-gq6.852): a story of
// several is more than one session finishes, and the turn cap hands it back
// unfinished. It is empty for a story of any other size or of at most one.
func MechanismWarning(label string, size Size, acceptance string) string {
	if !size.Big() {
		return ""
	}
	found := AcceptanceMechanisms(acceptance)
	if len(found) < 2 {
		return ""
	}
	parts := make([]string, 0, len(found))
	for _, mechanism := range found {
		numbers := make([]string, 0, len(mechanism.Criteria))
		for _, criterion := range mechanism.Criteria {
			numbers = append(numbers, fmt.Sprint(criterion.Number))
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", mechanism.Name, strings.Join(numbers, ", ")))
	}
	return fmt.Sprintf("WARN: size %s with %d mechanisms in its acceptance criteria: %s; split it into one story per mechanism, for %s",
		size, len(found), strings.Join(parts, ", "), label)
}
