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
//
// It is also the application.ReapTerminal of the same terminal (fakereap.go),
// as the real tmux adapter is both: a window it opens has an id and a pane a
// test can say what state is in.
type FakeWindows struct {
	mu   sync.Mutex
	open []application.Window
	// ids and panes are what fakereap.go says of each open window, by index
	// into open: its id, and what its pane is doing.
	ids   []string
	panes []application.PaneState
	// here is the id of the window the process under test runs in, "" for none.
	here string
	// nextID numbers the windows that are given no id of their own.
	nextID int
	// closed is the id of every window Close has closed.
	closed []string
	// typed is every call Type has made, in order.
	typed []typedInto

	// Opened is every spec Open was asked for, in the order it was asked.
	Opened []application.WindowSpec

	// Err, when set, is returned by every method instead of doing the work.
	Err error

	// PaneErr, when set, is returned by PaneState alone: a terminal that lists
	// its windows but cannot say what a pane is doing.
	PaneErr error

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
	f.HoldsWithID("", name, opened)
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
	f.add("", application.Window{Name: spec.Name, Opened: f.At})
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
