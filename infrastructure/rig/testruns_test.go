package rig_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// aHostWithTestRuns is a host whose test-runner slots are kept in a directory of
// the test's own, on a clock the test turns. Each time a start waits, it passes
// one poll of fake time and then lets the first of the runs it holds end: so a
// start that waits is seen to wait, and to start only after one of them ended.
type aHostWithTestRuns struct {
	runs    *rig.TestRuns
	now     time.Time
	sleeps  int
	holding []func()
	// atWait is what each wait found of the work the start was to do.
	atWait []bool
	marker string
}

func newHostWithTestRuns(t *testing.T, limit int) *aHostWithTestRuns {
	t.Helper()
	h := &aHostWithTestRuns{now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC), marker: filepath.Join(t.TempDir(), "started")}
	h.runs = rig.NewTestRuns(t.TempDir(), limit, rig.WithTestRunsClock(
		func() time.Time { return h.now },
		func(ctx context.Context, d time.Duration) error {
			h.sleeps++
			h.now = h.now.Add(d)
			_, err := os.Stat(h.marker)
			h.atWait = append(h.atWait, err == nil)
			if len(h.holding) > 0 {
				h.holding[0]()
				h.holding = h.holding[1:]
			}
			return ctx.Err()
		}))
	return h
}

// run holds n slots, as n runners that are going.
func (h *aHostWithTestRuns) running(t *testing.T, holders ...string) {
	t.Helper()
	for _, who := range holders {
		release, err := h.runs.Take(context.Background(), who)
		if err != nil {
			t.Fatalf("%s taking a slot: %v", who, err)
		}
		h.holding = append(h.holding, release)
	}
}

// The three kinds of start a host makes of a test runner, each running the one
// command that marks the work as begun.
func (h *aHostWithTestRuns) kinds(t *testing.T) map[string]func(context.Context) error {
	touch := "touch " + h.marker
	work := t.TempDir()
	return map[string]func(context.Context) error{
		"a gate part": func(ctx context.Context) error {
			_, err := rig.NewChecks(rig.WithTestRuns(h.runs)).RunLine(ctx, "lampas", work, touch)
			return err
		},
		"a Builder's test run through mw": func(ctx context.Context) error {
			_, err := rig.NewChecks(rig.WithCommand(touch), rig.WithTestRuns(h.runs)).Run(ctx, "lampas", work)
			return err
		},
		"an after-landing command": func(ctx context.Context) error {
			_, err := rig.NewAfterLanding(rig.WithAfterCommands(map[string]string{"lampas": touch}), rig.WithAfterTestRuns(h.runs)).Run(ctx, "lampas", work)
			return err
		},
	}
}

func TestTheHostsTestRunnerLimit(t *testing.T) {
	t.Run("the start after the limit waits until one of the runs ends, naming who it waits for", func(t *testing.T) {
		for name := range newHostWithTestRuns(t, 2).kinds(t) {
			t.Run(name, func(t *testing.T) {
				h := newHostWithTestRuns(t, 2)
				h.running(t, "gate of lampas", "after-landing of postern")
				var said bytes.Buffer
				ctx := application.WithRunProgress(context.Background(), &said)

				if err := h.kinds(t)[name](ctx); err != nil {
					t.Fatalf("starting the runner: %v", err)
				}

				if h.sleeps == 0 {
					t.Fatal("the start after the limit did not wait: both slots were held and it started at once")
				}
				for _, begun := range h.atWait {
					if begun {
						t.Error("the work had begun while a wait was still going on")
					}
				}
				if _, err := os.Stat(h.marker); err != nil {
					t.Errorf("the runner never started once a run had ended: %v", err)
				}
				for _, want := range []string{"held by gate of lampas, since 12:00:00Z", "held by after-landing of postern, since 12:00:00Z", "builder_test_runs"} {
					if !strings.Contains(said.String(), want) {
						t.Errorf("the wait did not say %q:\n%s", want, said.String())
					}
				}
			})
		}
	})

	t.Run("a start under the limit goes at once", func(t *testing.T) {
		for name := range newHostWithTestRuns(t, 2).kinds(t) {
			t.Run(name, func(t *testing.T) {
				h := newHostWithTestRuns(t, 2)
				h.running(t, "gate of lampas")
				if err := h.kinds(t)[name](context.Background()); err != nil {
					t.Fatalf("starting the runner: %v", err)
				}
				if h.sleeps != 0 {
					t.Errorf("a start with a slot free waited %d times", h.sleeps)
				}
			})
		}
	})

	t.Run("with the limit unset or 0 every start goes at once, as it did", func(t *testing.T) {
		for _, limit := range []int{0, -1} {
			for name := range newHostWithTestRuns(t, limit).kinds(t) {
				t.Run(name, func(t *testing.T) {
					h := newHostWithTestRuns(t, limit)
					h.running(t, "a", "b", "c", "d")
					if err := h.kinds(t)[name](context.Background()); err != nil {
						t.Fatalf("starting the runner: %v", err)
					}
					if h.sleeps != 0 {
						t.Errorf("a start with no limit waited %d times", h.sleeps)
					}
				})
			}
		}
		// No runs at all: the adapter built without a limit.
		work := t.TempDir()
		if _, err := rig.NewChecks(rig.WithCommand("true")).Run(context.Background(), "lampas", work); err != nil {
			t.Errorf("a run with no slots configured: %v", err)
		}
		if _, err := rig.NewChecks(rig.WithCommand("true"), rig.WithTestRuns(nil)).Run(context.Background(), "lampas", work); err != nil {
			t.Errorf("a run with the limit off: %v", err)
		}
	})

	t.Run("a slot is given back when the run ends, whatever way it ends", func(t *testing.T) {
		h := newHostWithTestRuns(t, 1)
		checks := rig.NewChecks(rig.WithCommand("exit 3"), rig.WithTestRuns(h.runs))
		checked, err := checks.Run(context.Background(), "lampas", t.TempDir())
		if err != nil || checked.Passed {
			t.Fatalf("expected a red run, got %+v, %v", checked, err)
		}
		release, err := h.runs.Take(context.Background(), "the next")
		if err != nil {
			t.Fatalf("the slot was not given back: %v", err)
		}
		release()
		if h.sleeps != 0 {
			t.Errorf("the next start waited %d times for a slot a finished run had", h.sleeps)
		}
	})

	t.Run("a wait that is told to stop ends with that, and takes no slot", func(t *testing.T) {
		h := newHostWithTestRuns(t, 1)
		h.running(t, "gate of lampas")
		h.holding = nil // the holder never ends
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := rig.NewChecks(rig.WithCommand("touch "+h.marker), rig.WithTestRuns(h.runs)).Run(ctx, "lampas", t.TempDir())
		if err == nil {
			t.Fatal("a cancelled wait for a slot went on to run")
		}
		if _, err := os.Stat(h.marker); err == nil {
			t.Error("a cancelled wait ran the command")
		}
	})
}

// aWaiterAtTheSlots is one start waiting for a test-runner slot, as a mw of its
// own would be: its own TestRuns on the host's shared directory, whose every
// wait stops at a step the test lets go of.
type aWaiterAtTheSlots struct {
	waiting chan struct{} // told each time the start has looked, found no slot, and is about to wait
	go_     chan struct{} // the test lets the wait end
	done    chan error    // the start's own result, once it has a slot
	release func()
}

func startAWaiter(t *testing.T, dir, name string, at time.Time) *aWaiterAtTheSlots {
	t.Helper()
	w := &aWaiterAtTheSlots{waiting: make(chan struct{}, 100), go_: make(chan struct{}), done: make(chan error, 1)}
	runs := rig.NewTestRuns(dir, 1, rig.WithTestRunsClock(
		func() time.Time { return at },
		func(ctx context.Context, d time.Duration) error {
			w.waiting <- struct{}{}
			select {
			case <-w.go_:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}))
	go func() {
		release, err := runs.Take(context.Background(), name)
		w.release = release
		w.done <- err
	}()
	return w
}

func (w *aWaiterAtTheSlots) lookedAndWaited(t *testing.T, what string) {
	t.Helper()
	select {
	case <-w.waiting:
	case err := <-w.done:
		t.Fatalf("%s: the start took a slot (%v) when it should have waited", what, err)
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: the start never looked at the slots", what)
	}
}

func TestATestRunnerSlotIsGivenToTheOldestWaiter(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	holder, err := rig.NewTestRuns(dir, 1, rig.WithTestRunsClock(func() time.Time { return start }, nil)).Take(context.Background(), "a run going")
	if err != nil {
		t.Fatalf("taking the one slot: %v", err)
	}

	first := startAWaiter(t, dir, "the first to wait", start.Add(time.Second))
	first.lookedAndWaited(t, "the first")
	second := startAWaiter(t, dir, "the second to wait", start.Add(2*time.Second))
	second.lookedAndWaited(t, "the second")

	// The slot is free, and the later waiter looks at it first.
	holder()
	second.go_ <- struct{}{}
	select {
	case err := <-second.done:
		t.Fatalf("the second to wait took the slot ahead of the first (%v)", err)
	case <-second.waiting:
	case <-time.After(10 * time.Second):
		t.Fatal("the second start neither took a slot nor waited again")
	}

	// The first looks and starts.
	first.go_ <- struct{}{}
	select {
	case err := <-first.done:
		if err != nil {
			t.Fatalf("the first to wait: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the first to wait did not take the free slot when it looked")
	}

	// Then, its run over, the second does.
	first.release()
	second.go_ <- struct{}{}
	select {
	case err := <-second.done:
		if err != nil {
			t.Fatalf("the second to wait: %v", err)
		}
		second.release()
	case <-time.After(10 * time.Second):
		t.Fatal("the second to wait did not take the slot once the first's run ended")
	}
}
