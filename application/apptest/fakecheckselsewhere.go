package apptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeChecksElsewhere is an in-memory application.CheckElsewhere: a test says
// what each host prints when mw check is run there, or that it fails, and reads
// back which hosts were asked and with which flags. A host it was told nothing
// of cannot be reached.
type FakeChecksElsewhere struct {
	mu    sync.Mutex
	out   map[string]string
	fails map[string]error
	calls []CheckCall
}

// CheckCall is one run of mw check on a host.
type CheckCall struct {
	Host, Story string
	Flags       []string
}

// NewFakeChecksElsewhere returns a remote that reaches no host.
func NewFakeChecksElsewhere() *FakeChecksElsewhere {
	return &FakeChecksElsewhere{out: map[string]string{}, fails: map[string]error{}}
}

var _ application.CheckElsewhere = (*FakeChecksElsewhere)(nil)

// Prints makes mw check on host succeed and print out.
func (f *FakeChecksElsewhere) Prints(host, out string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out[host] = out
	delete(f.fails, host)
}

// Fails makes mw check on host fail with err.
func (f *FakeChecksElsewhere) Fails(host string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails[host] = err
}

// Calls is every run asked of a host, oldest first.
func (f *FakeChecksElsewhere) Calls() []CheckCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CheckCall(nil), f.calls...)
}

// CheckThere implements application.CheckElsewhere.
func (f *FakeChecksElsewhere) CheckThere(_ context.Context, host, storyID string, flags []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, CheckCall{Host: host, Story: storyID, Flags: append([]string(nil), flags...)})
	if err, ok := f.fails[host]; ok {
		return f.out[host], err
	}
	if out, ok := f.out[host]; ok {
		return out, nil
	}
	return "", fmt.Errorf("no way to reach %s", host)
}
