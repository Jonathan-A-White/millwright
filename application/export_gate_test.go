package application

import (
	"context"

	"github.com/Jonathan-A-White/millwright/domain"
)

// FailingTestFiles lets the tests of package application_test ask which files
// a failing run names.
func FailingTestFiles(output string) []string { return failingTestFiles(output) }

// GateTheBatch lets the tests of package application_test run the gate a batch
// landing makes on its merged result, as the batch's report has it.
func (b BatchLand) GateTheBatch(ctx context.Context, dir string, report *BatchReport) Checked {
	return b.runGate(ctx, dir, dir, report, &NextReport{}).Checked
}

// RecordLanded lets the tests of package application_test run what follows a
// batch's push, for the stories its report holds as merged; release stands for
// letting the rig's merge slot go.
func (b BatchLand) RecordLanded(ctx context.Context, rigDir string, report *BatchReport, nr *NextReport, release func()) {
	b.recordLanded(ctx, rigDir, report, nr, release)
}

// WithTailTrace is n telling trace the name of each step of the landed tail as
// it is taken, followed by the story's id for a step taken for each story.
func (n Next) WithTailTrace(trace func(step string)) Next {
	n.tailTrace = trace
	return n
}

// LandedTailStep is a row of the landed tail as the tests of package
// application_test read it.
type LandedTailStep struct {
	Name         string
	OnceForBatch bool
	// Only is "single" or "batch" for a step one path alone takes, else empty.
	Only, Why string
}

// LandedTail is the rows of the landed tail, and the orders the two paths walk
// it in: the single landing's, and the batch's in groups.
func LandedTail() (steps []LandedTailStep, single []string, batch [][]string) {
	for _, step := range landedTail {
		steps = append(steps, LandedTailStep{Name: step.name, OnceForBatch: step.scope == onceForBatch, Only: string(step.only), Why: step.why})
	}
	single = append(append(append(single, singleReads...), singleAfterPush...), singleFinish...)
	return steps, single, batchOrder
}

// OutsideTheTailStep is a step of a landing that is no row of the landed tail,
// and why, as the tests of package application_test read it.
type OutsideTheTailStep struct{ Name, Why string }

// OutsideTheTail is the steps of a landing after its push that the landed tail
// does not hold, each with the reason the table gives.
func OutsideTheTail() (steps []OutsideTheTailStep) {
	for _, step := range outsideTheTail {
		steps = append(steps, OutsideTheTailStep{Name: step.name, Why: step.why})
	}
	return steps
}

// MergedStory is a story of a batch as the batch merged it, with the detail and
// path the batch read of it, for the tests of package application_test.
func MergedStory(id, merge string, detail StoryDetail, path domain.Path) BatchStory {
	return BatchStory{ID: id, Title: detail.Story.Title, Merge: merge, Commits: 1, detail: detail, path: path, Branch: "origin/" + StoryBranch(id)}
}

// MainAloneOfTheBatch lets the tests of package application_test run the batch's
// failing tests on main alone, as the batch landing does after its gate.
func (b BatchLand) MainAloneOfTheBatch(ctx context.Context, rigDir string, report *BatchReport, failed Checked) {
	b.mainAlone(ctx, rigDir, report, &NextReport{}, failed)
}

// GateTheBatchRun is GateTheBatch with what else the gate said: the error that
// kept the tests from running, and the notes the close-out helpers made.
func (b BatchLand) GateTheBatchRun(ctx context.Context, dir string, report *BatchReport) (Checked, error, []string) {
	var nr NextReport
	gate := b.runGate(ctx, dir, dir, report, &nr)
	return gate.Checked, gate.Err, nr.Notes
}

// FixTheBatch lets the tests of package application_test run the batch's fixer
// step (BatchLand.fix), and say whether the batch may go on to be tested.
func (b BatchLand) FixTheBatch(ctx context.Context, dir string, report *BatchReport) bool {
	return b.fix(ctx, dir, dir, domain.EpicRequirements{}, report, "the gate failed", &NextReport{})
}

// HoldQueue lets the tests of package application_test see the lease and the
// closing-out mark a batch keeps on its queue while it runs.
func (b BatchLand) HoldQueue(ctx context.Context, queue []BatchStory) func() {
	unhold := b.holdQueue(ctx, queue)
	return func() { unhold() }
}

// FixerReads lets the tests of package application_test read what a held batch's
// outcome.txt and Held mail say of its fixer.
func (b BatchLand) FixerReads(report *BatchReport) (outcome, mail string) {
	return report.fixerBundleLines(), report.fixerMail()
}

// HoldingTheSlot is b holding the rig's merge slot as Run holds it once it has
// taken it (batchSlot), so that its waits yield holding as a batch's do.
func (b BatchLand) HoldingTheSlot(holding Holding) BatchLand {
	b.slot = &batchSlot{holding: holding}
	b.Next.SlotYield, b.Next.SlotShared = b.slot.yield, b.slot.shared
	return b
}

// LandTheBatchIn lets the tests of package application_test run the batch's
// merges, fixer, gate and push (BatchLand.landIn) over a queue, and the note a
// landed batch leaves on each story it set aside, as Run does them in order.
func (b BatchLand) LandTheBatchIn(ctx context.Context, dir string, report *BatchReport) {
	var nr NextReport
	b.landIn(ctx, dir, dir, domain.EpicRequirements{}, report, &nr)
	b.handBackRepeaters(ctx, report)
	b.handBackRedAlone(ctx, report)
	if report.Landed {
		b.noteSetAside(ctx, report)
	}
	report.Notes = append(report.Notes, nr.Notes...)
}

// MailTheBatch lets the tests of package application_test send the one mail a
// batch that did not land whole sends the Mayor (BatchLand.mailTheMayor).
func (b BatchLand) MailTheBatch(ctx context.Context, report *BatchReport) {
	b.mailTheMayor(ctx, report, &NextReport{})
}

// ClaimTheRig is claimTheRig, for a test of what a landing does with the rig's
// batch claim.
func (b BatchLand) ClaimTheRig(ctx context.Context, rig string, report *BatchReport) (said []string, unclaim func(), claimed bool) {
	return b.claimTheRig(ctx, rig, report)
}
