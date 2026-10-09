package apptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeCloud is an application.CloudProvider that makes nothing: it records
// each call as "up <name>" or "down <name>", fails the first FailUps ups, and
// reports BilledUSD as the provider's billing when BilledKnown.
type FakeCloud struct {
	mu          sync.Mutex
	calls       []string
	FailUps     int
	BilledUSD   float64
	BilledKnown bool
}

var _ application.CloudProvider = (*FakeCloud)(nil)

// Up implements application.CloudProvider.
func (f *FakeCloud) Up(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "up "+name)
	if f.FailUps > 0 {
		f.FailUps--
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
