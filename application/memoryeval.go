package application

import (
	"context"
	"fmt"
	pathpkg "path"
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
		if term := queryTerm(word); term != "" {
			terms = append(terms, term)
		}
	}
	return terms
}

// queryTerm is a word of a question as a query term, or "" for a stop word.
func queryTerm(word string) string {
	word = strings.ToLower(strings.TrimFunc(word, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}))
	if evalStopWords[word] {
		return ""
	}
	return word
}

// without is the question with each word that is one of the terms taken out.
func (q EvalQuestion) without(terms []string) EvalQuestion {
	var kept []string
	for _, word := range strings.Fields(q.Question) {
		if indexOf(terms, queryTerm(word)) < 0 || queryTerm(word) == "" {
			kept = append(kept, word)
		}
	}
	q.Question = strings.Join(kept, " ")
	return q
}

// unfound are the slugs the question expects that are not among the first
// EvalTop results of the question run as a query over all.
func (q EvalQuestion) unfound(all []RigFact) []string {
	var out []string
	for _, slug := range q.Expect {
		if !q.finds(all, slug) {
			out = append(out, slug)
		}
	}
	return out
}

// missingFrom are the terms of the question that no field of the fact holds: the
// words that keep a query, which needs every term, from finding it.
func (q EvalQuestion) missingFrom(fact RigFact) []string {
	var out []string
	for _, term := range q.Terms() {
		if !fact.matches([]string{term}) {
			out = append(out, term)
		}
	}
	return out
}

// expects reports whether the question names the slug among its expected facts.
func (q EvalQuestion) expects(slug string) bool {
	for _, expected := range q.Expect {
		if expected == slug {
			return true
		}
	}
	return false
}

// finds reports whether the fact is among the first EvalTop results of the
// question run as a query over all, which are in the order query reads them.
func (q EvalQuestion) finds(all []RigFact, slug string) bool {
	rank := 0
	for _, fact := range all {
		if !fact.matches(q.Terms()) {
			continue
		}
		rank++
		if fact.Slug == slug {
			return rank <= EvalTop
		}
	}
	return false
}

// addExpect adds a slug to the expect line of a question's block in an eval.md
// text, after the slugs already there.
func addExpect(text string, q EvalQuestion, slug string) string {
	lines := strings.Split(text, "\n")
	for i := q.Line - 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "expect:") {
			lines[i] = strings.TrimRight(lines[i], " \t") + ", " + slug
			break
		}
	}
	return strings.Join(lines, "\n")
}

// replaceExpect puts a slug in place of another in the expect line of a
// question's block in an eval.md text.
func replaceExpect(text string, q EvalQuestion, from, to string) string {
	lines := strings.Split(text, "\n")
	for i := q.Line - 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "expect:") {
			var slugs []string
			for _, slug := range strings.Split(strings.TrimPrefix(strings.TrimSpace(lines[i]), "expect:"), ",") {
				if slug = strings.TrimSpace(slug); slug == from {
					slug = to
				}
				if slug != "" {
					slugs = append(slugs, slug)
				}
			}
			lines[i] = "expect: " + strings.Join(slugs, ", ")
			break
		}
	}
	return strings.Join(lines, "\n")
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
	// Failed and Total count questions: one with any slug missing or stale is
	// failed. Stale counts the slugs expected that are superseded or retired,
	// and is part of what Failed counts.
	Failed, Total, Stale int
	// Questions are the questions run, in the order of the file, each with
	// whether every slug it expects was found.
	Questions []EvalResult
}

// EvalResult is one question of an eval.md and whether it passed.
type EvalResult struct {
	EvalQuestion
	Failed bool
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
	report, err = evalText(text, bySlug)
	return report, true, err
}

// staleExpect is an expected fact that is superseded or retired: what an eval
// says of it, and the current fact (nil when there is none) that took its place.
type staleExpect struct {
	// Why finishes 'STALE <slug>: '.
	Why string
	// Head is the current (or flagged) fact at the end of the supersede chain,
	// or the one a retire reason names as merged into, nil when none.
	Head *RigFact
	// WhyNoHead says why a stale fact with no Head has none, as a drop gives it.
	WhyNoHead string
}

// staleOf reports whether the fact is superseded or retired, with the fact that
// took its place. A flagged (recheck) fact is not stale.
func staleOf(fact RigFact, facts map[string]RigFact) (staleExpect, bool) {
	switch fact.Status {
	case FactRetired:
		reason := "retired"
		if fact.Reason != "" {
			reason += ": " + fact.Reason
		}
		return staleExpect{Why: reason, Head: mergedInto(fact.Reason, facts, fact.Slug), WhyNoHead: reason}, true
	case FactSuperseded:
		if fact.SupersededBy == "" {
			return staleExpect{Why: "superseded, naming no successor", WhyNoHead: "superseded, naming no successor"}, true
		}
		head, status := fact.SupersededBy, "missing"
		seen := map[string]bool{fact.Slug: true}
		for {
			next, known := facts[head]
			if !known {
				break
			}
			status = string(next.Status)
			if next.Status != FactSuperseded || next.SupersededBy == "" || seen[next.Slug] {
				if next.Status == FactCurrent || next.Status == FactRecheck {
					return staleExpect{Why: fmt.Sprintf("superseded by %s (%s)", head, status), Head: &next}, true
				}
				break
			}
			seen[next.Slug] = true
			head = next.SupersededBy
		}
		return staleExpect{
			Why:       fmt.Sprintf("superseded by %s (%s)", head, status),
			WhyNoHead: fmt.Sprintf("superseded by %s, which is %s", head, status),
		}, true
	}
	return staleExpect{}, false
}

// evalText runs the questions of an eval.md text against facts. An expect whose
// fact is superseded or retired is STALE, and fails its question.
func evalText(text string, bySlug map[string]RigFact) (report EvalReport, err error) {
	questions, err := ParseEval(text)
	if err != nil {
		return EvalReport{}, err
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
			if stale, ok := staleOf(bySlug[slug], bySlug); ok {
				failed = true
				report.Stale++
				report.Lines = append(report.Lines, fmt.Sprintf("STALE %s: %s  %s", slug, stale.Why, q.Question))
				continue
			}
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
		report.Questions = append(report.Questions, EvalResult{EvalQuestion: q, Failed: failed})
		if failed {
			report.Failed++
		}
	}
	return report, nil
}

// Eval prints the PASS, FAIL or STALE line of each fact each question of the
// rig's eval.md expects, a fact passing when it is among the first three
// results of the question run as a query, and STALE when it is superseded or
// retired (mw memory eval --fix rewrites those). A rig with no eval.md prints
// 'no eval'. It is an error when any question fails. It reads files only.
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
	return report.err()
}

// err is the failure of a report that has one.
func (r EvalReport) err() error {
	if r.Failed == 0 {
		return nil
	}
	if r.Stale > 0 {
		return fmt.Errorf("eval failing: %d of %d (%d stale: mw memory eval --fix rewrites them)", r.Failed, r.Total, r.Stale)
	}
	return fmt.Errorf("eval failing: %d of %d", r.Failed, r.Total)
}

// EvalFix rewrites the rig's eval.md so that no question expects a superseded
// or retired fact, and commits it. A stale slug is replaced in place by the
// current fact that took its place (the head of its supersede chain, or the one
// a retire reason is merged into) unless the question already expects that
// fact, and is dropped when it has none or the question does; a question left
// with no expect is removed whole. Everything else in the text is kept as it
// is. It prints one line for each change, then what still fails the eval, and
// is an error when something does.
//
// A question that has a slug replaced is run again over the facts. One that
// the new fact does not put in its top EvalTop is named in a 'REWORD <slug>  Q
// (missing from the fact: <terms>)' line, the terms being the question's words
// the fact lacks, since a query needs every term.
func (m Memory) EvalFix(ctx context.Context, rig string) error {
	return m.evalFix(ctx, rig, false)
}

// EvalFixReword is EvalFix that also drops those missing words from a rewritten
// question, keeping the change only when the question then finds its facts in
// its top EvalTop; otherwise it names the question in a REWORD line as EvalFix
// does and leaves its words as they were.
func (m Memory) EvalFixReword(ctx context.Context, rig string) error {
	return m.evalFix(ctx, rig, true)
}

func (m Memory) evalFix(ctx context.Context, rig string, reword bool) error {
	_, _, bySlug, err := m.load(ctx, rig)
	if err != nil {
		return err
	}
	text, found, err := m.Files.ReadRigEval(ctx, m.Seat, rig)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(m.Out, "no eval")
		return nil
	}
	questions, err := ParseEval(text)
	if err != nil {
		return err
	}
	type edit struct {
		q      EvalQuestion
		expect []string
		// question is the Q to write, "" to keep the one there.
		question string
	}
	var edits []edit
	all := orderedFacts(bySlug)
	// rewords are the REWORD lines, by the FAIL line each stands in for.
	rewords := map[string]string{}
	for _, q := range questions {
		expect := append([]string(nil), q.Expect...)
		changed := false
		var said, gone []string
		for _, slug := range q.Expect {
			stale, ok := staleOf(bySlug[slug], bySlug)
			if !ok {
				continue
			}
			changed = true
			at := indexOf(expect, slug)
			switch {
			case stale.Head == nil:
				expect = append(expect[:at], expect[at+1:]...)
				said = append(said, fmt.Sprintf("dropped %s at line %d (%s): %s", slug, q.Line, q.Question, stale.WhyNoHead))
				gone = append(gone, slug+" "+stale.WhyNoHead)
			case indexOf(expect, stale.Head.Slug) >= 0:
				expect = append(expect[:at], expect[at+1:]...)
				said = append(said, fmt.Sprintf("dropped %s at line %d (%s): %s is already expected", slug, q.Line, q.Question, stale.Head.Slug))
				gone = append(gone, slug+" replaced by "+stale.Head.Slug)
			default:
				expect[at] = stale.Head.Slug
				said = append(said, fmt.Sprintf("replaced %s with %s at line %d (%s)", slug, stale.Head.Slug, q.Line, q.Question))
			}
		}
		if len(expect) == 0 {
			// One line for the block, not one for each slug it loses.
			said = []string{fmt.Sprintf("removed the question at line %d (%s): no expect left (%s)", q.Line, q.Question, strings.Join(gone, "; "))}
		}
		e := edit{q: q, expect: expect}
		if changed && len(expect) > 0 {
			var note string
			e.question, note = rewordNote(q, expect, bySlug, all, reword, rewords)
			if note != "" {
				said = append(said, note)
			}
		}
		for _, line := range said {
			fmt.Fprintln(m.Out, line)
		}
		if changed {
			edits = append(edits, e)
		}
	}
	if len(edits) == 0 {
		fmt.Fprintln(m.Out, "nothing stale")
	} else {
		// From the last block up, so that a removal does not move the lines of
		// the blocks before it.
		for i := len(edits) - 1; i >= 0; i-- {
			if len(edits[i].expect) == 0 {
				text = removeBlock(text, edits[i].q)
			} else {
				text = setExpect(text, edits[i].q, edits[i].expect)
				if edits[i].question != "" {
					text = setQuestion(text, edits[i].q, edits[i].question)
				}
			}
		}
		if _, err := m.Files.WriteRigEval(ctx, m.Seat, rig, text); err != nil {
			return err
		}
		path := pathpkg.Join(SeatsDir, m.Seat, RigsDir, rig, EvalFile)
		if err := m.record(ctx, "eval fix "+rig, path); err != nil {
			return err
		}
	}
	report, err := evalText(text, bySlug)
	if err != nil {
		return err
	}
	for _, line := range report.Lines {
		if reworded, ok := rewords[line]; ok {
			line = reworded
		}
		if !strings.HasPrefix(line, "PASS ") {
			fmt.Fprintln(m.Out, line)
		}
	}
	return report.err()
}

// rewordNote runs a question again with the expect a fix gave it, over all the
// facts. For each expected fact it does not find in its top EvalTop that lacks
// words of the question, it records the REWORD line that stands in for the FAIL
// line the final run prints. With reword set it first drops those words from
// the question, and when the question then finds every fact it returns the
// question to write and a line saying so, and records no REWORD line.
func rewordNote(q EvalQuestion, expect []string, bySlug map[string]RigFact, all []RigFact, reword bool, rewords map[string]string) (question, note string) {
	fixed := q
	fixed.Expect = expect
	lines := map[string]string{}
	var union []string
	for _, slug := range fixed.unfound(all) {
		missing := fixed.missingFrom(bySlug[slug])
		if len(missing) == 0 {
			continue
		}
		for _, term := range missing {
			if indexOf(union, term) < 0 {
				union = append(union, term)
			}
		}
		fail := fmt.Sprintf("FAIL %s not in top %d  %s", slug, EvalTop, q.Question)
		lines[fail] = fmt.Sprintf("REWORD %s  %s  (missing from the fact: %s)", slug, q.Question, strings.Join(missing, ", "))
	}
	if len(lines) == 0 {
		return "", ""
	}
	if reword {
		trimmed := fixed.without(union)
		if len(trimmed.Terms()) > 0 && len(trimmed.unfound(all)) == 0 {
			return trimmed.Question, fmt.Sprintf("reworded the question at line %d (%s): dropped %s, which the fact lacks; now %s", q.Line, q.Question, strings.Join(union, ", "), trimmed.Question)
		}
	}
	for fail, line := range lines {
		rewords[fail] = line
	}
	return "", ""
}

func indexOf(slugs []string, slug string) int {
	for i, s := range slugs {
		if s == slug {
			return i
		}
	}
	return -1
}

// setExpect puts the slugs on the expect line of a question's block in an
// eval.md text.
func setExpect(text string, q EvalQuestion, slugs []string) string {
	lines := strings.Split(text, "\n")
	for i := q.Line - 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "expect:") {
			lines[i] = "expect: " + strings.Join(slugs, ", ")
			break
		}
	}
	return strings.Join(lines, "\n")
}

// setQuestion puts a question on the Q line of a question's block in an eval.md
// text.
func setQuestion(text string, q EvalQuestion, question string) string {
	lines := strings.Split(text, "\n")
	for i := q.Line - 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "Q:") {
			lines[i] = "Q: " + question
			break
		}
	}
	return strings.Join(lines, "\n")
}

// removeBlock takes a question's block out of an eval.md text, with the blank
// lines after it, or before it when it is the last block.
func removeBlock(text string, q EvalQuestion) string {
	lines := strings.Split(text, "\n")
	start, end := q.Line-1, q.Line-1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
		end++
	}
	after := end
	for after < len(lines) && strings.TrimSpace(lines[after]) == "" {
		after++
	}
	if after < len(lines) {
		end = after
	} else {
		for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
			start--
		}
	}
	return strings.Join(append(lines[:start:start], lines[end:]...), "\n")
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
