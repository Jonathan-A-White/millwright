package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeTickLog is an in-memory application.TickLog: every line appended to it,
// in order. Its zero value is an empty log.
type FakeTickLog struct {
	mu    sync.Mutex
	lines []string

	// Err, when set, is returned by Append instead of doing the work, and
	// ReadErr by Read.
	Err     error
	ReadErr error
}

// FakeTickLog satisfies the port.
var _ application.TickLog = (*FakeTickLog)(nil)

// Append implements application.TickLog.
func (f *FakeTickLog) Append(_ context.Context, line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.lines = append(f.lines, line)
	return nil
}

// Read implements application.TickLog.
func (f *FakeTickLog) Read(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ReadErr != nil {
		return nil, f.ReadErr
	}
	return append([]string(nil), f.lines...), nil
}

// Lines is every line appended to the log, in order.
func (f *FakeTickLog) Lines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}
