package application_test

import "testing"

// mw-6ww.175.24: mw report with no --rig took over 5 minutes at load 15. The
// report reads the tracker for the stories since the window's start once, and
// the comments of the landed stories once (one call for all of them), however
// many rigs, models and hosts it sums: a read of the tracker is a bd of the
// whole vault's list, 40 s or more under load.
func TestFactoryReportReadsTheTrackerOnce(t *testing.T) {
	f := newChangeFactory(t)
	f.rows("2026-09-27", "lampas", 4, 1)
	f.rows("2026-10-05", "lampas", 2, 2)
	f.rows("2026-10-06", "fellowship", 3, 1)
	f.change("1. It works.\n2. Expect: refusals down", "2026-10-01")

	f.report(true)

	if got := f.tracker.Reads("StoriesSince"); got != 1 {
		t.Errorf("the report read the stories since %d times, want once", got)
	}
	if got := f.tracker.StoriesCommentsCalls(); got != 1 {
		t.Errorf("the report read the comments of the landed stories %d times, want once", got)
	}
}
