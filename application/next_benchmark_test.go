package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// timedChecks is the rig's tests that print what they were told to and take as
// long on the close-out's clock as they were told to.
type timedChecks struct {
	passed bool
	output string
	took   time.Duration
	clock  *time.Time
}

func (c *timedChecks) Run(context.Context, string, string) (application.Checked, error) {
	*c.clock = c.clock.Add(c.took)
	return application.Checked{Command: "make test", Passed: c.passed, Output: c.output}, nil
}

// aBenchmarkedCloseOut closes out a story claimed 40 minutes before it ends (the tests run inside that) whose tests
// took 5m10s, on a host at load, with two stories running, and returns what it
// left in the vault.
func aBenchmarkedCloseOut(t *testing.T, passed bool, output string, load application.LoadReading, past []application.Benchmark) (*gateRun, *fakeVault) {
	t.Helper()
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.2", Title: "Another story"})
	for _, id := range []string{"mw-x.1", "mw-x.2"} {
		if err := tracker.ClaimAs(id, "mw@laptop", now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tracker.SetStarted("mw-x.1", now.Add(-2090*time.Second)); err != nil {
		t.Fatal(err)
	}
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success","session_id":"s1"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	run := &gateRun{vault: vault, out: &strings.Builder{}, mailbox: apptest.NewFakeMailbox()}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &timedChecks{passed: passed, output: output, took: 310 * time.Second, clock: &now}, Slot: aSlot{}, Mailbox: run.mailbox,
		Load:       &apptest.FakeHostLoad{Reading: load},
		LoadWait:   func(context.Context, time.Duration) error { return nil },
		Benchmarks: &apptest.FakeBenchmarks{Records: past},
		Now:        func() time.Time { return now },
		Seat:       "builder", Host: "laptop",
		Rigs: map[string]string{"lampas": "/rigs/lampas"},
		Out:  run.out, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return run, vault
}

func TestACloseOutRecordsItsGateAndTheHostInTheResultFile(t *testing.T) {
	load := application.LoadReading{Load: 3.5, Cores: 20, MemAvailableMB: 9000, MemKnown: true, SwapInKBPerS: 12, SwapOutKBPerS: 34, SwapKnown: true}
	run, vault := aBenchmarkedCloseOut(t, true, "ok\n", load, nil)
	if run.err != nil || !run.report.Landed {
		t.Fatalf("expected the story landed, got err %v and %+v", run.err, run.report)
	}
	result := vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)]
	for _, want := range []string{`"gate_seconds":310`, `"load_at_gate":3.5`, `"cores":20`, `"mem_free_mb":9000`,
		`"running_count":2`, `"swap_in_per_s":12`, `"swap_out_per_s":34`, `"actual_s":2400`, `"kind":"lampas/feature/sonnet"`,
		`"session_id":"s1"`} {
		if !strings.Contains(result, want) {
			t.Errorf("expected the result to carry %s, got %s", want, result)
		}
	}
	// No history: no par yet, said so.
	if !strings.Contains(result, `"par_basis":"none"`) || !strings.Contains(result, `"par_s":0`) {
		t.Errorf("expected no par from no history, got %s", result)
	}
	if got := strings.Join(run.report.Bench, "\n"); !strings.Contains(got, "gate 5m10s at load 3.5 of 20 cores") {
		t.Errorf("expected the report to summarise the gate, got %q", got)
	}
}

func TestALandedStoryIsGivenTheParOfItsKindAndSaysHowFarOverIt(t *testing.T) {
	var past []application.Benchmark
	for i := 0; i < 5; i++ {
		past = append(past, application.Benchmark{
			Story: "old", Rig: "lampas", Host: "laptop", Kind: "lampas/feature/sonnet", Landed: true, ActualS: 1200,
			At: time.Date(2026, 10, 8, 0, i, 0, 0, time.UTC), GateSeconds: 60,
		})
	}
	run, vault := aBenchmarkedCloseOut(t, true, "ok\n", application.LoadReading{Load: 1, Cores: 20}, past)
	result := vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)]
	for _, want := range []string{`"par_s":1200`, `"actual_s":2400`, `"par_basis":"kind"`} {
		if !strings.Contains(result, want) {
			t.Errorf("expected the result to carry %s, got %s", want, result)
		}
	}
	if got := strings.Join(run.report.Bench, "\n"); !strings.Contains(got, "2.0x par") {
		t.Errorf("expected the report to say the story ran at 2.0x par, got %q", got)
	}
}

func TestATimeoutAtLoadAtTheCoreCountIsNamedInTheRefusalAndTheMail(t *testing.T) {
	busy := application.LoadReading{Load: 39, Cores: 20}
	run, vault := aBenchmarkedCloseOut(t, false, "--- FAIL: TestX\n    Test timed out after 30s\n", busy, nil)
	if run.err == nil || run.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", run.err, run.report)
	}
	if !strings.Contains(run.report.Why, "timeout under load") {
		t.Errorf("expected the refusal to say timeout under load, got %q", run.report.Why)
	}
	mail, _ := run.mailbox.Inbox(context.Background(), application.MayorMailbox)
	if len(mail) != 1 || !strings.Contains(mail[0].Body, "timeout under load") {
		t.Errorf("expected the mail to the Mayor to say timeout under load, got %+v", mail)
	}
	if result := vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)]; !strings.Contains(result, `"timeout_under_load":true`) || !strings.Contains(result, `"landed":false`) {
		t.Errorf("expected the refusal recorded as a timeout under load, got %s", result)
	}
}

func TestAFailureThatIsNotATimeoutIsNotNamedOne(t *testing.T) {
	busy := application.LoadReading{Load: 39, Cores: 20}
	run, _ := aBenchmarkedCloseOut(t, false, "--- FAIL: TestX\n    got 3, want 4\n", busy, nil)
	if run.err == nil || run.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", run.err, run.report)
	}
	if strings.Contains(run.report.Why, "timeout under load") {
		t.Errorf("expected no timeout named, got %q", run.report.Why)
	}
	mail, _ := run.mailbox.Inbox(context.Background(), application.MayorMailbox)
	if len(mail) != 1 || strings.Contains(mail[0].Body, "timeout under load") {
		t.Errorf("expected a mail that does not say timeout under load, got %+v", mail)
	}
}

func TestATimeoutOnACalmHostIsNotNamedUnderLoad(t *testing.T) {
	run, _ := aBenchmarkedCloseOut(t, false, "Test timed out after 30s\n", application.LoadReading{Load: 2, Cores: 20}, nil)
	if run.err == nil || strings.Contains(run.report.Why, "timeout under load") {
		t.Errorf("expected a plain refusal, got err %v and %q", run.err, run.report.Why)
	}
}
