package application

import "context"

// FailingTestFiles lets the tests of package application_test ask which files
// a failing run names.
func FailingTestFiles(output string) []string { return failingTestFiles(output) }

// GateTheBatch lets the tests of package application_test run the gate a batch
// landing makes on its merged result, as the batch's report has it.
func (b BatchLand) GateTheBatch(ctx context.Context, dir string, report *BatchReport) Checked {
	return b.runGate(ctx, dir, dir, report, &NextReport{}).Checked
}
