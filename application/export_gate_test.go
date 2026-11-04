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
