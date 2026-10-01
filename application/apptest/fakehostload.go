package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeHostLoad is an application.HostLoad that says what it was told: the load
// and core count of a host, as a scenario names them, or an error when the load
// could not be read.
type FakeHostLoad struct {
	Reading application.LoadReading
	Err     error
	// Reads is how many times the load was asked for.
	Reads int
}

var _ application.HostLoad = (*FakeHostLoad)(nil)

// Load implements application.HostLoad.
func (f *FakeHostLoad) Load(context.Context) (application.LoadReading, error) {
	f.Reads++
	return f.Reading, f.Err
}
