package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ReasonGateWait is a story whose Builder's check ran out of its wait limit
// behind another story's gate, the host's load or the merge slot, ran nothing,
// and was told to hand back (mw-gq6.1506): the story's steps are open because
// the gate could not be run, not because the Builder left them. The attempt is
// named tooling, so that it is not counted against the story, and the claim is
// given back so that the story is taken again, its branch continued.
const ReasonGateWait Reason = "gate-wait"

// CheckLimitFileName is the name of the record a check leaves in the story's run
// when a wait ended on its limit and it ran nothing, one for the attempt.
func CheckLimitFileName(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-limit.slot", attempt)
}

// The words that begin the lines of that record.
const (
	limitGaveUpWord = "gave-up"
	limitHolderWord = "holder"
	limitWhyWord    = "why"
)

// limitReachedNoted is limitReached that also keeps what it says for the
// close-out, which then reads open steps left by a check that ran nothing as
// the holder's wait and not as the Builder's.
func (k Check) limitReachedNoted(ctx context.Context, c *closeOut, label string, waited time.Duration, still, holder string) string {
	k.noteLimit(ctx, c, still, holder)
	if step, ok := k.gateStepLeft(ctx, c, label); ok {
		return k.limitReachedLeavingTheGate(label, waited, still, holder, step, c)
	}
	return k.limitReached(label, waited, still, holder)
}

// GateStepTitle is the title of the formula step that asks the Builder for the
// rig's whole build, tests and vet.
const GateStepTitle = "Run the full build, tests and vet"

// gateStepLeft is the open formula step of the rig's gate, when a whole check
// that reached its wait limit ran none of the rig's tests: the Builder can close it,
// the close-out running the gate itself (mw-gq6.1557). False for a part, the
// changed specs' wait, a story with no poured formula or one whose gate step is
// closed or cannot be read, which end as before.
func (k Check) gateStepLeft(ctx context.Context, c *closeOut, label string) (FormulaStep, bool) {
	root := c.detail.Molecule.RootID
	if k.Part != 0 || k.SpecsWorker || root == "" || k.Tracker == nil || strings.Contains(label, "changed specs") {
		return FormulaStep{}, false
	}
	open, err := k.Tracker.OpenSteps(ctx, root)
	if err != nil {
		return FormulaStep{}, false
	}
	for _, step := range open {
		if strings.EqualFold(strings.TrimSpace(step.Title), GateStepTitle) {
			return step, true
		}
	}
	return FormulaStep{}, false
}

// limitReachedLeavingTheGate is limitReached for a whole check whose gate step is
// open: it names the step by its id and says in one line to close it with the
// check's log as the reason, close the steps the Builder's own runs prove, write
// the closing comment and end the turn, because mw next runs the gate itself.
func (k Check) limitReachedLeavingTheGate(label string, waited time.Duration, still, holder string, step FormulaStep, c *closeOut) string {
	return fmt.Sprintf("%s did not start: the wait limit of %s was reached after %s and %s, and nothing was run; that is the holder's (%s) wait and not a failure of the check, so do not run it again: %s",
		label, gateWaited(k.partSlotWait()), gateWaited(waited), still, holder, k.leaveTheGate(step, c))
}

// leaveTheGate is what a Builder whose gate cannot be run for another's wait is
// told to do: close the gate's formula step as left to the close-out, with the
// check's log as the reason, close the steps its own runs prove, write the closing
// comment and end the turn.
func (k Check) leaveTheGate(step FormulaStep, c *closeOut) string {
	vault := "<vault>"
	if k.Vault != "" {
		vault = shellWord(k.Vault)
	}
	result := fmt.Sprintf("runs/%s/%s", c.id, CheckLogFileName(c.attempt()))
	if k.Logs != nil {
		result = k.Logs.RunFile(c.id, CheckLogFileName(c.attempt()))
	}
	return fmt.Sprintf("close the formula step %s (%s) with `bd -C %s close %s --reason \"gate left to the close-out: %s\"`, close the remaining steps your own runs prove, write the closing comment and end the turn: mw next runs the gate",
		step.ID, strings.TrimSpace(step.Title), vault, step.ID, result)
}

// repeatedWaitEnd is how the line of a whole check handed back a second time on
// the same wait in one attempt ends, after "which is counted once for this attempt
// and is not a failure of the check": the command to run again and, with it, the
// words to hand back if the wait is met once more (handBack, which begins with a
// comma). With the gate's formula step open and nothing of the rig's tests run,
// the hand-back is the one of limitReachedLeavingTheGate instead: the step is
// named and closed as left to the close-out, so that the Builder does not end
// with it open (mw-gq6.1562). A part, or a story with no gate step open, ends as
// it always has.
func (k Check) repeatedWaitEnd(ctx context.Context, c *closeOut, label, again, handBack string) string {
	if step, ok := k.gateStepLeft(ctx, c, label); ok {
		return ", so do not run it again: " + k.leaveTheGate(step, c)
	}
	return ": " + again + handBack
}

// noteLimit keeps in the attempt that a wait ended on its limit, who held what it
// waited for, and when. A record that cannot be kept leaves the close-out to read
// the open steps as the Builder's, as before.
func (k Check) noteLimit(ctx context.Context, c *closeOut, still, holder string) {
	if k.Records == nil {
		return
	}
	//mw:realclock the time of day a wait ended on its limit, written to the record the close-out reads
	record := fmt.Sprintf("%s %s\n%s %s\n%s %s\n", limitGaveUpWord, time.Now().UTC().Format("15:04:05Z"), limitHolderWord, holder, limitWhyWord, still)
	_, _ = k.Records.PutRunFile(ctx, c.id, CheckLimitFileName(c.attempt()), record)
}

// forgetLimit takes away what an earlier call of a check left when its
// wait ended on its limit: this call is the newer word.
func (k Check) forgetLimit(ctx context.Context, storyID string, attempt int) {
	if k.Records == nil {
		return
	}
	name := CheckLimitFileName(attempt)
	if text, err := k.Records.ReadRunFile(ctx, storyID, name); err == nil && strings.TrimSpace(text) != "" {
		_, _ = k.Records.PutRunFile(ctx, storyID, name, "")
	}
}

// limitOf reads that record: who held what the check waited for and what it said
// was still held. ok is false for a record with no such wait in it.
func limitOf(text string) (holder, why string, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		word, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch word {
		case limitGaveUpWord:
			ok = true
		case limitHolderWord:
			holder = rest
		case limitWhyWord:
			why = rest
		}
	}
	return holder, why, ok
}

// stepsLostToAWait is stepsLostToTheLock and then the same for a check's wait: the
// open steps a Builder's check left because it reached its wait limit and ran
// nothing (mw-gq6.1506) are a refusal of ReasonGateWait, which gives the story
// back and does not count the attempt. left and the other results are those of
// stepsLostToTheLock when no such wait was kept.
func (n Next) stepsLostToAWait(ctx context.Context, c *closeOut, report *NextReport, root string, open []FormulaStep, previewing bool) (left []FormulaStep, refusal Refusal, refused bool) {
	left, refusal, refused = n.stepsLostToTheLock(ctx, report, root, open, previewing)
	if refused || len(left) == 0 || n.GateRecords == nil {
		return left, refusal, refused
	}
	text, err := n.GateRecords.ReadRunFile(ctx, c.id, CheckLimitFileName(c.attempt()))
	if err != nil {
		// No record, or one that cannot be read: the open steps are the Builder's.
		return left, Refusal{}, false
	}
	holder, why, ok := limitOf(text)
	if !ok {
		return left, Refusal{}, false
	}
	said := fmt.Sprintf("the check of %s reached its wait limit and ran nothing: %s; that is the holder's (%s) wait and not a failure of the story, "+
		"so its %d formula step(s) are open because the gate could not be run, and the attempt is not counted against it", c.id, why, holder, len(left))
	return left, Refusal{ReasonGateWait, said, "Still open:\n" + stepList(left)}, true
}

// givesBackOpen is whether a stop for reason gives the story's claim back, so
// that the next dispatch tick takes it again (mw-gq6.341): a branch whose
// formula was left unfinished, a session cut at its turn cap or whose harness
// crashed, a refused acceptance or closing comment, and a gate that could not be
// run for another's wait. giveBack is whether a workflow-scope stop could name a
// host that can take the story.
func (n Next) givesBackOpen(reason Reason, c *closeOut, giveBack bool) bool {
	switch reason {
	case ReasonOpenSteps, ReasonAcceptanceChanged, ReasonNoClosingComment, ReasonGristExampleUnproved, ReasonTurnCap, ReasonHarnessCrash, ReasonHarnessMissing, ReasonGateWait:
		return true
	case ReasonWorkflowScope:
		return giveBack
	case ReasonNoFindings:
		return IsTesterStory(c.detail)
	}
	return false
}

// handOverOnStop is what a stop that names the attempt tooling does before it is
// commented on: the workflow-scope stop's hand-over, or, for a gate wait, the
// attempt named tooling (mw cause) and nothing more, the story being taken again
// by whichever host is next free.
func (n Next) handOverOnStop(ctx context.Context, c *closeOut, report *NextReport, reason Reason, why string) (said string, repathed bool) {
	if reason == ReasonGateWait {
		return n.nameToolingCause(ctx, c, report, why) + " The story is given back and taken again, its branch continued, when the gate it waited for is free.", true
	}
	return n.workflowScopeHandOver(ctx, c, report, why)
}
