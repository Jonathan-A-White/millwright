package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeUserProcesses is an in-memory application.UserProcesses: a process table
// that holds what it is given and drops what it is asked to stop.
type FakeUserProcesses struct {
	mu      sync.Mutex
	table   []application.Process
	stopped []int

	// Err, when set, is returned by List; StopErr by Stop.
	Err, StopErr error
}

// FakeUserProcesses satisfies the port.
var _ application.UserProcesses = (*FakeUserProcesses)(nil)

// NewFakeUserProcesses is an empty process table.
func NewFakeUserProcesses() *FakeUserProcesses { return &FakeUserProcesses{} }

// Start puts a process alive in the table.
func (f *FakeUserProcesses) Start(p application.Process) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.table = append(f.table, p)
}

// Alive is the process ids still in the table.
func (f *FakeUserProcesses) Alive() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	var pids []int
	for _, p := range f.table {
		pids = append(pids, p.Pid)
	}
	return pids
}

// Stopped is the process ids Stop was asked to end, in order.
func (f *FakeUserProcesses) Stopped() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.stopped...)
}

// List implements application.UserProcesses.
func (f *FakeUserProcesses) List(context.Context) ([]application.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]application.Process(nil), f.table...), nil
}

// Stop implements application.UserProcesses.
func (f *FakeUserProcesses) Stop(_ context.Context, pid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StopErr != nil {
		return f.StopErr
	}
	f.stopped = append(f.stopped, pid)
	kept := f.table[:0]
	for _, p := range f.table {
		if p.Pid != pid {
			kept = append(kept, p)
		}
	}
	f.table = kept
	return nil
}
