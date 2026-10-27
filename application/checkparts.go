package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CheckPartSlotWait is the most mw check --part N waits for the rig's merge
// slot before it ends with a line saying who holds it: under the 590 seconds a
// Builder's foreground tool call is given, so that the call ends in words rather
// than in the tool's own silent timeout (mw-6zgnz5y). It is nine minutes, since
// a batch landing holds the slot 10 to 40 minutes and a shorter wait handed the
// Builder back to a landing it had not caused (mw-gq6.814). Worktree checks
// share the slot, so only a landing in progress makes a part wait.
//
// A whole check with no --part on a rig whose gate is not split waits the same
// time for the same slot, for the same reason, and ends in the same kind of line
// (mw-gq6.817). Its wait for a calm host before the tests is bounded by the same
// time, in a line of its own (mw-gq6.819).
const CheckPartSlotWait = 9 * time.Minute

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

// CheckPartHeldFileName is the name of the record of the waits of part n of a
// story's attempt on the merge slot, in the story's run directory: one file for
// the part and the attempt, written again by each wait, so that a part handed
// back on the slot twice in one attempt is one wait on record (mw-gq6.814).
func CheckPartHeldFileName(attempt, n int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-part-%d.slot", attempt, n)
}

// slotFreeRunners are the commands a gate part may begin each of its steps with
// and still run without the merge slot: tools that read and write the story's
// worktree alone, at the branch's tip.
var slotFreeRunners = []string{
	"npm ci", "npm install", "npm test", "npm run test", "npm run typecheck", "npm run lint", "npm run build",
	"npx vitest", "npx tsc", "npx eslint", "go build", "go vet", "go test", "cd",
}

// PartNeedsSlot says whether a part of a split gate takes the rig's merge slot
// (mw-gq6.814). The slot is there so that a close-out's gate and a check's never
// run on the merge target at once; a part runs in the story's own worktree, and
// one that only installs, type-checks, lints, builds or tests there does not touch
// the target, so it takes none and a landing holding the slot does not stop it.
// Any part whose command this does not know to be so (a step that is not one of
// slotFreeRunners, or a pipe, redirect, substitution or ';') keeps the slot. Of
// the lampas gate, so, only a part running the screenshot gate needs it.
func PartNeedsSlot(part GatePart) bool {
	command := strings.TrimSpace(part.Command)
	if command == "" || strings.ContainsAny(command, ";|<>`$(){}\n") {
		return true
	}
	for _, step := range strings.Split(command, "&&") {
		fields := strings.Fields(step)
		// A leading VAR=value belongs to the command that follows.
		for len(fields) > 0 && strings.Contains(fields[0], "=") && !strings.HasPrefix(fields[0], "-") {
			fields = fields[1:]
		}
		if !slotFreeStep(strings.Join(fields, " ")) {
			return true
		}
	}
	return false
}

func slotFreeStep(step string) bool {
	for _, runner := range slotFreeRunners {
		if step == runner || strings.HasPrefix(step, runner+" ") {
			return true
		}
	}
	return false
}

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
// rig's merge slot around the part, shared with the other stories' checks, as a
// whole check does, unless the part only runs tools in the worktree
// (PartNeedsSlot); a landing holds it alone, and a part that waits on one for
// CheckPartSlotWait ends with a line naming the holder and the command to run
// again.
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

	if k.Slot != nil && PartNeedsSlot(part) {
		holding, err := k.takeSlotWithin(ctx, c, slotTry{
			label:  label,
			noun:   "part",
			record: CheckPartHeldFileName(report.Attempt, k.Part),
			again:  fmt.Sprintf("run the same part again with `mw check %s --part %d`", c.id, k.Part),
		})
		if err != nil {
			return report, fmt.Errorf("checking %s: %w", c.id, err)
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

func (k Check) partSlotWait() time.Duration {
	if k.PartSlotWait > 0 {
		return k.PartSlotWait
	}
	return CheckPartSlotWait
}

// slotTry is what a check that waits for the merge slot is called in the line it
// ends with: label names it, noun is what a repeat is not a failure of, record is
// the run file that keeps its waits in the attempt (one per kind of try), and
// again is the command to run next.
type slotTry struct {
	label  string
	noun   string
	record string
	again  string
}

// takeSlotWithin takes the rig's slot for a worktree check, waiting no longer
// than partSlotWait: under the 590 seconds a Builder's foreground call is given.
// It ends a wait that ran out in one line (slotRefusal).
func (k Check) takeSlotWithin(ctx context.Context, c *closeOut, try slotTry) (Holding, error) {
	waitCtx, cancel := context.WithTimeout(ctx, k.partSlotWait())
	defer cancel()
	holding, err := takeForCheck(waitCtx, k.Slot, c.rigDir, fmt.Sprintf("%s checking %s", SeatIdentity("", k.Host), c.id))
	if err != nil {
		return nil, k.slotRefusal(ctx, c, try, err)
	}
	return holding, nil
}

// slotRefusal is the one line a check ends with when the merge slot could not be
// taken. A wait that ran out of its budget names who holds the slot, since
// when, and the command to run next, which is the same check again: the wait is
// the landing's, not the check's failure, and a check handed back on the slot
// again in the same attempt is counted as the one wait it is (mw-gq6.814,
// mw-gq6.817). Any other trouble is told as it was.
func (k Check) slotRefusal(ctx context.Context, c *closeOut, try slotTry, err error) error {
	var waited *SlotWaitError
	if ctx.Err() != nil || !errors.As(err, &waited) || !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("the merge slot of %s could not be taken: %w", c.path.Rig, err)
	}
	since := "a moment ago"
	if !waited.Since.IsZero() {
		since = fmt.Sprintf("%s (%s ago)", waited.Since.UTC().Format("15:04:05Z"), time.Since(waited.Since).Round(time.Second))
	}
	earlier := k.noteSlotWait(ctx, c.id, try.record, waited)
	if earlier != "" {
		return fmt.Errorf("%s did not start: after %s the merge slot of %s is still held by %s, since %s, and nothing was run; it was held when this %s was tried at %s as well, which is counted once for this attempt and is not a failure of the %s: %s, and if the slot is held again, comment on the story naming this %s and who holds the slot, and hand back",
			try.label, waited.Waited.Round(time.Second), c.path.Rig, waited.Holder, since, try.noun, earlier, try.noun, try.again, try.noun)
	}
	return fmt.Errorf("%s did not start: after %s the merge slot of %s is still held by %s, since %s, and nothing was run; that is the landing's wait and not a failure of the %s: %s",
		try.label, waited.Waited.Round(time.Second), c.path.Rig, waited.Holder, since, try.noun, try.again)
}

// noteSlotWait keeps what a check waited on in the attempt, once for the record
// named: the first time it was handed back on the slot, who held it and since
// when. It returns when the part was first handed back if it had been
// before, and "" otherwise, or when the record cannot be kept.
func (k Check) noteSlotWait(ctx context.Context, storyID, name string, waited *SlotWaitError) string {
	return k.noteWait(ctx, storyID, name, waited.Holder, waited.Since)
}

// noteWait is noteSlotWait for any wait a check is handed back on: holder says
// what it waited on (who has the slot, the host's load), since when that began.
func (k Check) noteWait(ctx context.Context, storyID, name, holder string, since time.Time) string {
	if k.Records == nil {
		return ""
	}
	if text, err := k.Records.ReadRunFile(ctx, storyID, name); err == nil {
		if first, _, _, ok := slotWaitOf(text); ok {
			return first
		}
	}
	sinceAt := ""
	if !since.IsZero() {
		sinceAt = since.UTC().Format("15:04:05Z")
	}
	record := fmt.Sprintf("%s %s\nholder %s\nsince %s\n", slotWaitWord, time.Now().UTC().Format("15:04:05Z"), holder, sinceAt)
	_, _ = k.Records.PutRunFile(ctx, storyID, name, record)
	return ""
}

// slotWaitWord begins the first line of a record of a part's wait on the slot,
// followed by when the part was first handed back.
const slotWaitWord = "held"

// slotWaitOf reads a record of a part's wait: when it was first handed back, who
// held the slot and since when.
func slotWaitOf(text string) (first, holder, since string, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		word, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch word {
		case slotWaitWord:
			first, ok = rest, rest != ""
		case "holder":
			holder = rest
		case "since":
			since = rest
		}
	}
	return first, holder, since, ok
}

// CheckWholeHeldFileName is the name of the record of the waits of a whole check's
// tests (no --part) on the merge slot, in the story's run directory, one for the
// attempt; CheckSpecsHeldFileName the same for its changed specs (mw-gq6.817).
func CheckWholeHeldFileName(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-whole.slot", attempt)
}

func CheckSpecsHeldFileName(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-specs.slot", attempt)
}

// wholeTry is how a whole check's tests, or its changed specs, are named when they
// are handed back on the merge slot.
func wholeTry(c *closeOut, label, record string) slotTry {
	return slotTry{
		label:  label,
		noun:   "check",
		record: record,
		again:  fmt.Sprintf("run the same check again with `mw check %s`", c.id),
	}
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
			found = append(found, fmt.Sprintf("part %d (%s) has not been run at this commit%s%s", i+1, part.Name, fix, g.heldNote(ctx, i+1)))
		}
	}
	return Checked{
		Command: fmt.Sprintf("the rig's gate in %d parts", len(g.parts)),
		Passed:  len(found) == 0,
		Output:  strings.Join(found, "\n"),
	}, nil
}

// heldNote is what an unrun part's line adds when the part was handed back on the
// merge slot in this attempt: who held it, so that whoever reads the refusal
// knows the part never ran, rather than failed (mw-gq6.814).
func (g recordedGate) heldNote(ctx context.Context, n int) string {
	text, err := g.records.ReadRunFile(ctx, g.storyID, CheckPartHeldFileName(g.attempt, n))
	if err != nil {
		return ""
	}
	first, holder, since, ok := slotWaitOf(text)
	if !ok {
		return ""
	}
	said := fmt.Sprintf(" (it waited on the merge slot held by %s", holder)
	if since != "" {
		said += ", since " + since
	}
	return said + fmt.Sprintf(" when it was tried at %s: that is a wait, not a failure of the part)", first)
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
