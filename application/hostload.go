package application

import "context"

// HostLoad is how busy this host is, read when a story that may run on any host
// (domain.HostAuto) is about to be taken. It is a port so that a dispatch can be
// tested at a load a scenario names, without waiting for the machine to have it.
type HostLoad interface {
	Load(ctx context.Context) (LoadReading, error)
}

// LoadReading is the 1-minute load average of a host and the number of cores it
// has to spend it on.
type LoadReading struct {
	Load  float64
	Cores int
}

// Busy reports whether the load has reached the core count: every core has
// something waiting its turn.
func (r LoadReading) Busy() bool { return r.Cores > 0 && r.Load >= float64(r.Cores) }
