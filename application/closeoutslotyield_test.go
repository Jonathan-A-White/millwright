package application_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1424: a close-out took its rig's merge slot alone before the merge and
// gave it back only after the push, and the merged result's wait for a calm host
// was made inside it: a whole check of the same rig, which takes the slot shared,
// waited that wait out too. A batch landing gives its slot up to shared for such a
// wait (SlotYield, mw-4hqh0en) and takes it alone again before its tests; a
// close-out of one story does the same.

// aYieldingSlot is the rig's merge slot as a test watches it: whether it is held
// alone, shared (yielded) or not at all, and how often it was yielded.
type aYieldingSlot struct {
	mu      sync.Mutex
	state   string // "", "alone" or "shared"
	yields  int
	resumes int
	// resumeErr, when set, is what taking the slot alone again returns.
	resumeErr error
}

func (s *aYieldingSlot) Take(context.Context, string, string) (application.Holding, error) {
	s.set("alone")
	return &aYieldingHolding{s: s}, nil
}

func (s *aYieldingSlot) set(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

func (s *aYieldingSlot) now() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

type aYieldingHolding struct{ s *aYieldingSlot }

func (h *aYieldingHolding) Release(context.Context) error { h.s.set(""); return nil }
func (h *aYieldingHolding) HeldBy() string                { return "builder on laptop, closing out mw-x.1" }

func (h *aYieldingHolding) Yield(context.Context) error {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if h.s.state == "alone" {
		h.s.state = "shared"
		h.s.yields++
	}
	return nil
}

func (h *aYieldingHolding) Resume(context.Context) error {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if h.s.state != "shared" {
		return nil
	}
	h.s.resumes++
	if h.s.resumeErr != nil {
		h.s.state = ""
		return h.s.resumeErr
	}
	h.s.state = "alone"
	return nil
}

// checksThatFillTheHost are the rig's tests: the host fills when the run in the
// worktree ends, so that the run on the merged result waits for it to calm, and
// each run notes how the merge slot was held while it ran.
type checksThatFillTheHost struct {
	host *aHostThatFillsAfterTheGate
	slot *aYieldingSlot
	held []string
}

func (c *checksThatFillTheHost) Run(context.Context, string, string) (application.Checked, error) {
	c.held = append(c.held, c.slot.now())
	if len(c.held) == 1 {
		c.host.set(true)
	}
	return application.Checked{Command: "make test", Passed: true, Output: "ok\n"}, nil
}

// aCloseOutWithAYieldingSlot is mw next's close-out of one story whose merged
// result finds the host busy, over slot; each step of the wait for a calm host
// notes how the slot was held, and calms the host.
func aCloseOutWithAYieldingSlot(t *testing.T, slot *aYieldingSlot) (application.NextReport, error, *checksThatFillTheHost, []string) {
	t.Helper()
	host := &aHostThatFillsAfterTheGate{}
	checks := &checksThatFillTheHost{host: host, slot: slot}
	var waitedHolding []string
	wait := func(context.Context, time.Duration) error {
		waitedHolding = append(waitedHolding, slot.now())
		host.set(false)
		return nil
	}
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	landing := &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: checks, Slot: slot, Mailbox: apptest.NewFakeMailbox(), GateLock: &aWatchedGateLock{},
		Load: host, LoadWait: wait, LoadPoll: time.Minute,
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return report, err, checks, waitedHolding
}

func TestACloseOutHoldsNoMergeSlotWhileItWaitsForCalmOnTheMergedResult(t *testing.T) {
	slot := &aYieldingSlot{}

	report, err, checks, waitedHolding := aCloseOutWithAYieldingSlot(t, slot)

	if err != nil || !report.Landed {
		t.Fatalf("expected the story to land once the host calmed, got %v and %q\n%s", err, report.Reason, strings.Join(report.Notes, "\n"))
	}
	if len(waitedHolding) == 0 {
		t.Fatal("expected the merged result to wait for the host to calm")
	}
	for i, held := range waitedHolding {
		if held == "alone" {
			t.Errorf("expected the close-out to share its merge slot while the merged result waits for a calm host, step %d of %d of the wait held it alone", i+1, len(waitedHolding))
		}
	}
	if len(checks.held) != 2 {
		t.Fatalf("expected the rig's tests to run in the worktree and on the merged result, got %d runs", len(checks.held))
	}
	if checks.held[1] != "alone" {
		t.Errorf("expected the rig's tests on the merged result to run with the merge slot taken alone again, it was %q", checks.held[1])
	}
	if slot.now() != "" {
		t.Errorf("expected the merge slot to be given back at the end, it is held %s", slot.now())
	}
}

// A close-out that cannot take its slot alone again after the wait pushes
// nothing: the merge slot is not its, so the close-out is handed back, as one
// that could not take the slot at all is.
func TestACloseOutThatCannotTakeItsSlotAloneAgainAfterTheWaitPushesNothing(t *testing.T) {
	slot := &aYieldingSlot{resumeErr: fmt.Errorf("the merge slot was still held after 2h0m0s: %w", application.ErrSlotCap)}

	report, _, checks, _ := aCloseOutWithAYieldingSlot(t, slot)

	if report.Landed {
		t.Fatal("expected nothing landed by a close-out that lost its merge slot during a wait")
	}
	if report.Reason != application.ReasonMergeSlot {
		t.Errorf("expected the close-out to be handed back for the merge slot, got %q\n%s", report.Reason, strings.Join(report.Notes, "\n"))
	}
	if len(checks.held) != 1 {
		t.Errorf("expected no run on the merged result without the merge slot, got %d runs", len(checks.held))
	}
}

// fillingChecks are the rig's tests, passing, with the host filling when the
// run in the worktree ends.
type fillingChecks struct {
	host *aHostThatFillsAfterTheGate
	runs int
}

func (c *fillingChecks) Run(context.Context, string, string) (application.Checked, error) {
	if c.runs++; c.runs == 1 {
		c.host.set(true)
	}
	return application.Checked{Command: "make test", Passed: true, Output: "ok\n"}, nil
}

// Over the real merge slots and gate lock: while the merged result waits for a
// calm host, a check of the same rig takes the slot shared at once.
func TestACheckOfTheRigTakesTheSlotSharedWhileACloseOutWaitsForCalmOnTheMergedResult(t *testing.T) {
	w := newWholeCheckBesideTheGateLock(t, nil)
	host := &aHostThatFillsAfterTheGate{}
	shared, waits := 0, 0
	wait := func(ctx context.Context, _ time.Duration) error {
		waits++
		try, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if holding, err := w.slots.TakeShared(try, w.rigDir, "builder on laptop, checking mw-x.2"); err == nil {
			shared++
			_ = holding.Release(ctx)
		}
		host.set(false)
		return nil
	}
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	landing := &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
	checks := &fillingChecks{host: host}

	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: checks, Slot: w.slots, Mailbox: apptest.NewFakeMailbox(), GateLock: w.gate,
		Load: host, LoadWait: wait, LoadPoll: time.Minute,
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": w.rigDir},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")

	if err != nil || !report.Landed {
		t.Fatalf("expected the story to land once the host calmed, got %v and %q\n%s", err, report.Reason, strings.Join(report.Notes, "\n"))
	}
	if waits == 0 || shared != waits {
		t.Errorf("expected a check of the rig to take the merge slot shared at every step of the merged result's wait for a calm host, it did at %d of %d", shared, waits)
	}
	if checks.runs != 2 {
		t.Errorf("expected the rig's tests to run in the worktree and on the merged result, got %d runs", checks.runs)
	}
	try, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	holding, err := w.slots.Take(try, w.rigDir, "the test")
	if err != nil {
		t.Fatalf("expected the merge slot given back at the end: %v", err)
	}
	_ = holding.Release(context.Background())
}
