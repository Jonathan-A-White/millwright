package application

import (
	"fmt"
	"regexp"
	"strings"
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
// reason of a retire or what is in doubt for a recheck.
type FactProposal struct {
	Verb     ProposalVerb
	Subject  string
	Slug     string
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
	// proposalListMarker is a bullet or a number a line may start with.
	proposalListMarker = regexp.MustCompile(`^(?:[-*+]|\d+[.)])\s+`)
)

// ParseFactProposals reads the 'For the rig memory:' section of one closing
// comment: the lines from the heading to the end, the text after the heading's
// own colon counting as the first. Blank lines are skipped; a bullet or a
// number in front of a line is ignored. A line that is none of the proposal
// forms is returned as malformed, never dropped. When the comment holds the
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
		text := strings.TrimSpace(proposalListMarker.ReplaceAllString(strings.TrimSpace(raw), ""))
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

// MemoryCommand is the mw memory line that carries the proposal out for the
// rig, ready to paste. The source of a new fact is the story that proposed it.
func (p FactProposal) MemoryCommand(rig, story string) string {
	switch p.Verb {
	case ProposeGotcha, ProposeDecision:
		return fmt.Sprintf("mw memory add %s --kind %s --subject %s --source %s %s",
			rig, p.Verb, shellQuoted(p.Subject), story, shellQuoted(p.Sentence))
	case ProposeSupersede:
		return fmt.Sprintf("mw memory supersede %s %s --source %s %s", rig, p.Slug, story, shellQuoted(p.Sentence))
	case ProposeRetire:
		return fmt.Sprintf("mw memory retire %s %s --reason %s", rig, p.Slug, shellQuoted(p.Sentence))
	case ProposeRecheck:
		return fmt.Sprintf("mw memory recheck %s %s --why %s", rig, p.Slug, shellQuoted(p.Sentence))
	}
	return string(ProposeNothing)
}

// RigMemoryBlock is what a Landed mail says of the closing comment's proposals:
// 'For the rig memory (<rig>):' and a line each, an mw memory command to paste,
// 'nothing', or 'MALFORMED: <line>'. It is empty when the comment has no section.
func (f FactProposals) RigMemoryBlock(rig, story string) string {
	if !f.Found {
		return ""
	}
	out := []string{"For the rig memory (" + rig + "):"}
	for _, line := range f.Lines {
		if line.Malformed != "" {
			out = append(out, "MALFORMED: "+line.Malformed)
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
