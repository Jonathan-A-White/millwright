package application_test

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1423: the story's changed specs run again on a merged result when they
// fail with the host far over its cores, and the wait for the host to calm before
// that run was made holding this host's gate lock, taken for the gate on the
// merged result, and the rig's merge slot across hosts: every other gate on the
// host, and the other host's landing of the rig, waited out the calm with it. The
// gate on the merged result itself never waits holding either (mw-gq6.422,
// mw-gq6.1420), and neither does the specs' run again.

// specsFailFarOverOnTheMergedResult is a host that is calm until the changed
// specs' second run, the one on the merged result, and far over its cores after
// it until it has been waited on calmsAfter times.
func specsFailFarOverOnTheMergedResult(calmsAfter int) func(runs, waits int) application.LoadReading {
	return func(runs, waits int) application.LoadReading {
		if runs == 2 && waits < calmsAfter {
			return specsFarOver
		}
		return specsCalm
	}
}

func TestTheChangedSpecsOnAMergedResultHoldNoGateLockWhileTheyWaitToRunAgain(t *testing.T) {
	lock := &aWatchedGateLock{}
	specs := &aSpecsHost{
		// In the worktree, on the merged result, and on it again once the host calmed.
		results: []application.SpecsChecked{passedSpecs(), failedSpecs("TestA"), passedSpecs()},
		loads:   specsFailFarOverOnTheMergedResult(3),
	}
	server := apptest.NewFakeTracker()
	var crossHeldWaits int
	watch := &aCalmWatch{lock: lock, then: func(d time.Duration) {
		if note, _ := server.Note(context.Background(), application.CrossSlotKey("millwright")); note != "" {
			crossHeldWaits++
		}
		_ = specs.wait(context.Background(), d)
	}}
	var polls atomic.Int32
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	landing := &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
	checks := &checksUnderTheLock{lock: lock}

	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: checks, SpecChecks: specs, Slot: aSlot{}, CrossSlot: aCrossSlotOn(server, "laptop", &polls),
		Mailbox: apptest.NewFakeMailbox(), GateLock: lock,
		Load: specs, LoadWait: watch.wait, LoadPoll: time.Minute,
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")

	if err != nil || !report.Landed {
		t.Fatalf("expected the story to land once its changed specs passed on the calm run, got %v and %q\n%s", err, report.Reason, strings.Join(report.Notes, "\n"))
	}
	if specs.runs != 3 {
		t.Fatalf("expected the changed specs to run once in the worktree and twice on the merged result, got %d runs", specs.runs)
	}
	watch.sawTheLockFree(t, "the changed specs on a merged result, before their run again")
	if crossHeldWaits > 0 {
		t.Errorf("expected the changed specs on a merged result to hold no merge slot across hosts while they wait for the host to calm: %d of %d steps of the wait were made holding it", crossHeldWaits, watch.waits)
	}
	if note, _ := server.Note(context.Background(), application.CrossSlotKey("millwright")); note != "" {
		t.Errorf("expected the merge slot across hosts to be given back at the end, it holds %q", note)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "the changed specs ran twice") {
		t.Errorf("expected a note that the changed specs ran twice, got:\n%s", strings.Join(report.Notes, "\n"))
	}
}
