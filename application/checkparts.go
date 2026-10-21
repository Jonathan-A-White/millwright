package application

import (
	"context"
	"fmt"
	"strings"
)

// A rig's whole gate can outrun the 590 seconds a Builder's foreground tool call
// is given, and waiting on one run in the background is a wait the auto-mode
// classifier refuses (mw-gq6.567). A host can therefore name a rig's gate as
// parts (the `[test_parts.<rig>]` tables of its config file). The Builder runs
// `mw check <story> --part N` once for each part, each call short enough to
// finish in the foreground, and then `mw check <story>` once more: that last
// call runs none of the gate, it reads what each part said at the branch's tip
// and reports the whole gate green, or which part is not.

// GatePart is one part of a rig's gate: a name for the summary, and the command
// line that runs it, read by a shell in the story's worktree.
type GatePart struct {
	Name    string
	Command string
}

// GateParts is the port a split gate is read and run through: the parts a rig's
// gate is named as on this host (none when it is not split), and the running of
// one part's command line, judged as the rig's tests are.
type GateParts interface {
	Parts(rig string) []GatePart
	SuiteRunner
}

// CheckRecords is the part of the vault that keeps what each part of a gate said:
// one file of a story's run for each part.
type CheckRecords interface {
	PutRunFile(ctx context.Context, storyID, name, contents string) (string, error)
	ReadRunFile(ctx context.Context, storyID, name string) (string, error)
}

// CheckPartFileName is the name of the record of part n of a story's attempt, in
// the story's run directory. Like the check's log it begins with check- and ends
// .log, and mw next's trust of a check reads the log alone, never this.
func CheckPartFileName(attempt, n int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-part-%d.log", attempt, n)
}

// The words of a part's record: its first line names the commit the part ran at
// (partNoTip when the worktree held uncommitted work or the tip moved), its
// second what it came to.
const (
	partTipWord = "tip"
	partNoTip   = "-"
	partPassed  = "passed"
	partFailed  = "failed"
)

func (k Check) partsOf(rig string) []GatePart {
	if k.Parts == nil {
		return nil
	}
	return k.Parts.Parts(rig)
}

// splitGate says the check is of a rig whose gate is named as parts and keeps
// their records.
func (k Check) splitGate(rig string) bool {
	return k.Records != nil && len(k.partsOf(rig)) > 0
}

// partLabel is how a part is named in what the check prints.
func partLabel(n, of int, part GatePart) string {
	return fmt.Sprintf("part %d of %d (%s)", n, of, part.Name)
}

// checkPart is mw check <story> --part N: part N of the rig's gate, run alone
// in the story's worktree and its result kept for the last call. It makes none
// of the other checks, which that last call makes, and it waits for no calm
// host and runs nothing twice, so that its time is the part's own. It takes the
// rig's merge slot around the part, as a whole check does.
func (k Check) checkPart(ctx context.Context, report CheckReport, c *closeOut) (CheckReport, error) {
	parts := k.partsOf(c.path.Rig)
	switch {
	case len(parts) == 0 || k.Records == nil:
		return report, fmt.Errorf("checking %s: the gate of the rig %s is not split into parts on %s: run mw check %s without --part, or name the parts under [test_parts.%s] in the config file",
			c.id, c.path.Rig, k.Host, c.id, c.path.Rig)
	case k.Part < 1 || k.Part > len(parts):
		return report, fmt.Errorf("checking %s: the gate of the rig %s has %d parts, so there is no part %d", c.id, c.path.Rig, len(parts), k.Part)
	}
	part := parts[k.Part-1]
	label := partLabel(k.Part, len(parts), part)
	tip := k.cleanTip(ctx, c)

	if k.Slot != nil {
		holding, err := k.Slot.Take(ctx, c.rigDir, fmt.Sprintf("%s checking %s", SeatIdentity("", k.Host), c.id))
		if err != nil {
			return report, fmt.Errorf("checking %s: the merge slot of %s could not be taken: %w", c.id, c.path.Rig, err)
		}
		defer holding.Release(ctx)
	}
	checked, err := k.Parts.RunLine(ctx, c.path.Rig, c.worktree, part.Command)
	if err != nil {
		return report, fmt.Errorf("checking %s: %s could not be run: %w", c.id, label, err)
	}
	// The record says which commit the part ran at only when nothing else was in
	// the worktree and the tip did not move while it ran.
	if after, err := k.Landing.RefHead(ctx, c.rigDir, c.branch); tip != "" && (err != nil || after != tip) {
		tip = ""
	}

	result := partPassed
	if !checked.Passed {
		result = partFailed
	}
	recordedTip := tip
	if recordedTip == "" {
		recordedTip = partNoTip
	}
	record := fmt.Sprintf("%s %s\n%s\n%s\n", partTipWord, recordedTip, result, checked.Tail(CheckLines))
	if _, err := k.Records.PutRunFile(ctx, c.id, CheckPartFileName(report.Attempt, k.Part), record); err != nil {
		return report, fmt.Errorf("checking %s: what %s said could not be kept in the run: %w", c.id, label, err)
	}

	var said strings.Builder
	fmt.Fprintf(&said, "check on %s: %s\n", k.Host, c.id)
	switch {
	case checked.NotRun:
		fmt.Fprintf(&said, "  %s could not be run: `%s` did not start, so this host is missing something the command needs (a toolchain not on its PATH?)\n", label, checked.Command)
	case checked.Passed:
		fmt.Fprintf(&said, "  %s passed", label)
		if tip != "" {
			fmt.Fprintf(&said, " at %s", shortCommit(tip))
		} else {
			said.WriteString(", but the worktree held uncommitted work, so the last call will not count it")
		}
		said.WriteString("\n")
	default:
		fmt.Fprintf(&said, "  %s failed: `%s`\n", label, checked.Command)
	}
	if !checked.Passed {
		if block := FailingTestsBlock(checked.Output); block != "" {
			said.WriteString(indented(block))
		}
		said.WriteString(indented(checked.Tail(CheckLines)))
	}
	if k.Out != nil {
		fmt.Fprint(k.Out, said.String())
	}
	if !checked.Passed {
		return report, fmt.Errorf("checking %s: %s did not pass, so mw next would not land it", c.id, label)
	}
	return report, nil
}

// indented is text with four spaces before each line, ending in a newline.
func indented(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		b.WriteString("    " + line + "\n")
	}
	return b.String()
}

// recordedGate is the rig's gate as the last call of a split gate reads it:
// nothing is run, each part's record is read, and the gate passed when every
// part passed at the commit at the branch's tip. What it says of the parts that
// did not is the refusal's detail, with the command that would fix each.
type recordedGate struct {
	records CheckRecords
	storyID string
	attempt int
	parts   []GatePart
	// tip is the branch's tip when the worktree holds nothing else, empty when
	// it does or the tip could not be read: then no part's run covers it.
	tip string
}

// Run implements Checks.
func (g recordedGate) Run(ctx context.Context, _, _ string) (Checked, error) {
	var found []string
	if g.tip == "" {
		found = append(found, "the worktree holds uncommitted work or its tip could not be read, so no part's run covers it: commit, then run the parts")
	}
	for i, part := range g.parts {
		fix := fmt.Sprintf(": mw check %s --part %d", g.storyID, i+1)
		switch g.stateOf(ctx, i+1) {
		case partPassed:
		case partFailed:
			found = append(found, fmt.Sprintf("part %d (%s) failed at this commit%s", i+1, part.Name, fix))
		default:
			found = append(found, fmt.Sprintf("part %d (%s) has not been run at this commit%s", i+1, part.Name, fix))
		}
	}
	return Checked{
		Command: fmt.Sprintf("the rig's gate in %d parts", len(g.parts)),
		Passed:  len(found) == 0,
		Output:  strings.Join(found, "\n"),
	}, nil
}

// stateOf is what part n's record says of the commit at the tip: partPassed,
// partFailed, or empty when there is no record of a run at the tip.
func (g recordedGate) stateOf(ctx context.Context, n int) string {
	if g.tip == "" {
		return ""
	}
	text, err := g.records.ReadRunFile(ctx, g.storyID, CheckPartFileName(g.attempt, n))
	if err != nil {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return ""
	}
	tip := strings.Fields(lines[0])
	if len(tip) != 2 || tip[0] != partTipWord || tip[1] != g.tip {
		return ""
	}
	switch result := strings.TrimSpace(lines[1]); result {
	case partPassed, partFailed:
		return result
	}
	return ""
}
