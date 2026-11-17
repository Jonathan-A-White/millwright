package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// The paste rules: what the Mayor's seat file says a message to the Governor
// must and must not hold, kept in the tool so no seat has to remember them.
// mw postern send, mw talk say and mw hands add run the table over what they
// are about to send and refuse with the rule's name and reason; --allow <rule>
// with --allow-why overrides a refusal and writes the reason on a bead.
//
// PasteRules is the one table. The help of each command is printed from it,
// so the help and the check cannot differ.

// PasteSurface is the command a paste check runs for; it is also the prefix of
// what the command says.
type PasteSurface string

const (
	PasteSurfaceSend  PasteSurface = "mw postern send"
	PasteSurfaceSay   PasteSurface = "mw talk say"
	PasteSurfaceHands PasteSurface = "mw hands add"
)

// PasteRule is one row of the table.
type PasteRule struct {
	// Name is what --allow takes and what a refusal opens with.
	Name string
	// Reason is why the rule is there.
	Reason string
	// Surfaces are the commands that run the rule.
	Surfaces []PasteSurface

	check func(in pasteInput) pasteFinding
}

// pasteFinding is what one rule found: a refusal, a warning, or nothing.
type pasteFinding struct {
	refuse string // the detail of a refusal, "" for none
	warn   string // a warning line (without the surface), "" for none
}

var (
	pasteEverywhere = []PasteSurface{PasteSurfaceSend, PasteSurfaceSay, PasteSurfaceHands}
	pasteWritten    = []PasteSurface{PasteSurfaceSend, PasteSurfaceSay}
	pasteSendOnly   = []PasteSurface{PasteSurfaceSend}
)

// PasteRules is the table of paste rules, in the order they are checked.
var PasteRules = []PasteRule{
	{
		Name:     "no-base64",
		Reason:   "Bitdefender quarantined his PowerShell history over 'echo <base64> | base64 -d' (mw-gq6.1431, 2026-11-17): no base64 and no -EncodedCommand in anything he pastes",
		Surfaces: pasteEverywhere,
		check:    checkNoBase64,
	},
	{
		Name:     "windows-paste-one-line",
		Reason:   "a Windows paste is ONE line joined by ';': a multi-line paste ran its first line and hung at '>>' (procedures.md rule 31)",
		Surfaces: pasteEverywhere,
		check:    checkWindowsOneLine,
	},
	{
		Name:     "code-in-one-fence",
		Reason:   "any code, even one line, is in ONE fenced code block (it has a Copy button): not two blocks, and no command inline in backticks",
		Surfaces: pasteWritten,
		check:    checkCodeInOneFence,
	},
	{
		Name:     "windows-paste-names-powershell",
		Reason:   "a Windows paste says to open a NEW Windows PowerShell (the words, or a 'PS C:\\' prompt in the block), so it is not pasted into a shell already in use",
		Surfaces: pasteWritten,
		check:    checkNamesPowerShell,
	},
	{
		Name:     "words",
		Reason:   "a phone shows about 80 words: over 80 warns and sends, over 120 refuses (a card's question 60 and 120); the words of a fenced block do not count",
		Surfaces: pasteSendOnly,
		check:    checkWords,
	},
}

// PasteRuleNames lists the names of the table, in order.
func PasteRuleNames() []string {
	names := make([]string, len(PasteRules))
	for i, r := range PasteRules {
		names[i] = r.Name
	}
	return names
}

// PasteRulesHelp is the paragraph of a command's --help that lists the rules
// the command runs, built from the table.
func PasteRulesHelp(surface PasteSurface) string {
	var b strings.Builder
	b.WriteString("Paste rules, run over the text before anything is sent (refused with the rule's name and reason):\n")
	for _, r := range PasteRules {
		if r.RunsOn(surface) {
			fmt.Fprintf(&b, "  %s: %s\n", r.Name, r.Reason)
		}
	}
	b.WriteString("--allow <rule> (repeatable) sends past a refusal, with --allow-why \"<reason>\" (an empty reason is refused) and the bead the reason is written on")
	if surface == PasteSurfaceHands {
		b.WriteString(" (the bead given here)")
	} else if surface == PasteSurfaceSay {
		b.WriteString(" (--bead)")
	} else {
		b.WriteString(" (--bead or --bead-channel)")
	}
	b.WriteString(": one comment 'ALLOWED <rule>: <reason>' for each rule it let through. An unknown rule is refused, naming the table's.\n")
	return b.String()
}

// RunsOn reports whether the command runs the rule.
func (r PasteRule) RunsOn(surface PasteSurface) bool {
	for _, s := range r.Surfaces {
		if s == surface {
			return true
		}
	}
	return false
}

// PasteCheck is a text to run the table over.
type PasteCheck struct {
	Surface PasteSurface
	// Text is what is sent; Class "decision-needed" makes it a card's question.
	Text  string
	Class string
	// Also is more text the code rules read but the word count does not: a
	// card's options and recommendation, a hands step's way back.
	Also []string
	// Long is --long, the alias of --allow words: it needs no reason.
	Long bool
	// Allow, Why and Bead are --allow, --allow-why and the bead the reason
	// is written on.
	Allow []string
	Why   string
	Bead  string
}

// PasteVerdict is what a check let through.
type PasteVerdict struct {
	// Warnings are lines to print before sending, each already opening with
	// the command's name.
	Warnings []string
	// Allowed are the rules that would have refused and were allowed, with
	// the reason and bead to write them on.
	Allowed []string
	Why     string
	Bead    string
}

// PasteCommenter writes a comment on a bead.
type PasteCommenter interface {
	CommentOnStory(ctx context.Context, id, text string) error
}

// Record writes 'ALLOWED <rule>: <reason>' on the bead, one comment for each
// rule a refusal was overridden for.
func (v PasteVerdict) Record(ctx context.Context, tracker PasteCommenter) error {
	for _, rule := range v.Allowed {
		if err := tracker.CommentOnStory(ctx, v.Bead, fmt.Sprintf("ALLOWED %s: %s", rule, strings.TrimSpace(v.Why))); err != nil {
			return fmt.Errorf("%s was sent past the rule %s, but writing the reason on %s failed: %w", "the message", rule, v.Bead, err)
		}
	}
	return nil
}

// Run checks the text. It returns an error naming every rule that refuses and
// was not allowed, and the warnings of those that only warn.
func (c PasteCheck) Run() (PasteVerdict, error) {
	allowed := map[string]bool{}
	for _, name := range c.Allow {
		name = strings.TrimSpace(name)
		if findPasteRule(name) == nil {
			return PasteVerdict{}, fmt.Errorf("%s: --allow %q names no paste rule; the rules are %s", c.Surface, name, strings.Join(PasteRuleNames(), ", "))
		}
		allowed[name] = true
	}
	if len(allowed) > 0 {
		if strings.TrimSpace(c.Why) == "" {
			return PasteVerdict{}, fmt.Errorf("%s: --allow needs --allow-why \"<reason>\": say why the rule does not hold here", c.Surface)
		}
		if strings.TrimSpace(c.Bead) == "" {
			return PasteVerdict{}, fmt.Errorf("%s: --allow is refused: name the bead the reason is written on (%s)", c.Surface, c.beadFlag())
		}
	}
	if c.Long {
		allowed["words"] = true
	}
	in := newPasteInput(c)
	verdict := PasteVerdict{Why: c.Why, Bead: c.Bead}
	var refusals []string
	for _, rule := range PasteRules {
		if !rule.RunsOn(c.Surface) {
			continue
		}
		found := rule.check(in)
		if found.refuse == "" && found.warn == "" {
			continue
		}
		if allowed[rule.Name] {
			if found.refuse != "" && !(c.Long && rule.Name == "words" && !pasteNames(c.Allow, "words")) {
				verdict.Allowed = append(verdict.Allowed, rule.Name)
			}
			continue
		}
		if found.refuse != "" {
			refusals = append(refusals, fmt.Sprintf("%s: refused by the paste rule %s (%s): %s; to send it anyway give --allow %s --allow-why \"<reason>\" and %s", c.Surface, rule.Name, rule.Reason, found.refuse, rule.Name, c.beadFlag()))
			continue
		}
		verdict.Warnings = append(verdict.Warnings, string(c.Surface)+": "+found.warn)
	}
	if len(refusals) > 0 {
		return PasteVerdict{}, fmt.Errorf("%s", strings.Join(refusals, "\n"))
	}
	return verdict, nil
}

func (c PasteCheck) beadFlag() string {
	switch c.Surface {
	case PasteSurfaceHands:
		return "the bead of the step is where it is written"
	case PasteSurfaceSay:
		return "name the bead with --bead"
	}
	return "name the bead with --bead or --bead-channel"
}

func findPasteRule(name string) *PasteRule {
	for i := range PasteRules {
		if PasteRules[i].Name == name {
			return &PasteRules[i]
		}
	}
	return nil
}

func pasteNames(list []string, want string) bool {
	for _, s := range list {
		if strings.TrimSpace(s) == want {
			return true
		}
	}
	return false
}

// pasteInput is the text parsed once for every rule.
type pasteInput struct {
	surface PasteSurface
	text    string     // Text, for the word count
	all     string     // Text and Also, for the code rules
	blocks  [][]string // the lines of each fenced block; a hands step with no fence is one block
	outside []string   // the lines outside every fence
	class   string
}

func newPasteInput(c PasteCheck) pasteInput {
	all := strings.Join(append([]string{c.Text}, c.Also...), "\n")
	in := pasteInput{surface: c.Surface, text: c.Text, all: all, class: c.Class}
	in.blocks, in.outside = splitFences(all)
	if c.Surface == PasteSurfaceHands && len(in.blocks) == 0 {
		in.blocks = [][]string{strings.Split(all, "\n")}
		in.outside = nil
	}
	return in
}

// splitFences cuts text into the lines of each fenced block (the fence lines
// left out; a block never closed runs to the end) and the lines outside.
func splitFences(text string) (blocks [][]string, outside []string) {
	var open []string
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inFence {
				blocks = append(blocks, open)
				open = nil
			}
			inFence = !inFence
			continue
		}
		if inFence {
			open = append(open, line)
		} else {
			outside = append(outside, line)
		}
	}
	if inFence {
		blocks = append(blocks, open)
	}
	return blocks, outside
}

func nonBlankLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func checkNoBase64(in pasteInput) pasteFinding {
	lower := strings.ToLower(in.all)
	for _, word := range []string{"base64", "-encodedcommand"} {
		if strings.Contains(lower, word) {
			return pasteFinding{refuse: fmt.Sprintf("the text holds %q", word)}
		}
	}
	return pasteFinding{}
}

var windowsPaste = regexp.MustCompile(`(?i:wsl\.exe|powershell|-ScheduledTask)|\b(?:Set|Get)-[A-Z]`)

const psPrompt = `PS C:\`

// windowsBlock reports whether a fenced block is a Windows paste: it opens at
// a 'PS C:\' prompt or holds wsl.exe, powershell, or a scheduled-task, Set- or
// Get- cmdlet.
func windowsBlock(lines []string) bool {
	return startsAtPrompt(lines) || windowsPaste.MatchString(strings.Join(lines, "\n"))
}

func startsAtPrompt(lines []string) bool {
	first := nonBlankLines(lines)
	return len(first) > 0 && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(first[0])), strings.ToUpper(psPrompt))
}

func checkWindowsOneLine(in pasteInput) pasteFinding {
	for _, block := range in.blocks {
		if !windowsBlock(block) {
			continue
		}
		if n := len(nonBlankLines(block)); n > 1 {
			return pasteFinding{refuse: fmt.Sprintf("a Windows paste runs to %d lines: join them with ';' into one", n)}
		}
	}
	return pasteFinding{}
}

var inlineCommands = map[string]bool{"mw": true, "bd": true, "git": true, "wsl": true, "sudo": true, "systemctl": true, "npm": true, "go": true, "cd": true, "powershell": true}

var inlineSpan = regexp.MustCompile("`([^`\n]+)`")

func checkCodeInOneFence(in pasteInput) pasteFinding {
	if len(in.blocks) > 1 {
		return pasteFinding{refuse: fmt.Sprintf("%d fenced blocks: put all the code in one", len(in.blocks))}
	}
	for _, line := range in.outside {
		for _, m := range inlineSpan.FindAllStringSubmatch(line, -1) {
			fields := strings.Fields(m[1])
			if len(fields) >= 2 && inlineCommands[fields[0]] {
				return pasteFinding{refuse: fmt.Sprintf("the command `%s` is inline: put it in the one fenced block", m[1])}
			}
		}
	}
	return pasteFinding{}
}

func checkNamesPowerShell(in pasteInput) pasteFinding {
	if strings.Contains(strings.ToLower(in.all), "new windows powershell") {
		return pasteFinding{}
	}
	any := false
	for _, block := range in.blocks {
		if !windowsBlock(block) {
			continue
		}
		any = true
		if startsAtPrompt(block) {
			return pasteFinding{}
		}
	}
	if any {
		return pasteFinding{refuse: "a Windows paste that does not say 'new Windows PowerShell' and shows no 'PS C:\\' prompt"}
	}
	return pasteFinding{}
}

func checkWords(in pasteInput) pasteFinding {
	warn, refuse := lengthFindings(in.text, in.class)
	return pasteFinding{warn: warn, refuse: refuse}
}
