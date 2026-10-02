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
	// Readings, when it is not empty, is what the next reads say, one each in
	// turn; the last is said again for every read after. A scenario uses it to
	// say a host that is busy and then calms.
	Readings []application.LoadReading
	Err      error
	// Reads is how many times the load was asked for.
	Reads int
}

var _ application.HostLoad = (*FakeHostLoad)(nil)

// Load implements application.HostLoad.
func (f *FakeHostLoad) Load(context.Context) (application.LoadReading, error) {
	f.Reads++
	if len(f.Readings) > 0 {
		i := min(f.Reads, len(f.Readings)) - 1
		return f.Readings[i], f.Err
	}
	return f.Reading, f.Err
}
