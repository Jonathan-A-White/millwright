package application_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1064: the run of the failing tests on main alone that fails while the host
// is far over its cores is the host's more than main's, so it is run once more
// when the host has calmed, as a gate and a part of a check are.

// aHostBusyDuringTheRunOnMain is calm (load 2 of 8 cores) until a run on main
// alone is made; fills[i] is the load the host is at once run i has ended (0 for
// calm), and each wait for it brings it back to calm unless neverCalms.
type aHostBusyDuringTheRunOnMain struct {
	mu         sync.Mutex
	load       float64
	neverCalms bool
	fills      []float64
	results    []application.NamedRun
	asked      [][]string
}

func (h *aHostBusyDuringTheRunOnMain) Load(context.Context) (application.LoadReading, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.load == 0 {
		return application.LoadReading{Load: 2, Cores: 8}, nil
	}
	return application.LoadReading{Load: h.load, Cores: 8}, nil
}

func (h *aHostBusyDuringTheRunOnMain) wait(context.Context, time.Duration) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.neverCalms {
		h.load = 0
	}
	return nil
}

func (h *aHostBusyDuringTheRunOnMain) RunNamed(_ context.Context, _, _ string, tests []string) (application.NamedRun, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	i := len(h.asked)
	h.asked = append(h.asked, tests)
	if i < len(h.fills) {
		h.load = h.fills[i]
	}
	return h.results[min(i, len(h.results)-1)], nil
}

func failedOnMain(output string) application.NamedRun {
	return application.NamedRun{Checked: application.Checked{Command: "go test -run", Output: output}, Ran: true}
}

func passedOnMain() application.NamedRun {
	return application.NamedRun{Checked: application.Checked{Command: "go test -run", Passed: true}, Ran: true}
}

func aStopWithMainRunOn(t *testing.T, h *aHostBusyDuringTheRunOnMain) closeOutOnMain {
	t.Helper()
	return aTestsFailStopWaiting(t, h, h, 0, nil, h.wait)
}

const bothFailOnMain = "--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n"

func TestARunOnMainThatFailsFarOverTheCoresIsRunAgainWhenTheHostCalms(t *testing.T) {
	h := &aHostBusyDuringTheRunOnMain{
		fills:   []float64{20},
		results: []application.NamedRun{failedOnMain(bothFailOnMain), passedOnMain()},
	}
	got := aStopWithMainRunOn(t, h)

	if len(h.asked) != 2 {
		t.Fatalf("expected the tests run on main twice, got %q", h.asked)
	}
	if got.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected main not called red, the story stopping for tests-fail, got %q (%v)", got.report.Reason, got.err)
	}
	for _, want := range []string{"TestA: branch only", "TestB: branch only"} {
		if !strings.Contains(got.mail, want) {
			t.Errorf("expected the mail to say %q, got:\n%s", want, got.mail)
		}
	}
	if !strings.Contains(strings.Join(got.report.Notes, "\n"), "ran twice") {
		t.Errorf("expected a note that the run on main was made twice, got %q", got.report.Notes)
	}
}

func TestARunOnMainThatFailsAgainOnACalmHostIsMainsRedOnTheSecondRun(t *testing.T) {
	h := &aHostBusyDuringTheRunOnMain{
		fills: []float64{20},
		results: []application.NamedRun{
			failedOnMain("--- FAIL: TestB (0.01s)\nFAIL\n"),
			failedOnMain("--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n"),
		},
	}
	got := aStopWithMainRunOn(t, h)

	if len(h.asked) != 2 {
		t.Fatalf("expected the tests run on main twice, got %q", h.asked)
	}
	if got.report.Reason != application.ReasonMainRed {
		t.Fatalf("expected the story held on main's red, got %q (%v)", got.report.Reason, got.err)
	}
	if !strings.Contains(got.mail, "TestA: fails on main too") || !strings.Contains(got.mail, "TestB: fails on main too") {
		t.Errorf("expected the second run's failing tests to stand, got:\n%s", got.mail)
	}
	if !strings.Contains(strings.Join(got.report.Notes, "\n"), "ran twice") {
		t.Errorf("expected a note that the run on main was made twice, got %q", got.report.Notes)
	}
}

func TestARunOnMainThatFailsOnACalmHostOrOnlyABusyOneIsNotRunAgain(t *testing.T) {
	for name, fill := range map[string]float64{"calm": 0, "busy, not far over": 12} {
		t.Run(name, func(t *testing.T) {
			h := &aHostBusyDuringTheRunOnMain{
				fills:   []float64{fill},
				results: []application.NamedRun{failedOnMain(bothFailOnMain), passedOnMain()},
			}
			got := aStopWithMainRunOn(t, h)
			if len(h.asked) != 1 {
				t.Fatalf("expected one run on main, got %q", h.asked)
			}
			if got.report.Reason != application.ReasonMainRed {
				t.Fatalf("expected main's red as before, got %q (%v)", got.report.Reason, got.err)
			}
			if strings.Contains(strings.Join(got.report.Notes, "\n"), "ran twice") {
				t.Errorf("expected no note of a second run, got %q", got.report.Notes)
			}
		})
	}
}

func TestARunOnMainThatFailsFarOverTheCoresOnAHostThatNeverCalmsIsNotCalledRed(t *testing.T) {
	h := &aHostBusyDuringTheRunOnMain{
		neverCalms: true,
		fills:      []float64{20},
		results:    []application.NamedRun{failedOnMain(bothFailOnMain)},
	}
	got := aStopWithMainRunOn(t, h)

	if len(h.asked) != 1 {
		t.Fatalf("expected no second run on a host that never calmed, got %q", h.asked)
	}
	if got.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected main not called red, got %q (%v)", got.report.Reason, got.err)
	}
	for _, want := range []string{"TestA: not run: ", "TestB: not run: ", "load 20.0 of 8 cores"} {
		if !strings.Contains(got.mail, want) {
			t.Errorf("expected the mail to say %q, got:\n%s", want, got.mail)
		}
	}
	if strings.Contains(got.mail, application.FailsOnMainToo) {
		t.Errorf("expected no test said to fail on main, got:\n%s", got.mail)
	}
}
