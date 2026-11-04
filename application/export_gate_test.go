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
// batch's push, for the stories its report holds as merged.
func (b BatchLand) RecordLanded(ctx context.Context, rigDir string, report *BatchReport, nr *NextReport) {
	b.recordLanded(ctx, rigDir, report, nr)
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
