package apptest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeWindows is an in-memory application.Windows. No terminal is touched:
// a test seeds the windows it wants open with Holds and reads back what would
// have been started, so that a use case can be walked through starting a seat
// without a session anywhere.
type FakeWindows struct {
	mu   sync.Mutex
	open []application.Window

	// Opened is every spec Open was asked for, in the order it was asked.
	Opened []application.WindowSpec

	// Err, when set, is returned by every method instead of doing the work.
	Err error

	// At is what a window opened by Open is recorded as having been opened at.
	At time.Time
}

// NewFakeWindows returns a terminal with no windows open in it.
func NewFakeWindows() *FakeWindows {
	return &FakeWindows{}
}

// FakeWindows satisfies the port.
var _ application.Windows = (*FakeWindows)(nil)

// Holds puts a window in the terminal as if something else had opened it.
func (f *FakeWindows) Holds(name string, opened time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open = append(f.open, application.Window{Name: name, Opened: opened})
}

// Open implements application.Windows.
func (f *FakeWindows) Open(_ context.Context, spec application.WindowSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	for _, window := range f.open {
		if window.Name == spec.Name {
			return fmt.Errorf("a window named %q is already open", spec.Name)
		}
	}
	f.Opened = append(f.Opened, spec)
	f.open = append(f.open, application.Window{Name: spec.Name, Opened: f.At})
	return nil
}

// List implements application.Windows.
func (f *FakeWindows) List(_ context.Context) ([]application.Window, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	open := make([]application.Window, len(f.open))
	copy(open, f.open)
	return open, nil
}
