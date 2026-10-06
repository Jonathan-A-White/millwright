package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeNetworkProbe is an application.NetworkProbe that says what it was told:
// the connection Windows reports, or an error when it could not be asked.
type FakeNetworkProbe struct {
	Condition application.NetworkCondition
	Err       error
	// Probes is how many times Windows was asked.
	Probes int
}

var _ application.NetworkProbe = (*FakeNetworkProbe)(nil)

// Probe implements application.NetworkProbe.
func (f *FakeNetworkProbe) Probe(context.Context) (application.NetworkCondition, error) {
	f.Probes++
	return f.Condition, f.Err
}

// FakeNetworkStore is an in-memory application.NetworkStore.
type FakeNetworkStore struct {
	mu     sync.Mutex
	Memory application.NetworkMemory
	Saves  int
}

var _ application.NetworkStore = (*FakeNetworkStore)(nil)

// Load implements application.NetworkStore.
func (f *FakeNetworkStore) Load(context.Context) (application.NetworkMemory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Memory, nil
}

// Save implements application.NetworkStore.
func (f *FakeNetworkStore) Save(_ context.Context, memory application.NetworkMemory) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Memory = memory
	f.Saves++
	return nil
}

// FakeNetwork is an application.NetworkReader that says one thing: metered or
// not, as a scenario names it.
type FakeNetwork struct {
	Reading application.NetworkReading
	// Reads is how many times the network was asked about.
	Reads int
}

var _ application.NetworkReader = (*FakeNetwork)(nil)

// Metered is a FakeNetwork that reads as a metered Windows connection.
func Metered() *FakeNetwork {
	return &FakeNetwork{Reading: application.NetworkReading{Metered: true, Source: application.NetworkWindows, Profile: "Whitehouse", Cost: "Fixed"}}
}

// Unmetered is a FakeNetwork that reads as an unmetered one.
func Unmetered() *FakeNetwork {
	return &FakeNetwork{Reading: application.NetworkReading{Source: application.NetworkWindows, Profile: "Whitehouse", Cost: "Unrestricted"}}
}

// Read implements application.NetworkReader.
func (f *FakeNetwork) Read(context.Context) application.NetworkReading {
	f.Reads++
	return f.Reading
}
