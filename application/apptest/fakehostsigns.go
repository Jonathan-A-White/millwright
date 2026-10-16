package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeHostSigns is an in-memory application.HostSigns: the newest sign of
// life, by host, that a test says the vault holds of it.
type FakeHostSigns struct {
	mu    sync.Mutex
	signs map[string]application.HostSign

	// Err, when set, is returned instead of looking.
	Err error
}

// FakeHostSigns satisfies the port.
var _ application.HostSigns = (*FakeHostSigns)(nil)

// NewFakeHostSigns is a vault holding no sign of any host.
func NewFakeHostSigns() *FakeHostSigns {
	return &FakeHostSigns{signs: map[string]application.HostSign{}}
}

// Set records the newest sign of life of a host.
func (f *FakeHostSigns) Set(host string, sign application.HostSign) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signs[host] = sign
}

// LastSign implements application.HostSigns.
func (f *FakeHostSigns) LastSign(_ context.Context, host string) (application.HostSign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return application.HostSign{}, f.Err
	}
	return f.signs[host], nil
}
