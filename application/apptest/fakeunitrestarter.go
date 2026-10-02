package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeUnitRestarter is an application.UnitRestarter that restarts nothing and
// remembers what it was asked: a scenario says which units are not running (so
// that a try-restart leaves them be) and which fail to restart.
type FakeUnitRestarter struct {
	// NotRunning names the units a try-restart finds stopped, or not installed.
	NotRunning map[string]bool
	// Fails names the units whose restart fails, with the error it fails with.
	Fails map[string]error
	// Asked is every unit a try-restart was asked for, in order.
	Asked []string
}

var _ application.UnitRestarter = (*FakeUnitRestarter)(nil)

// TryRestart implements application.UnitRestarter.
func (f *FakeUnitRestarter) TryRestart(_ context.Context, unit string) (bool, error) {
	f.Asked = append(f.Asked, unit)
	if err := f.Fails[unit]; err != nil {
		return false, err
	}
	return !f.NotRunning[unit], nil
}
