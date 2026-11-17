package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1422: the wait for a calm host read the 1-minute load against the cores,
// which counts the factory's own part workers (mw check <story> --part N
// --part-worker --no-slot), run outside the gate lock by design. On the Laptop two
// of them held the load at 18 to 26 of 20, so every gate's wait ran to its bound
// and a batch then ended 'host still at load'. The calm wait takes their share off
// (PartWorkerLoad each); a host far over its cores on the raw reading is still
// refused (mw-gq6.903).

// thePartWorkersOnly is a host over its cores only by its two running part
// workers: 26 of 20, of which 8 is theirs.
func thePartWorkersOnly() application.LoadReading {
	return application.LoadReading{Load: 26, Cores: 20, PartWorkers: 2}
}

// noPartWorkers is the same load with no part worker running.
func noPartWorkers() application.LoadReading { return application.LoadReading{Load: 26, Cores: 20} }

// thePartWorkersAndMore is two part workers and other load over the cores besides.
func thePartWorkersAndMore() application.LoadReading {
	return application.LoadReading{Load: 34, Cores: 20, PartWorkers: 2}
}

// The poll and bound of every wait below: a wait to its bound is four polls.
const (
	partWorkersPoll  = 30 * time.Second
	partWorkersBound = 2 * time.Minute
)

// aCalmWait is one place a gate waits for a calm host, run on a host whose every
// reading is the one given.
type aCalmWait struct {
	name string
	run  func(t *testing.T, reading application.LoadReading) calmWaitRun
}

// calmWaitRun is how the wait went: the polls it waited, whether what it waited
// before was run, how it ended and what it printed.
type calmWaitRun struct {
	polls   int
	ran     bool
	err     error
	printed string
}

func theCalmWaits() []aCalmWait {
	return []aCalmWait{
		{"a close-out's gate", func(t *testing.T, reading application.LoadReading) calmWaitRun {
			run, checks := aCloseOutWhoseTestsGo(t, []bool{true}, []application.LoadReading{reading}, partWorkersBound)
			return calmWaitRun{polls: run.waits, ran: checks.runs > 0, err: run.err, printed: run.out.String()}
		}},
		{"a batch's gate", aBatchGateWaiting},
		{"the suite", aSuiteWaiting},
		{"a part check", func(t *testing.T, reading application.LoadReading) calmWaitRun {
			check, parts, said, waited, _ := partOnABusyHost(t, []application.LoadReading{reading}, partWorkersBound)
			_, err := check()
			return calmWaitRun{polls: int(*waited / partWorkersPoll), ran: len(parts.ran) > 0, err: err, printed: said.String()}
		}},
	}
}

// aBatchGateWaiting is aBatchGateAtLoad with its waits counted.
func aBatchGateWaiting(t *testing.T, reading application.LoadReading) calmWaitRun {
	t.Helper()
	checks := &scriptedChecks{passes: []bool{true}}
	var out bytes.Buffer
	polls := 0
	land := application.BatchLand{Next: application.Next{
		Tracker: apptest.NewFakeTracker(), Vault: newFakeVault(), Checks: checks,
		Load:      &apptest.FakeHostLoad{Reading: reading},
		LoadWait:  func(context.Context, time.Duration) error { polls++; return nil },
		LoadBound: partWorkersBound, LoadPoll: partWorkersPoll,
		Host: "laptop", Seat: "builder", Out: &out, Err: &out,
	}}
	report := &application.BatchReport{Rig: "lampas", Number: 552, Target: "main", Base: "origin/main",
		Merged: []application.BatchStory{{ID: "mw-x.1"}}}
	_, err, _ := land.GateTheBatchRun(context.Background(), t.TempDir(), report)
	return calmWaitRun{polls: polls, ran: checks.runs > 0, err: err, printed: out.String()}
}

// aSuiteWaiting is mw suite on a host whose every reading is the one given.
func aSuiteWaiting(t *testing.T, reading application.LoadReading) calmWaitRun {
	t.Helper()
	w := newSuiteBesideTheGateLock(t, nil)
	suite := w.suite()
	suite.Load = &apptest.FakeHostLoad{Reading: reading}
	suite.LoadPoll, suite.LoadBound = partWorkersPoll, partWorkersBound
	polls := 0
	suite.LoadWait = func(context.Context, time.Duration) error { polls++; return nil }
	_, err := suite.Run(context.Background(), "lampas")
	return calmWaitRun{polls: polls, ran: len(w.runner.Ran()) > 0, err: err, printed: w.told.String()}
}

func TestAGateDoesNotWaitOutTheLoadOfThePartWorkers(t *testing.T) {
	for _, wait := range theCalmWaits() {
		t.Run(wait.name, func(t *testing.T) {
			got := wait.run(t, thePartWorkersOnly())

			if got.polls > 1 {
				t.Errorf("expected the wait to end within one poll on a host over its cores only by its part workers, it waited %d polls:\n%s", got.polls, got.printed)
			}
			if !got.ran {
				t.Errorf("expected it to run, it did not (err %v):\n%s", got.err, got.printed)
			}
			if errors.As(got.err, new(application.HostStillBusyError)) || errors.As(got.err, new(application.HostBusyError)) {
				t.Errorf("expected no end for a busy host, got %v", got.err)
			}
			// The line says how much of the load was the part workers' (acceptance 5).
			for _, want := range []string{"load 26.0 of 20 cores", "8.0 of it its 2 running part workers' (4.0 each)", "18.0"} {
				if !strings.Contains(got.printed, want) {
					t.Errorf("expected the log to say %q, got:\n%s", want, got.printed)
				}
			}
		})
	}
}

func TestALoadThatIsNotThePartWorkersStillWaitsToItsBound(t *testing.T) {
	for _, wait := range theCalmWaits() {
		for _, c := range []struct {
			name    string
			reading application.LoadReading
		}{
			{"no part worker running", noPartWorkers()},
			{"part workers and other load over the cores", thePartWorkersAndMore()},
		} {
			t.Run(wait.name+", "+c.name, func(t *testing.T) {
				got := wait.run(t, c.reading)

				if got.polls != int(partWorkersBound/partWorkersPoll) {
					t.Errorf("expected the wait to run to its bound (4 polls), it waited %d:\n%s", got.polls, got.printed)
				}
				if c.reading.PartWorkers > 0 && !strings.Contains(got.printed, "8.0 of it its 2 running part workers' (4.0 each)") {
					t.Errorf("expected the wait's line to say the part workers' share, got:\n%s", got.printed)
				}
			})
		}
	}
}

func TestABatchWhoseHostIsBusyBeyondItsPartWorkersStillWaitsForTheNextLanding(t *testing.T) {
	got := aBatchGateWaiting(t, thePartWorkersAndMore())

	if got.ran || !errors.As(got.err, new(application.HostStillBusyError)) {
		t.Errorf("expected no run and a HostStillBusyError, got ran %v and %v", got.ran, got.err)
	}
}

// A host whose raw load is far over its cores is refused as it was, even when
// taking the part workers' share off would leave it calm: 9 of 4 with two part
// workers.
func TestAHostFarOverItsCoresWithThePartWorkersCountedIsStillRefused(t *testing.T) {
	far := application.LoadReading{Load: 9, Cores: 4, PartWorkers: 2}
	for _, wait := range []aCalmWait{
		theCalmWaits()[0],
		{"a batch's gate", aBatchGateWaiting},
	} {
		t.Run(wait.name, func(t *testing.T) {
			got := wait.run(t, far)

			// A close-out hands a HostBusyError back in its own words, which keep
			// the error's (Next.Run, isHostBusy).
			const hostBusy = "laptop was still at load 9.0 of 4 cores, at least 2 times them after waiting 2m"
			if got.ran || got.err == nil || !strings.Contains(got.err.Error(), hostBusy) {
				t.Errorf("expected no run and the HostBusyError %q, got ran %v and %v:\n%s", hostBusy, got.ran, got.err, got.printed)
			}
			if wait.name == "a batch's gate" && !errors.As(got.err, new(application.HostBusyError)) {
				t.Errorf("expected a batch's gate to end in a HostBusyError, got %v", got.err)
			}
		})
	}
}

func TestThePartWorkersShareIsTakenOffForBusyOnly(t *testing.T) {
	for _, c := range []struct {
		reading                  application.LoadReading
		busy, farOver, overCores bool
	}{
		{thePartWorkersOnly(), false, false, true},
		{noPartWorkers(), true, false, true},
		{thePartWorkersAndMore(), true, false, true},
		{application.LoadReading{Load: 9, Cores: 4, PartWorkers: 2}, true, true, true},
		{application.LoadReading{Load: 3, Cores: 20, PartWorkers: 2}, false, false, false},
		{application.LoadReading{Load: 26, PartWorkers: 2}, false, false, false},
	} {
		r := c.reading
		if r.Busy() != c.busy || r.FarOver() != c.farOver || r.OverCores() != c.overCores {
			t.Errorf("%+v: expected busy %v, far over %v, over its cores %v; got %v, %v, %v",
				r, c.busy, c.farOver, c.overCores, r.Busy(), r.FarOver(), r.OverCores())
		}
	}
}
