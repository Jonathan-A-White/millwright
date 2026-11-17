package application_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1438: the wait for room before the run of the failing tests on main
// alone decided on the load with the factory's own part workers counted, so a
// host held over its cores only by them was 'not made' at once.

// aHostHeldOnlyByPartWorkers reads 26 of 20 cores with 2 part workers running
// (their 8.0 taken off, 18.0 is under the cores), at every reading.
type aHostHeldOnlyByPartWorkers struct{}

func (aHostHeldOnlyByPartWorkers) Load(context.Context) (application.LoadReading, error) {
	return application.LoadReading{Load: 26, Cores: 20, PartWorkers: 2}, nil
}

func TestTheRunOnMainAloneIsMadeWhenOnlyThePartWorkersHoldTheHostOverItsCores(t *testing.T) {
	lock := &aWatchedGateLock{}
	watch := &aCalmWatch{lock: lock}
	named := &namedTestsThat{run: func(context.Context, []string) (application.NamedRun, error) {
		return passedOnMain(), nil
	}}

	report, _ := aCloseOutWatched(t, lock, watch, twoFailingTests{}, aHostHeldOnlyByPartWorkers{}, named)

	if len(named.asked) != 1 {
		t.Fatalf("expected the run on main alone to be made once, got %d runs; notes %q", len(named.asked), report.Notes)
	}
}
