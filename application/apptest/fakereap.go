package apptest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeWindows satisfies the terminal a reaper watches.
var _ application.ReapTerminal = (*FakeWindows)(nil)

// add opens a window of the given id — a numbered one when the id is empty —
// with an idle pane. The caller holds the lock.
func (f *FakeWindows) add(id string, window application.Window) {
	if id == "" {
		f.nextID++
		id = fmt.Sprintf("@%d", f.nextID)
	}
	f.open = append(f.open, window)
	f.ids = append(f.ids, id)
	f.panes = append(f.panes, application.PaneIdle)
}

// index is where a window of that id is among the open ones, -1 when it is not
// open. The caller holds the lock.
func (f *FakeWindows) index(id string) int {
	for i, have := range f.ids {
		if have == id {
			return i
		}
	}
	return -1
}

// HoldsWithID puts a window in the terminal as if something else had opened
// it, under the id a test names it by, which an empty id leaves to the fake.
func (f *FakeWindows) HoldsWithID(id, name string, opened time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.add(id, application.Window{Name: name, Opened: opened})
}

// Pane says what the pane of an open window is doing.
func (f *FakeWindows) Pane(id string, state application.PaneState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.index(id)
	if i < 0 {
		return fmt.Errorf("no window %s is open", id)
	}
	f.panes[i] = state
	return nil
}

// RunsIn says which window the process under test is running in.
func (f *FakeWindows) RunsIn(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.here = id
}

// Vanish removes a window as if a person had closed it: it is not recorded as
// closed by anyone the test is watching.
func (f *FakeWindows) Vanish(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remove(id)
}

// remove drops a window from the open ones. The caller holds the lock.
func (f *FakeWindows) remove(id string) {
	i := f.index(id)
	if i < 0 {
		return
	}
	f.open = append(f.open[:i:i], f.open[i+1:]...)
	f.ids = append(f.ids[:i:i], f.ids[i+1:]...)
	f.panes = append(f.panes[:i:i], f.panes[i+1:]...)
}

// IDOf is the id of the open window of that name, and whether there is one.
func (f *FakeWindows) IDOf(name string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, window := range f.open {
		if window.Name == name {
			return f.ids[i], true
		}
	}
	return "", false
}

// IsOpen reports whether a window of that id is open.
func (f *FakeWindows) IsOpen(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.index(id) >= 0
}

// ClosedIDs is the id of every window Close closed, in order.
func (f *FakeWindows) ClosedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.closed...)
}

// OpenWindows implements application.ReapTerminal.
func (f *FakeWindows) OpenWindows(_ context.Context) ([]application.ReapWindow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	windows := make([]application.ReapWindow, len(f.open))
	for i, window := range f.open {
		windows[i] = application.ReapWindow{ID: f.ids[i], Name: window.Name, Opened: window.Opened}
	}
	return windows, nil
}

// ThisWindow implements application.ReapTerminal.
func (f *FakeWindows) ThisWindow(_ context.Context) (application.ReapWindow, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return application.ReapWindow{}, false, f.Err
	}
	i := f.index(f.here)
	if f.here == "" || i < 0 {
		return application.ReapWindow{}, false, nil
	}
	return application.ReapWindow{ID: f.ids[i], Name: f.open[i].Name, Opened: f.open[i].Opened}, true, nil
}

// PaneState implements application.ReapTerminal.
func (f *FakeWindows) PaneState(_ context.Context, id string) (application.PaneState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if f.PaneErr != nil {
		return "", f.PaneErr
	}
	i := f.index(id)
	if i < 0 {
		return "", fmt.Errorf("no window %s is open", id)
	}
	return f.panes[i], nil
}

// Close implements application.ReapTerminal.
func (f *FakeWindows) Close(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	if f.index(id) < 0 {
		return nil
	}
	f.remove(id)
	f.closed = append(f.closed, id)
	return nil
}

// FakeReapArmer is an in-memory application.ReapArmer: nothing is started, and
// a test reads back what would have been.
type FakeReapArmer struct {
	mu sync.Mutex

	// Armed is every reaper Arm was asked to start, in order.
	Armed []application.ReapArming

	// Err, when set, is returned by Arm instead of recording anything.
	Err error
}

// FakeReapArmer satisfies the port.
var _ application.ReapArmer = (*FakeReapArmer)(nil)

// Arm implements application.ReapArmer.
func (f *FakeReapArmer) Arm(_ context.Context, arming application.ReapArming) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Armed = append(f.Armed, arming)
	return nil
}
