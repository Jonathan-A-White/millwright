package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeUnitRestarter is an application.UnitRestarter that restarts nothing and
// remembers what it was asked: a scenario says which units are not running (so
// that a try-restart leaves them be), which fail to restart and which unit
// files changed on disk since the user manager read them.
type FakeUnitRestarter struct {
	// NotRunning names the units a try-restart finds stopped, or not installed.
	NotRunning map[string]bool
	// Fails names the units whose restart fails, with the error it fails with.
	Fails map[string]error
	// Stale names the units whose files changed on disk and that the user
	// manager has not reloaded: the first ReloadStale reloads them and clears it.
	Stale []string
	// ReloadErr is what a ReloadStale that has units to reload fails with.
	ReloadErr error
	// Asked is every unit a try-restart was asked for, in order.
	Asked []string
	// Reloaded is how many times a daemon-reload was run.
	Reloaded int
	// Events is every reload and restart asked, in order: "reload" and
	// "restart <unit>".
	Events []string
}

var _ application.UnitRestarter = (*FakeUnitRestarter)(nil)

// TryRestart implements application.UnitRestarter.
func (f *FakeUnitRestarter) TryRestart(_ context.Context, unit string) (bool, error) {
	f.Asked = append(f.Asked, unit)
	f.Events = append(f.Events, "restart "+unit)
	if err := f.Fails[unit]; err != nil {
		return false, err
	}
	return !f.NotRunning[unit], nil
}

// ReloadStale implements application.UnitRestarter.
func (f *FakeUnitRestarter) ReloadStale(_ context.Context) ([]string, error) {
	stale := f.Stale
	if len(stale) == 0 {
		return nil, nil
	}
	f.Reloaded++
	f.Events = append(f.Events, "reload")
	if f.ReloadErr != nil {
		return stale, f.ReloadErr
	}
	f.Stale = nil
	return stale, nil
}
