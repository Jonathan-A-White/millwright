package application

import (
	"context"
	"fmt"
	"strings"
)

// specsRefusal runs the rig's changed-specs command in dir, on the specs the
// story's branch added or changed since its merge-base with base, and says why
// the landing must stop when it did not pass (mw-gq6.366). It is false when
// the rig names no command, the story changed no spec, or the run passed. It
// is asked after the rig's gate has passed, on the worktree and again on a merged
// result; fail is the reason a failed run is refused as.
func (n Next) specsRefusal(ctx context.Context, c *closeOut, report *NextReport, dir, base, where string, fail Reason) (Refusal, bool) {
	if n.SpecChecks == nil {
		return Refusal{}, false
	}
	run, err := n.SpecChecks.RunSpecs(ctx, c.path.Rig, dir, base)
	if len(run.LeftOut) > 0 {
		report.Notes = appendNew(report.Notes, LeftOutTimingSpecs(run.LeftOut))
	}
	named := strings.Join(run.Specs, ", ")
	switch {
	case err != nil:
		return Refusal{ReasonTestsNotRun, fmt.Sprintf("the rig's changed specs could not be run %s in %s: %v", where, dir, err), ""}, true
	case len(run.Specs) == 0, run.Passed:
		return Refusal{}, false
	case run.NotRun:
		return Refusal{ReasonTestsNotRun, fmt.Sprintf("the rig's changed specs could not be run %s: `%s` did not start, so this host is missing something the command needs (a toolchain not on its PATH?) [%s]", where, run.Command, named),
			"The last lines of `" + run.Command + "` in " + dir + ":\n\n" + fenced(run.Tail(CheckLines))}, true
	}
	why := fmt.Sprintf("the rig's changed specs fail %s: `%s` did not pass [%s]", where, run.Command, named)
	return Refusal{fail, why, n.failedTestsDetail(ctx, c, report, run.Checked, dir)}, true
}

// LeftOutTimingSpecs says which changed specs the changed-specs command was not
// given, and why: the rig's timing_tests name them (mw-gq6.682).
func LeftOutTimingSpecs(specs []string) string {
	return "left out: timing test, runs nightly: " + strings.Join(specs, ", ")
}

// specsFailReason is what a failing changed-specs run in the story's worktree
// is refused as: tests-fail, or the second merged-tests-fail for a worktree that
// already carries the target merged in, as the gate's own failure is.
func (n Next) specsFailReason(ctx context.Context, c *closeOut) Reason {
	if sentBack, _ := n.Tracker.StoryState(ctx, c.id, MergedTestsState); sentBack == MergedTestsSentBack {
		return ReasonMergedTestsFail
	}
	return ReasonTestsFail
}

// slottedSpecs runs the rig's changed-specs command while holding the rig's
// merge slot, as slottedChecks does the rig's tests.
type slottedSpecs struct {
	specs  SpecChecks
	slot   MergeSlot
	rigDir string
	holder string
}

func (s slottedSpecs) RunSpecs(ctx context.Context, rig, dir, base string) (SpecsChecked, error) {
	holding, err := takeForCheck(ctx, s.slot, s.rigDir, s.holder)
	if err != nil {
		return SpecsChecked{}, fmt.Errorf("the merge slot of %s could not be taken: %w", rig, err)
	}
	defer holding.Release(ctx)
	return s.specs.RunSpecs(ctx, rig, dir, base)
}
