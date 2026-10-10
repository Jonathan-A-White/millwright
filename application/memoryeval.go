package application

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// EvalFile is a rig's scripted question set in its folder of the seat: the pass
// bar for the rig's memory.
const EvalFile = "eval.md"

// EvalTop is how far down the results of a question the expected fact may be.
const EvalTop = 3

// evalStopWords are dropped from a question before it is run as a query.
var evalStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "to": true, "of": true, "in": true, "on": true,
	"is": true, "does": true, "how": true, "what": true, "when": true, "where": true,
}

// EvalQuestion is one block of an eval.md: what a Builder would ask, and the
// slugs of the facts that answer it.
type EvalQuestion struct {
	Question string
	Expect   []string
	// Line is where the block starts in the file.
	Line int
}

// Terms are the question's words as query terms: lower-case, edged punctuation
// off, stop words dropped.
func (q EvalQuestion) Terms() []string {
	var terms []string
	for _, word := range strings.Fields(q.Question) {
		word = strings.ToLower(strings.TrimFunc(word, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}))
		if word != "" && !evalStopWords[word] {
			terms = append(terms, word)
		}
	}
	return terms
}

// ParseEval reads an eval.md: blocks of 'Q: <question>' and 'expect: <slug>,
// <slug>...', separated by blank lines; a line starting with '#' is a comment.
// A block that is not a Q with an expect, or has no words to look for in its Q,
// is an error naming its line.
func ParseEval(text string) ([]EvalQuestion, error) {
	var out []EvalQuestion
	var cur EvalQuestion
	var hasExpect bool
	closeBlock := func() error {
		defer func() { cur, hasExpect = EvalQuestion{}, false }()
		if cur.Line == 0 {
			return nil
		}
		if cur.Question == "" {
			return fmt.Errorf("%s line %d: an expect with no Q", EvalFile, cur.Line)
		}
		if !hasExpect {
			return fmt.Errorf("%s line %d: the question %q has no expect", EvalFile, cur.Line, cur.Question)
		}
		if len(cur.Terms()) == 0 {
			return fmt.Errorf("%s line %d: the question %q has no words to look for", EvalFile, cur.Line, cur.Question)
		}
		out = append(out, cur)
		return nil
	}
	for i, raw := range strings.Split(text, "\n") {
		line, no := strings.TrimSpace(raw), i+1
		switch {
		case strings.HasPrefix(line, "#"):
		case line == "":
			if err := closeBlock(); err != nil {
				return nil, err
			}
		case strings.HasPrefix(line, "Q:"), strings.HasPrefix(line, "expect:"):
			if cur.Line == 0 {
				cur.Line = no
			}
			if q, ok := strings.CutPrefix(line, "Q:"); ok {
				if cur.Question != "" {
					return nil, fmt.Errorf("%s line %d: a second Q in one block: separate blocks with a blank line", EvalFile, no)
				}
				cur.Question = strings.TrimSpace(q)
				continue
			}
			if hasExpect {
				return nil, fmt.Errorf("%s line %d: a second expect in one block: give the slugs on one line, comma-separated", EvalFile, no)
			}
			hasExpect = true
			for _, slug := range strings.Split(strings.TrimPrefix(line, "expect:"), ",") {
				if slug = strings.TrimSpace(slug); slug != "" {
					cur.Expect = append(cur.Expect, slug)
				}
			}
			if len(cur.Expect) == 0 {
				return nil, fmt.Errorf("%s line %d: expect names no slug", EvalFile, no)
			}
		default:
			return nil, fmt.Errorf("%s line %d: %q is neither a Q:, an expect:, a # comment nor blank", EvalFile, no, line)
		}
	}
	if err := closeBlock(); err != nil {
		return nil, err
	}
	return out, nil
}

// EvalReport is what running a rig's eval found.
type EvalReport struct {
	// Lines are the PASS and FAIL lines, one for each slug each question expects.
	Lines []string
	// Failed and Total count questions: one with any slug missing is failed.
	Failed, Total int
}

// runEval runs the rig's eval.md against its facts, whatever their status. found
// is false when the rig has no eval.md. It reads files only.
func (m Memory) runEval(ctx context.Context, rig string) (report EvalReport, found bool, err error) {
	_, _, bySlug, err := m.load(ctx, rig)
	if err != nil {
		return EvalReport{}, false, err
	}
	text, found, err := m.Files.ReadRigEval(ctx, m.Seat, rig)
	if err != nil || !found {
		return EvalReport{}, false, err
	}
	questions, err := ParseEval(text)
	if err != nil {
		return EvalReport{}, true, err
	}
	all := orderedFacts(bySlug)
	for _, q := range questions {
		var hits []RigFact
		for _, fact := range all {
			if fact.matches(q.Terms()) {
				hits = append(hits, fact)
			}
		}
		failed := false
		for _, slug := range q.Expect {
			rank := 0
			for i, fact := range hits {
				if fact.Slug == slug {
					rank = i + 1
					break
				}
			}
			if rank == 0 || rank > EvalTop {
				failed = true
				report.Lines = append(report.Lines, fmt.Sprintf("FAIL %s not in top %d  %s", slug, EvalTop, q.Question))
				continue
			}
			report.Lines = append(report.Lines, fmt.Sprintf("PASS %d %s  %s", rank, slug, q.Question))
		}
		report.Total++
		if failed {
			report.Failed++
		}
	}
	return report, true, nil
}

// Eval prints the PASS or FAIL line of each fact each question of the rig's
// eval.md expects, a fact passing when it is among the first three results of
// the question run as a query. A rig with no eval.md prints 'no eval'. It is
// an error when any question fails. It reads files only.
func (m Memory) Eval(ctx context.Context, rig string) error {
	report, found, err := m.runEval(ctx, rig)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(m.Out, "no eval")
		return nil
	}
	for _, line := range report.Lines {
		fmt.Fprintln(m.Out, line)
	}
	if report.Failed > 0 {
		return fmt.Errorf("eval failing: %d of %d", report.Failed, report.Total)
	}
	return nil
}

// RigEvalFailure is a rig whose eval is failing, for mw status.
type RigEvalFailure struct {
	Rig string
	// Failed and Total count questions. When Problem is set the eval.md could
	// not be read and they are zero.
	Failed, Total int
	Problem       string
}

// Line is the failure as mw status prints it under RIG MEMORY.
func (f RigEvalFailure) Line() string {
	if f.Problem != "" {
		return fmt.Sprintf("%s eval failing: %s", f.Rig, f.Problem)
	}
	return fmt.Sprintf("%s eval failing: %d of %d", f.Rig, f.Failed, f.Total)
}
