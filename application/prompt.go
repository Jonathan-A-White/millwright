package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// Prompts is where saved prompts live: the postern backend, so the Governor's
// app can list them and run one by name. Put saves a prompt whole, replacing
// one of the same name.
type Prompts interface {
	// List reports every saved prompt.
	List(ctx context.Context) ([]domain.Prompt, error)
	// Get reports the prompt saved under name; false when there is none.
	Get(ctx context.Context, name string) (domain.Prompt, bool, error)
	// Put saves p under its name.
	Put(ctx context.Context, p domain.Prompt) error
	// Delete removes the prompt saved under name; none is not an error.
	Delete(ctx context.Context, name string) error
}

// PromptSaveRequest is a prompt as the Mayor writes it: Options are each
// `<flag>:<type>=<default>`, with a `:required` suffix for one that has none.
type PromptSaveRequest struct {
	Name    string
	Summary string
	Options []string
	Body    string
}

// PromptSave saves a prompt to the backend. It checks the prompt first, so
// that nothing is saved that PromptRun could not run.
type PromptSave struct {
	Prompts Prompts
	// Out is where it says what it saved. A nil Out prints nothing.
	Out io.Writer
}

// Run saves the prompt and reports it.
func (s PromptSave) Run(ctx context.Context, req PromptSaveRequest) (domain.Prompt, error) {
	prompt := domain.Prompt{
		Name: strings.TrimSpace(req.Name), Summary: strings.TrimSpace(req.Summary),
		Signature: []string{}, Body: req.Body,
	}
	switch {
	case s.Prompts == nil:
		return prompt, fmt.Errorf("mw prompt save: there is no backend to save the prompt to")
	case !domain.ValidPromptName(prompt.Name):
		return prompt, fmt.Errorf("mw prompt save: %q is not a prompt name: 1 to 32 of lower-case letters, digits and -", req.Name)
	case prompt.Summary == "":
		return prompt, fmt.Errorf("mw prompt save: /%s needs a --summary: the line the list shows", prompt.Name)
	case strings.TrimSpace(prompt.Body) == "":
		return prompt, fmt.Errorf("mw prompt save: /%s has an empty body", prompt.Name)
	}
	for _, spec := range req.Options {
		option, err := domain.ParsePromptOption(strings.TrimSpace(spec))
		if err != nil {
			return prompt, fmt.Errorf("mw prompt save: %w", err)
		}
		prompt.Signature = append(prompt.Signature, option.String())
	}
	if _, err := prompt.Options(); err != nil {
		return prompt, fmt.Errorf("mw prompt save: %w", err)
	}
	if err := s.Prompts.Put(ctx, prompt); err != nil {
		return prompt, fmt.Errorf("mw prompt save: saving /%s: %w", prompt.Name, err)
	}
	if s.Out != nil {
		fmt.Fprintf(s.Out, "saved /%s (%s)\n", prompt.Name, prompt.SignatureText())
	}
	return prompt, nil
}

// PromptList prints every saved prompt, one to a line: its name, its summary
// and its signature, separated by tabs, by name.
type PromptList struct {
	Prompts Prompts
	Out     io.Writer
}

// Run prints the list.
func (l PromptList) Run(ctx context.Context) ([]domain.Prompt, error) {
	if l.Prompts == nil {
		return nil, fmt.Errorf("mw prompt list: there is no backend to read the prompts from")
	}
	prompts, err := l.Prompts.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("mw prompt list: %w", err)
	}
	sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })
	if l.Out != nil {
		for _, p := range prompts {
			fmt.Fprintf(l.Out, "%s\t%s\t%s\n", p.Name, oneLine(p.Summary), p.SignatureText())
		}
	}
	return prompts, nil
}

// PromptShow prints one saved prompt whole.
type PromptShow struct {
	Prompts Prompts
	Out     io.Writer
}

// Run prints the prompt named name.
func (s PromptShow) Run(ctx context.Context, name string) (domain.Prompt, error) {
	prompt, err := getPrompt(ctx, s.Prompts, "show", name)
	if err != nil {
		return prompt, err
	}
	if s.Out != nil {
		fmt.Fprintf(s.Out, "PROMPT /%s\nsummary: %s\nsignature: %s\n\n%s", prompt.Name, oneLine(prompt.Summary), prompt.SignatureText(), prompt.Body)
		if !strings.HasSuffix(prompt.Body, "\n") {
			fmt.Fprintln(s.Out)
		}
	}
	return prompt, nil
}

func getPrompt(ctx context.Context, prompts Prompts, verb, name string) (domain.Prompt, error) {
	if prompts == nil {
		return domain.Prompt{}, fmt.Errorf("mw prompt %s: there is no backend to read the prompt from", verb)
	}
	prompt, found, err := prompts.Get(ctx, name)
	if err != nil {
		return domain.Prompt{}, fmt.Errorf("mw prompt %s: %w", verb, err)
	}
	if !found {
		return domain.Prompt{}, fmt.Errorf("mw prompt %s: no saved prompt /%s: mw prompt list says which there are", verb, name)
	}
	return prompt, nil
}

// PromptFactsHeading heads the facts a run prints after the body.
const PromptFactsHeading = "FACTS"

// The headings of the facts a run prints: WaitingHeading, and these.
const (
	PromptLandedHeading = "LANDED NOT VERIFIED"
	PromptCardsHeading  = "OPEN CARDS"
	PromptDemosHeading  = "OPEN DEMOS"
	PromptHandsHeading  = "HANDS STEPS THAT WAIT"
)

// PromptRun prints a saved prompt's body with its options filled in, then the
// facts an answer to it is made of, every one read from the tracker and its
// notes: what waits on the Governor, what landed and is not yet VERIFIED, the
// open cards, the open demos and the hands steps that wait. It spends no fuel
// and writes nothing but the landed memory the view keeps.
type PromptRun struct {
	Prompts Prompts
	Tracker WorkTracker
	Notes   PosternNotes
	// Host is this host's own name; Now the clock the facts are measured by.
	Host string
	Now  func() time.Time
	Out  io.Writer
	// Err is where a failure to write the landed memory is said.
	Err io.Writer
}

// Run prints the prompt named name, called with args: `--flag value` or
// `--flag=value`, one pair for each option. A call the signature does not
// allow prints nothing, and is refused naming the signature.
func (r PromptRun) Run(ctx context.Context, name string, args []string) error {
	prompt, err := getPrompt(ctx, r.Prompts, "run", name)
	if err != nil {
		return err
	}
	given, err := parsePromptArgs(prompt, args)
	if err != nil {
		return err
	}
	body, err := prompt.Fill(given)
	if err != nil {
		return fmt.Errorf("mw prompt run: %w", err)
	}
	if r.Tracker == nil || r.Notes == nil {
		return fmt.Errorf("mw prompt run: no work tracker to read the facts from")
	}
	facts, err := r.facts(ctx)
	if err != nil {
		return fmt.Errorf("mw prompt run: reading the facts: %w", err)
	}

	var b strings.Builder
	b.WriteString("PROMPT /" + prompt.Name)
	for _, arg := range given {
		b.WriteString(" --" + arg.Flag + " " + quoteIfSpaced(arg.Value))
	}
	b.WriteString("\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n" + PromptFactsHeading + "\n")
	for _, section := range facts {
		b.WriteString(section.String())
	}
	if r.Out != nil {
		_, err = io.WriteString(r.Out, b.String())
	}
	return err
}

// promptSection is one block of facts: a heading and its lines, each as
// printed under it.
type promptSection struct {
	heading string
	lines   []string
}

func (s promptSection) String() string {
	var b strings.Builder
	if len(s.lines) == 0 {
		return s.heading + "\n  none\n\n"
	}
	b.WriteString(fmt.Sprintf("%s (%d)\n", s.heading, len(s.lines)))
	for _, line := range s.lines {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

// facts reads the five sections. A bead is always printed as its id.
func (r PromptRun) facts(ctx context.Context) ([]promptSection, error) {
	waiting, err := r.Tracker.ReadyWithLabel(ctx, LabelHitl)
	if err != nil {
		return nil, fmt.Errorf("reading what waits for the Governor: %w", err)
	}
	sort.SliceStable(waiting, func(i, j int) bool { return waiting[i].Priority < waiting[j].Priority })
	notes, err := r.Notes.NotesWithPrefix(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("reading the notes: %w", err)
	}
	doc, err := PosternView{Tracker: r.Tracker, Notes: r.Notes, Host: r.Host, Now: r.Now, Err: r.Err}.Build(ctx)
	if err != nil {
		return nil, err
	}

	governor := promptSection{heading: WaitingHeading}
	listed := map[string]bool{}
	for _, d := range waiting {
		listed[d.Story.ID] = true
		governor.lines = append(governor.lines, "  "+d.Story.ID+"  "+oneLine(d.Story.Title))
	}
	landed := promptSection{heading: PromptLandedHeading}
	demos := promptSection{heading: PromptDemosHeading}
	for _, need := range doc.Needs {
		switch need.Kind {
		case PosternNeedVerify:
			// A hitl:verify bead waits for the Governor, listed above; only a
			// landing is listed as landed.
			if listed[need.Bead] {
				continue
			}
			landed.lines = append(landed.lines, "  "+need.Bead+"  "+oneLine(need.Title))
			landed.lines = append(landed.lines, indentLines(need.Text, "    ")...)
		case PosternNeedDemo:
			demos.lines = append(demos.lines, "  "+need.Bead+"  "+oneLine(need.Title))
		}
	}
	return []promptSection{governor, landed, openCards(notes), demos, waitingHands(notes)}, nil
}

// openCards are the questions still marked open, from the postern.question.*
// notes, by bead.
func openCards(notes map[string]string) promptSection {
	section := promptSection{heading: PromptCardsHeading}
	for _, key := range sortedKeys(notes) {
		bead, ok := strings.CutPrefix(key, PosternQuestionKey(""))
		if !ok || strings.TrimSpace(notes[key]) == "" {
			continue
		}
		text := "(the question is on the bead)"
		if facts, ok := questionFromNote(notes[key]); ok {
			text = oneLine(facts.Text)
		}
		section.lines = append(section.lines, "  "+bead+"  "+text)
	}
	return section
}

// waitingHands are the hands steps no run has been recorded for, from the
// hands.<bead> notes; a step a newer one superseded cannot be approved, so it
// is left out.
func waitingHands(notes map[string]string) promptSection {
	section := promptSection{heading: PromptHandsHeading}
	for _, key := range sortedKeys(notes) {
		bead, ok := strings.CutPrefix(key, HandsStepsKey(""))
		if !ok {
			continue
		}
		// hands.ran.*, hands.superseded.* and hands.approval.* are not step
		// lists: a step list is a JSON array, which they are not.
		if records, err := parseHandsSteps(notes[key]); err != nil || len(records) == 0 {
			continue
		}
		for _, step := range viewHandsSteps(bead, notes) {
			if step.Ran != nil || step.SupersededBy != "" {
				continue
			}
			section.lines = append(section.lines, fmt.Sprintf("  %s  step %s on %s as %s: %s",
				bead, step.ID, step.Host, step.As, oneLine(step.Run)))
		}
	}
	return section
}

// parsePromptArgs reads a call's tokens against p's signature. A bool option
// may stand alone: --flag alone is --flag true. When the signature has a
// free-text option, every word no flag took is its value, in order, joined by
// single spaces; otherwise such a word is refused.
func parsePromptArgs(p domain.Prompt, tokens []string) ([]domain.PromptArg, error) {
	options, err := p.Options()
	if err != nil {
		return nil, fmt.Errorf("mw prompt run: the saved prompt /%s has a signature that does not read: %w", p.Name, err)
	}
	isBool := map[string]bool{}
	textFlag := ""
	for _, o := range options {
		isBool[o.Flag] = o.Type == domain.PromptTypeBool
		if o.Type == domain.PromptTypeText {
			textFlag = o.Flag
		}
	}
	var given []domain.PromptArg
	var words []string
	for i := 0; i < len(tokens); i++ {
		flag, ok := strings.CutPrefix(tokens[i], "--")
		if (!ok || flag == "") && textFlag != "" {
			words = append(words, tokens[i])
			continue
		}
		if !ok || flag == "" {
			return nil, fmt.Errorf("mw prompt run: /%s: %q is not --<flag> <value>; its signature is %s", p.Name, tokens[i], p.SignatureText())
		}
		if name, value, hasValue := strings.Cut(flag, "="); hasValue {
			given = append(given, domain.PromptArg{Flag: name, Value: value})
			continue
		}
		switch {
		case i+1 < len(tokens) && !(isBool[flag] && strings.HasPrefix(tokens[i+1], "--")):
			i++
			given = append(given, domain.PromptArg{Flag: flag, Value: tokens[i]})
		case isBool[flag]:
			given = append(given, domain.PromptArg{Flag: flag, Value: "true"})
		default:
			return nil, fmt.Errorf("mw prompt run: /%s: --%s needs a value; its signature is %s", p.Name, flag, p.SignatureText())
		}
	}
	if len(words) > 0 {
		given = append(given, domain.PromptArg{Flag: textFlag, Value: strings.Join(words, " ")})
	}
	return given, nil
}

func quoteIfSpaced(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\n\"'") {
		return strconv.Quote(value)
	}
	return value
}

// indentLines is text's lines, each behind pad, the blank ones left out so a
// section stays one block.
func indentLines(text, pad string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, pad+line)
		}
	}
	return lines
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
