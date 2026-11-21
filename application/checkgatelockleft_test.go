package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// checkWithAPouredFormula is w.check on a story whose formula was poured, so that
// the gate's formula step is open to be read (mw-gq6.1562).
func (w *aWholeCheckBesideTheHostsGateLock) checkWithAPouredFormula(t *testing.T, wait time.Duration) (application.CheckReport, error) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	tracker.AddFormula("tdd-feature",
		application.FormulaStep{ID: "s-1", Title: "Write the failing test first"},
		application.FormulaStep{ID: "s-2", Title: "Run the full build, tests and vet"})
	poured, err := tracker.PourFormula(context.Background(), "tdd-feature", "mw-x.1", "One", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryMetadata(context.Background(), "mw-x.1", map[string]string{application.MoleculeField: poured.RootID}); err != nil {
		t.Fatal(err)
	}
	return application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: w.checks,
		Slot: w.slots, GateLock: w.gate, Records: w.records, PartSlotWait: wait,
		GateNoticeEvery: 40 * time.Millisecond,
		Vault:           "/home/mw/vault", Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir},
		Out:  writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
}

// A foreground check handed back on the host's gate lock a second time in one
// attempt, with the gate's formula step open, no longer ends by telling the Builder
// to hand back with the step open: it names the step and the command that closes
// it as left to the close-out, as the check that reached its wait limit does
// (mw-gq6.1557, mw-gq6.1562).
func TestAWholeCheckTwiceHandedBackOnTheGateLockLeavesTheGateToTheCloseOut(t *testing.T) {
	w := newWholeCheckBesideTheGateLock(t, &scriptedChecks{passes: []bool{true}})
	closeOut, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOut.Holding.Release(context.Background())

	first, _ := w.checkWithAPouredFormula(t, 80*time.Millisecond)
	one := slotRefusalOf(first)
	if one == nil || strings.Contains(one.Why, "counted once") {
		t.Fatalf("expected the first hand-back to be a plain wait, got %+v", first.Refusals)
	}
	second, _ := w.checkWithAPouredFormula(t, 80*time.Millisecond)
	two := slotRefusalOf(second)
	if two == nil {
		t.Fatalf("expected a refusal on the second hand-back, got %+v", second.Refusals)
	}
	for _, want := range []string{
		"counted once for this attempt", "not a failure", "mw-gq6.894",
		"f-mol-1.2", "Run the full build, tests and vet",
		"bd -C /home/mw/vault close f-mol-1.2", "gate left to the close-out",
		"write the closing comment", "end the turn", "mw next runs the gate",
	} {
		if !strings.Contains(two.Why, want) {
			t.Errorf("expected the repeat to say %q, got %q", want, two.Why)
		}
	}
	for _, not := range []string{"and hand back", "run the same check again"} {
		if strings.Contains(two.Why, not) {
			t.Errorf("expected the repeat not to say %q with the gate step open, got %q", not, two.Why)
		}
	}
}
