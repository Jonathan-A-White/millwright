package application

import (
	"context"
	"fmt"

	"github.com/Jonathan-A-White/millwright/domain"
)

// leaveRedStoryOutAfterFixer is leaveRedStoryOut for a red batch whose fixer
// ended badly (failedFixer): the batch is held no longer when it can say which
// story made it red, by what the fixer said was the cause or touched, else by
// the files of the failing tests (BatchReport.suspect). The fixer's commits are
// kept as a held batch keeps them. A batch that cannot say, or whose fixer did
// not run, stays held, to be landed in parts.
func (b BatchLand) leaveRedStoryOutAfterFixer(ctx context.Context, dir *string, rigDir string, rules domain.EpicRequirements, report *BatchReport, nr *NextReport) (gated, bool) {
	f := report.Fixer
	if f == nil || !f.Started || f.Failed == "" || !report.redForTheFixer() {
		return gated{}, false
	}
	return b.leaveRedStoryOut(ctx, dir, rigDir, rules, report, nr, report.suspect())
}

// leaveRedStoryOut makes the batch again without the story id, the one of its
// merged stories that made its tests red, and runs the tests on it as merged:
// the green stories are not held for a whole gate more, nor for a fixer, nor
// for the drain that lands a held batch in parts (mw-qqxlwm3.6). The story stays
// queued, set aside with the tests it failed; it is not counted as set aside for
// a conflict, which would hand it to a Builder to rebase. It is done once for a
// batch, and only for a batch of several merged stories. It reports the run of
// the tests on the batch made again, and whether the batch was made again: when
// it was not, nothing was changed. When it was, Held says why the batch goes no
// further, or is empty.
//
// The landing worktree is made again from the base, not undone by git, because
// the fixer's commits (when it ran) are kept as a bundle from the first one.
func (b BatchLand) leaveRedStoryOut(ctx context.Context, dir *string, rigDir string, rules domain.EpicRequirements, report *BatchReport, nr *NextReport, id string) (gated, bool) {
	n := b.Next
	if id == "" || report.leftOutRed || len(report.Merged) < 2 {
		return gated{}, false
	}
	var out BatchStory
	var rest []BatchStory
	for _, s := range report.Merged {
		if s.ID == id {
			out = s
		} else {
			rest = append(rest, s)
		}
	}
	if out.ID == "" || len(rest) == 0 {
		return gated{}, false
	}
	// What is kept of the first worktree is kept before it is taken away.
	b.bundleFixer(ctx, *dir, report)
	fresh, err := n.Landing.OpenLanding(ctx, rigDir, report.Base)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the batch could not be made again without %s, whose tests failed, so it is held as it was: %v", id, err))
		return gated{}, false
	}
	if err := n.Landing.CloseLanding(ctx, rigDir, *dir); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the landing worktree %s could not be taken away: %v", *dir, err))
	}
	*dir, report.landing, report.leftOutRed = fresh, fresh, true
	report.Held = ""

	out.Why = "its tests failed on the batch"
	if failing := FailingTestsShort(report.Gate.Output); failing != "" {
		out.Why += " (" + failing + ")"
	}
	out.Why += ", so the rest of the batch went on without it"
	b.say("%s: %s left out for its failing tests; the other %s made again and tested without it", report.Label(), id, countStories(len(rest)))

	// The batch again, from the base, in its order: what merged before merges now.
	// The stories set aside for their conflicts stay set aside, as counted.
	report.Merged = nil
	queue := report.Queue
	report.Queue = rest
	b.mergeAll(ctx, fresh, rigDir, rules, report, nr)
	report.Queue = queue
	report.SetAside = append(report.SetAside, out)
	report.Version, report.versionCommit, report.changelogFiles, report.changelogWritten = "", "", nil, nil

	if len(report.Merged) == 0 {
		report.Held = fmt.Sprintf("no branch of the batch merged onto %s once %s was left out for its failing tests", report.Target, id)
		return gated{}, true
	}
	if !b.raiseVersion(ctx, fresh, rigDir, rules, report, nr) {
		return gated{}, true
	}
	redo := b.runGate(ctx, fresh, rigDir, report, nr)
	// Still red without it: it was not the whole cause, and the batch's line does
	// not name it the suspect for the drain to leave out (BatchReport.suspect).
	if redo.Err == nil && !redo.Checked.NotRun && !redo.Checked.Passed {
		report.failingOwner = ""
	}
	return redo, true
}
