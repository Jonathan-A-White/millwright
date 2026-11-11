package application_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1076: the changed-specs run is dealt with as a gate's failure is when
// the host was far over its cores while it ran: the host is waited on and the
// specs are run once more, the second run standing.

// aSpecsHost is a rig's changed-specs command and the host it runs on. Each run
// answers with the next of results (the last once they are spent). The load is
// read from loads, given the number of runs made so far and the waits made since
// the last of them.
type aSpecsHost struct {
	mu      sync.Mutex
	results []application.SpecsChecked
	loads   func(runs, waits int) application.LoadReading
	runs    int
	waits   int
}

func (h *aSpecsHost) RunSpecs(context.Context, string, string, string) (application.SpecsChecked, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.runs++
	h.waits = 0
	return h.results[min(h.runs, len(h.results))-1], nil
}

func (h *aSpecsHost) Load(context.Context) (application.LoadReading, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.loads(h.runs, h.waits), nil
}

func (h *aSpecsHost) wait(context.Context, time.Duration) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.waits++
	return nil
}

func failedSpecs(failing string) application.SpecsChecked {
	return application.SpecsChecked{
		Checked: application.Checked{Command: "npx playwright test", Output: "--- FAIL: " + failing + " (0.01s)\nFAIL\n"},
		Specs:   []string{"tests/e2e/a.spec.ts"},
	}
}

func passedSpecs() application.SpecsChecked {
	return application.SpecsChecked{
		Checked: application.Checked{Command: "npx playwright test", Passed: true},
		Specs:   []string{"tests/e2e/a.spec.ts"},
	}
}

var (
	specsCalm    = application.LoadReading{Load: 2, Cores: 8}
	specsBusy    = application.LoadReading{Load: 12, Cores: 8} // between the cores and twice them
	specsFarOver = application.LoadReading{Load: 24, Cores: 8}
)

func aSpecsRun(t *testing.T, host *aSpecsHost) closeOutOnMain {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	landing := &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
	mailbox := apptest.NewFakeMailbox()
	got := closeOutOnMain{landing: landing, vault: vault}
	got.report, got.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: gatePasses{}, SpecChecks: host, Slot: aSlot{}, Mailbox: mailbox,
		Load: host, LoadWait: func(ctx context.Context, d time.Duration) error { return host.wait(ctx, d) },
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	inbox, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range inbox {
		got.mail += m.Body + "\n"
	}
	return got
}

// farOverAfterTheFirstRun is a host that is calm until the first run, far over
// its cores after it until it has been waited on calmsAfter times, and calm
// after; with farAgain it is far over after the second run too.
func farOverAfterTheFirstRun(calmsAfter int, farAgain bool) func(runs, waits int) application.LoadReading {
	return func(runs, waits int) application.LoadReading {
		switch {
		case runs == 1 && waits < calmsAfter:
			return specsFarOver
		case runs >= 2 && farAgain:
			return specsFarOver
		}
		return specsCalm
	}
}

func TestChangedSpecsThatFailFarOverPassOnTheCalmRunAndAreNotRefused(t *testing.T) {
	host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), passedSpecs()}, loads: farOverAfterTheFirstRun(3, false)}
	got := aSpecsRun(t, host)
	if got.report.Reason == application.ReasonTestsFail || got.report.Reason == application.ReasonMergedTestsFail || got.report.Reason == application.ReasonTestsNotRun {
		t.Fatalf("expected no refusal for changed specs that passed on the calm run, got %q (%v)", got.report.Reason, got.err)
	}
	// Twice in the worktree, and once more on the merged result.
	if host.runs != 3 {
		t.Errorf("expected the changed specs run twice in the worktree and once on the merged result, got %d runs", host.runs)
	}
	notes := strings.Join(got.report.Notes, "\n")
	for _, want := range []string{"the changed specs ran twice", "load 24.0 of 8 cores", "Flaked: TestA"} {
		if !strings.Contains(notes, want) {
			t.Errorf("expected a note saying %q, got:\n%s", want, notes)
		}
	}
}

func TestChangedSpecsThatFailFarOverAndAgainOnTheCalmRunAreRefusedOnTheSecondRun(t *testing.T) {
	host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), failedSpecs("TestC")}, loads: farOverAfterTheFirstRun(2, false)}
	got := aSpecsRun(t, host)
	if got.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected tests-fail, got %q (%v)", got.report.Reason, got.err)
	}
	if host.runs != 2 {
		t.Errorf("expected the changed specs run twice, got %d runs", host.runs)
	}
	if !strings.Contains(got.mail, "TestC") || strings.Contains(got.mail, "--- FAIL: TestA") {
		t.Errorf("expected the second run's failure, TestC, in the refusal and not the first run's, got:\n%s", got.mail)
	}
}

func TestChangedSpecsThatFailOnACalmOrOnlyABusyHostAreRunOnceAndRefused(t *testing.T) {
	for name, load := range map[string]application.LoadReading{"calm": specsCalm, "busy": specsBusy} {
		t.Run(name, func(t *testing.T) {
			host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), passedSpecs()}, loads: func(int, int) application.LoadReading { return load }}
			if name == "busy" {
				// The gate before the specs is calm: only the specs' own run is busy.
				host.loads = func(runs, _ int) application.LoadReading {
					if runs == 0 {
						return specsCalm
					}
					return load
				}
			}
			got := aSpecsRun(t, host)
			if got.report.Reason != application.ReasonTestsFail {
				t.Fatalf("expected tests-fail, got %q (%v)", got.report.Reason, got.err)
			}
			if host.runs != 1 {
				t.Errorf("expected one run of the changed specs, got %d", host.runs)
			}
		})
	}
}

func TestChangedSpecsThatFailFarOverOnAHostThatNeverCalmsAreRefusedAsNotRun(t *testing.T) {
	host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), passedSpecs()}, loads: farOverAfterTheFirstRun(1<<30, false)}
	got := aSpecsRun(t, host)
	if got.report.Reason != application.ReasonTestsNotRun {
		t.Fatalf("expected tests-not-run, not tests-fail, for a host that never calmed, got %q (%v)", got.report.Reason, got.err)
	}
	if host.runs != 1 {
		t.Errorf("expected one run of the changed specs, got %d", host.runs)
	}
	if !strings.Contains(got.mail, "load 24.0 of 8 cores") {
		t.Errorf("expected the refusal to name the host's load, got:\n%s", got.mail)
	}
}
