package application

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// RigMemoryMarker heads the section at the end of a Builder's closing comment
// that proposes what the rig's memory should keep.
const RigMemoryMarker = "For the rig memory:"

// ProposalVerb is which of the proposal forms a line of that section is.
type ProposalVerb string

const (
	ProposeGotcha    ProposalVerb = "gotcha"
	ProposeDecision  ProposalVerb = "decision"
	ProposeSupersede ProposalVerb = "supersede"
	ProposeRetire    ProposalVerb = "retire"
	ProposeRecheck   ProposalVerb = "recheck"
	// ProposeNothing is the single word 'nothing': the Builder proposes no fact.
	ProposeNothing ProposalVerb = "nothing"
)

// FactProposal is one proposal a Builder made, in the form mw memory takes. A
// gotcha or a decision has a Subject; a supersede, a retire and a recheck have
// the Slug of the fact they act on. Sentence is the fact's sentence, the
// reason of a retire or what is in doubt for a recheck. NewSlug is the file
// name a Builder chose for the new fact of a gotcha, a decision or a supersede,
// in the command form; empty is mw memory's default.
type FactProposal struct {
	Verb     ProposalVerb
	Subject  string
	Slug     string
	NewSlug  string
	Sentence string
}

// ProposalLine is one line of the section: the proposal it parsed to, or, when
// it could not be parsed, the line as written in Malformed.
type ProposalLine struct {
	Proposal  FactProposal
	Malformed string
}

// FactProposals is what a closing comment's 'For the rig memory:' section says.
// Found is false when the comment has no such section.
type FactProposals struct {
	Found bool
	Lines []ProposalLine
}

var (
	// proposalSubjectForm is 'gotcha [subject]: sentence', and the decision's.
	proposalSubjectForm = regexp.MustCompile(`^(gotcha|decision) \[([^\]]*)\]:(.*)$`)
	// proposalSlugForm is 'supersede <slug>: sentence', and retire's and recheck's.
	proposalSlugForm = regexp.MustCompile(`^(supersede|retire|recheck) ([a-z0-9][a-z0-9-]*):(.*)$`)
)

// ParseFactProposals reads the 'For the rig memory:' section of one closing
// comment: the lines from the heading to the end, or to a 'For common errors:'
// heading after it, the text after the heading's own colon counting as the
// first. Blank lines are skipped; a bullet or a number in front of a line is
// ignored. A line is either a ready mw memory add, supersede, retire or recheck
// command, which is how a Builder is asked to write it (mw-gq6.771), or the
// older typed form 'gotcha [subject]: sentence'. A line that is none of these
// is returned as malformed, never dropped. When the comment holds the
// heading more than once, the last is the section.
func ParseFactProposals(comment string) FactProposals {
	lines := strings.Split(strings.ReplaceAll(comment, "\r\n", "\n"), "\n")
	start, first := -1, ""
	for i, line := range lines {
		if at := strings.Index(strings.ToLower(line), strings.ToLower(RigMemoryMarker)); at >= 0 {
			start, first = i, line[at+len(RigMemoryMarker):]
		}
	}
	if start < 0 {
		return FactProposals{}
	}
	found := FactProposals{Found: true}
	for _, raw := range append([]string{first}, lines[start+1:]...) {
		// A bug's 'For common errors:' section, written after this one, is not
		// a proposal of it (mw-gq6.420).
		if strings.Contains(strings.ToLower(raw), strings.ToLower(CommonErrorsMarker)) {
			break
		}
		text := strings.TrimSpace(domain.TrimListMarker(raw))
		if text == "" {
			continue
		}
		if proposal, ok := parseFactProposal(text); ok {
			found.Lines = append(found.Lines, ProposalLine{Proposal: proposal})
		} else {
			found.Lines = append(found.Lines, ProposalLine{Malformed: strings.TrimSpace(raw)})
		}
	}
	return found
}

func parseFactProposal(text string) (FactProposal, bool) {
	if strings.EqualFold(strings.Trim(text, " .*_`"), string(ProposeNothing)) {
		return FactProposal{Verb: ProposeNothing}, true
	}
	if strings.HasPrefix(text, memoryCommandPrefix) {
		return parseMemoryCommand(text)
	}
	if m := proposalSubjectForm.FindStringSubmatch(text); m != nil {
		subject, sentence := strings.TrimSpace(m[2]), strings.Join(strings.Fields(m[3]), " ")
		if subject == "" || sentence == "" {
			return FactProposal{}, false
		}
		return FactProposal{Verb: ProposalVerb(m[1]), Subject: subject, Sentence: sentence}, true
	}
	if m := proposalSlugForm.FindStringSubmatch(text); m != nil {
		sentence := strings.Join(strings.Fields(m[3]), " ")
		if sentence == "" {
			return FactProposal{}, false
		}
		return FactProposal{Verb: ProposalVerb(m[1]), Slug: m[2], Sentence: sentence}, true
	}
	return FactProposal{}, false
}

// memoryCommandPrefix opens a proposal written as the mw memory command that
// places it.
const memoryCommandPrefix = "mw memory "

var proposalSlugWord = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// memoryCommandShape is what each mw memory verb a proposal may use takes: how
// many positional words (the rig first), which flags, and the flag that holds
// the sentence of a retire or a recheck.
var memoryCommandShape = map[string]struct {
	positionals int
	flags       []string
}{
	"add":       {2, []string{"kind", "subject", "source", "slug"}},
	"supersede": {3, []string{"source", "slug"}},
	"retire":    {2, []string{"reason"}},
	"recheck":   {2, []string{"why"}},
}

// parseMemoryCommand reads 'mw memory add|supersede|retire|recheck <rig> ...'
// as the proposal it carries out. The rig and the --source are not kept: the
// landing names its own rig and the story is the source. A command with a word
// or a flag it does not know, a missing required part or an unclosed quote is
// not a proposal.
func parseMemoryCommand(text string) (FactProposal, bool) {
	words, ok := shellWords(strings.TrimPrefix(text, memoryCommandPrefix))
	if !ok || len(words) == 0 {
		return FactProposal{}, false
	}
	shape, known := memoryCommandShape[words[0]]
	if !known {
		return FactProposal{}, false
	}
	flags, positionals := map[string]string{}, []string{}
	for rest := words[1:]; len(rest) > 0; rest = rest[1:] {
		word := rest[0]
		if !strings.HasPrefix(word, "--") {
			positionals = append(positionals, word)
			continue
		}
		name, value, joined := strings.Cut(strings.TrimPrefix(word, "--"), "=")
		if !contains(shape.flags, name) {
			return FactProposal{}, false
		}
		if !joined {
			if len(rest) < 2 {
				return FactProposal{}, false
			}
			value, rest = rest[1], rest[1:]
		}
		flags[name] = value
	}
	if len(positionals) != shape.positionals {
		return FactProposal{}, false
	}
	oneLine := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	newSlug := flags["slug"]
	if newSlug != "" && !proposalSlugWord.MatchString(newSlug) {
		return FactProposal{}, false
	}
	switch words[0] {
	case "add":
		kind, subject, sentence := flags["kind"], oneLine(flags["subject"]), oneLine(positionals[1])
		if (kind != string(ProposeGotcha) && kind != string(ProposeDecision)) || subject == "" || sentence == "" {
			return FactProposal{}, false
		}
		return FactProposal{Verb: ProposalVerb(kind), Subject: subject, NewSlug: newSlug, Sentence: sentence}, true
	case "supersede":
		sentence := oneLine(positionals[2])
		if !proposalSlugWord.MatchString(positionals[1]) || sentence == "" {
			return FactProposal{}, false
		}
		return FactProposal{Verb: ProposeSupersede, Slug: positionals[1], NewSlug: newSlug, Sentence: sentence}, true
	}
	verb, sentence := ProposeRetire, oneLine(flags["reason"])
	if words[0] == "recheck" {
		verb, sentence = ProposeRecheck, oneLine(flags["why"])
	}
	if !proposalSlugWord.MatchString(positionals[1]) || sentence == "" {
		return FactProposal{}, false
	}
	return FactProposal{Verb: verb, Slug: positionals[1], Sentence: sentence}, true
}

// shellWords splits a command line as a POSIX shell would for quoting alone:
// single quotes keep everything, double quotes keep everything but \" \\ $ and
// backtick escapes, a backslash outside quotes escapes the next character. It
// is false for an unclosed quote or a trailing backslash. Nothing is expanded.
func shellWords(line string) ([]string, bool) {
	var words []string
	var word strings.Builder
	inWord, quote := false, rune(0)
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(runes) && strings.ContainsRune("\"\\$`", runes[i+1]):
				i++
				word.WriteRune(runes[i])
			default:
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '\\':
			if i+1 == len(runes) {
				return nil, false
			}
			i++
			word.WriteRune(runes[i])
			inWord = true
		case r == ' ' || r == '\t':
			if inWord {
				words, inWord = append(words, word.String()), false
				word.Reset()
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, true
}

// MemoryCommand is the mw memory line that carries the proposal out for the
// rig, ready to paste. The source of a new fact is the story that proposed it.
func (p FactProposal) MemoryCommand(rig, story string) string {
	switch p.Verb {
	case ProposeGotcha, ProposeDecision:
		return fmt.Sprintf("mw memory add %s --kind %s --subject %s%s --source %s %s",
			rig, p.Verb, shellQuoted(p.Subject), p.slugFlag(), story, shellQuoted(p.Sentence))
	case ProposeSupersede:
		return fmt.Sprintf("mw memory supersede %s %s%s --source %s %s", rig, p.Slug, p.slugFlag(), story, shellQuoted(p.Sentence))
	case ProposeRetire:
		return fmt.Sprintf("mw memory retire %s %s --reason %s", rig, p.Slug, shellQuoted(p.Sentence))
	case ProposeRecheck:
		return fmt.Sprintf("mw memory recheck %s %s --why %s", rig, p.Slug, shellQuoted(p.Sentence))
	}
	return string(ProposeNothing)
}

// slugFlag is ' --slug <name>' when the Builder chose the new fact's file name.
func (p FactProposal) slugFlag() string {
	if p.NewSlug == "" {
		return ""
	}
	return " --slug " + p.NewSlug
}

// FlatFileLine is what the Mayor does by hand for a rig whose memory is one
// flat file, which mw memory refuses (mw-gq6.558): the file, then the fact as
// the file holds it, then the story that proposed it.
func (p FactProposal) FlatFileLine(flatFile, story string) string {
	switch p.Verb {
	case ProposeGotcha, ProposeDecision:
		return fmt.Sprintf("place by hand in %s: %s [%s]: %s (%s)", flatFile, p.Verb, p.Subject, p.Sentence, story)
	case ProposeSupersede, ProposeRetire, ProposeRecheck:
		return fmt.Sprintf("edit by hand in %s: %s %s: %s (%s)", flatFile, p.Verb, p.Slug, p.Sentence, story)
	}
	return string(ProposeNothing)
}

// RigMemoryBlock is what a Landed mail says of the closing comment's proposals:
// 'For the rig memory (<rig>):' and a line each, an mw memory command to paste,
// 'nothing', or 'MALFORMED: <line>'. For a flat-file rig, whose flatFile is
// named, the line says to place the fact by hand in that file instead, as mw
// memory has no folder to put it in. It is empty when the comment has no section.
func (f FactProposals) RigMemoryBlock(rig, story, flatFile string) string {
	if !f.Found {
		return ""
	}
	out := []string{"For the rig memory (" + rig + "):"}
	for _, line := range f.Lines {
		if line.Malformed != "" {
			out = append(out, "MALFORMED: "+line.Malformed)
			continue
		}
		if flatFile != "" {
			out = append(out, line.Proposal.FlatFileLine(flatFile, story))
			continue
		}
		out = append(out, line.Proposal.MemoryCommand(rig, story))
	}
	if len(f.Lines) == 0 {
		out = append(out, "MALFORMED: the section is empty")
	}
	return strings.Join(out, "\n")
}

// shellQuoted is text as one word for a shell: in single quotes, a quote in it
// closed, escaped and opened again.
func shellQuoted(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}
