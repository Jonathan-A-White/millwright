package application

import (
	"context"
	"fmt"
)

// keepMainRed says which tests and which bug the batch's line keeps for the
// rig's next batch. A host's gap is not main's red: the home passes those tests,
// so nothing is kept to check them by.
func (r *BatchReport) keepMainRed(red *MainRed) {
	if red.Gap != nil {
		return
	}
	r.mainTests, r.mainBug, r.mainTitles = red.Tests, red.Bug, red.Titles
}

// mainStillRed is the check made before a batch is merged and gated, for a rig
// whose last batch was held on main's own red: the tests that hold named are run
// by name on the target as it is now, alone (mainAlone's own run), a Playwright
// test at the line its kept title is at there, and when they still all fail the batch is held again at once, on the same carrier and the new
// head, with no gate run: a gate costs half an hour, and the red was not touched
// by the move (batch 677 of lampas, mw-gq6.1706). When they pass, or fail other
// than as before, or cannot be run, or the carrier is closed (its fix is the
// move), or the hold kept no tests, or the batch holds the carrier itself (its
// own gate proves its fix, mw-gq6.1708), the batch goes on to its gate as
// before. It says whether the batch was held.
func (b BatchLand) mainStillRed(ctx context.Context, rigDir string, report *BatchReport, nr *NextReport) bool {
	if b.Metrics == nil || b.Next.Tracker == nil {
		return false
	}
	held, ok := HeldOnMainRed(rigBatches(readBatches(ctx, b.Metrics), report.Rig))
	if !ok || len(held.MainTests) == 0 || held.MainBug == "" || held.MainBranch != report.Target {
		return false
	}
	// A batch that holds the carrier is the one landing the red does not hold: its
	// own gate proves the fix, and its landing is what moves main (mw-gq6.1708).
	if _, ok := held.redCarrierIn(storyIDs(report.Queue)); ok {
		nr.Notes = append(nr.Notes, fmt.Sprintf("%s, which carries %s's red, is in this batch: it is gated without a check of the red first", held.MainBug, report.Target))
		return false
	}
	n := b.Next
	carrier, err := n.Tracker.ShowBeads(ctx, []string{held.MainBug})
	if err != nil {
		nr.Notes = append(nr.Notes, fmt.Sprintf("%s's red was not run again before the gate: %s, which carries it, could not be read: %v", report.Target, held.MainBug, err))
		return false
	}
	open := false
	for _, d := range carrier {
		open = open || (d.Story.ID == held.MainBug && !d.Closed())
	}
	if !open {
		nr.Notes = append(nr.Notes, fmt.Sprintf("%s, which carried %s's red in %s, is closed: the batch is gated", held.MainBug, report.Target, BatchLabel(report.Rig, held.Batch)))
		return false
	}
	var again NextReport
	bc := b.batchCloseOut(report, rigDir)
	red := n.mainAloneNamed(ctx, bc, &again, held.MainTests, held.MainTitles, report.Base, func() string {
		return "they no longer fail as they did in " + BatchLabel(report.Rig, held.Batch)
	})
	if record := bc.recordPath(bc.mainLog); bc.mainLog != "" && record != "" {
		report.Records = append(report.Records, record)
	}
	nr.Notes = append(nr.Notes, again.Notes...)
	if red == nil || red.Gap != nil || ctx.Err() != nil {
		return false
	}
	red.Bug = held.MainBug
	nr.MainRed = red
	report.MainLabels = again.MainLabels
	report.HeldOnMainRed = true
	report.keepMainRed(red)
	report.Held = fmt.Sprintf("main is red: %s, carried by %s; they failed on %s alone before the batch was made, as in %s, so the rig's tests were not run on the batch and the stories stay queued",
		red.tests(), red.Bug, shortCommit(report.Base), BatchLabel(report.Rig, held.Batch))
	return true
}
