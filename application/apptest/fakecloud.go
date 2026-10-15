package apptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeCloud is an application.CloudProvider that makes nothing: it records
// each Up and Down call as "up <name>" or "down <name>", fails the first FailUps ups, and
// reports BilledUSD as the provider's billing when BilledKnown.
type FakeCloud struct {
	mu      sync.Mutex
	calls   []string
	FailUps int
	// FailUpWith is the error the failing ups fail with, else "did not come up".
	FailUpWith error
	// Orphans are the instances Vultr holds that no state knows: Down does not
	// find them, and Reap destroys them. ReapErr, when set, is why it cannot.
	Orphans     []application.CloudInstance
	ReapErr     error
	BilledUSD   float64
	BilledKnown bool
	begun       []string
}

var _ application.CloudProvider = (*FakeCloud)(nil)

// Begin implements application.CloudProvider: it only notes the box, which
// TakeBegun hands to the scenario to make, as the process it detaches would.
func (f *FakeCloud) Begin(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begun = append(f.begun, name)
	return nil
}

// TakeBegun are the boxes begun and not yet made, which it forgets.
func (f *FakeCloud) TakeBegun() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	begun := f.begun
	f.begun = nil
	return begun
}

// Up implements application.CloudProvider.
func (f *FakeCloud) Up(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "up "+name)
	if f.FailUps > 0 {
		f.FailUps--
		if f.FailUpWith != nil {
			return f.FailUpWith
		}
		return fmt.Errorf("%s did not come up", name)
	}
	return nil
}

// Down implements application.CloudProvider.
func (f *FakeCloud) Down(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "down "+name)
	return nil
}

// Billed implements application.CloudProvider.
func (f *FakeCloud) Billed(context.Context, []string) (float64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.BilledUSD, f.BilledKnown, nil
}

// Instances implements application.CloudProvider.
func (f *FakeCloud) Instances(context.Context) ([]application.CloudInstance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]application.CloudInstance(nil), f.Orphans...), nil
}

// Reap implements application.CloudProvider: it records "reap <label>".
func (f *FakeCloud) Reap(_ context.Context, instance application.CloudInstance) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "reap "+instance.Label)
	if f.ReapErr != nil {
		return f.ReapErr
	}
	for i, o := range f.Orphans {
		if o.ID == instance.ID {
			f.Orphans = append(f.Orphans[:i], f.Orphans[i+1:]...)
			break
		}
	}
	return nil
}

// Calls are the ups and downs asked for, in order.
func (f *FakeCloud) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// FakeCloudBook is an in-memory application.CloudBook: State is what it
// holds, and Whys what each Write said.
type FakeCloudBook struct {
	State application.CloudState
	Whys  []string
}

var _ application.CloudBook = (*FakeCloudBook)(nil)

// Read implements application.CloudBook.
func (f *FakeCloudBook) Read(context.Context) (application.CloudState, error) {
	return f.State, nil
}

// Write implements application.CloudBook.
func (f *FakeCloudBook) Write(_ context.Context, state application.CloudState, why string) error {
	f.State = state
	f.Whys = append(f.Whys, why)
	return nil
}

// FakeCloudNotice is an application.CloudNotice that keeps the cards it is
// asked to send.
type FakeCloudNotice struct {
	mu    sync.Mutex
	Cards []string
}

var _ application.CloudNotice = (*FakeCloudNotice)(nil)

// Tell implements application.CloudNotice.
func (f *FakeCloudNotice) Tell(_ context.Context, title, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cards = append(f.Cards, title+": "+text)
	return nil
}
