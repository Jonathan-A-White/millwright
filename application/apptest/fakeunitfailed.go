package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.UnitSystem   = (*FakeUnitSystem)(nil)
	_ application.UnitFailures = (*FakeUnitFailures)(nil)
)

// FakeUnitSystem is an in-memory application.UnitSystem: which units a timer
// runs, what their journals end with, and when each last ran clean.
type FakeUnitSystem struct {
	mu       sync.Mutex
	timers   map[string]bool
	journals map[string]string
	clean    map[string][]time.Time
}

// NewFakeUnitSystem is a system with no unit run by a timer.
func NewFakeUnitSystem() *FakeUnitSystem {
	return &FakeUnitSystem{timers: map[string]bool{}, journals: map[string]string{}, clean: map[string][]time.Time{}}
}

// SetTimer says whether a timer runs the unit.
func (f *FakeUnitSystem) SetTimer(unit string, run bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.timers[unit] = run
}

// SetJournal sets what the unit's journal ends with.
func (f *FakeUnitSystem) SetJournal(unit, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.journals[unit] = text
}

// RanClean notes that the unit had a run that ended clean at the given time.
func (f *FakeUnitSystem) RanClean(unit string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clean[unit] = append(f.clean[unit], at)
}

// RunByTimer implements application.UnitSystem.
func (f *FakeUnitSystem) RunByTimer(_ context.Context, unit string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.timers[unit], nil
}

// JournalTail implements application.UnitSystem.
func (f *FakeUnitSystem) JournalTail(_ context.Context, unit string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.journals[unit], nil
}

// RanCleanSince implements application.UnitSystem.
func (f *FakeUnitSystem) RanCleanSince(_ context.Context, unit string, since time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, at := range f.clean[unit] {
		if at.After(since) {
			return true, nil
		}
	}
	return false, nil
}

// FakeUnitFailures is an in-memory application.UnitFailures.
type FakeUnitFailures struct {
	mu       sync.Mutex
	failures map[string]application.UnitFailure
}

// NewFakeUnitFailures keeps no failure for any unit.
func NewFakeUnitFailures() *FakeUnitFailures {
	return &FakeUnitFailures{failures: map[string]application.UnitFailure{}}
}

// Load implements application.UnitFailures.
func (f *FakeUnitFailures) Load(_ context.Context, unit string) (application.UnitFailure, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	failure, ok := f.failures[unit]
	return failure, ok, nil
}

// Save implements application.UnitFailures.
func (f *FakeUnitFailures) Save(_ context.Context, unit string, failure application.UnitFailure) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[unit] = failure
	return nil
}

// Clear implements application.UnitFailures.
func (f *FakeUnitFailures) Clear(_ context.Context, unit string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.failures, unit)
	return nil
}
