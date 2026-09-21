package application

import (
	"context"
	"errors"
	"time"
)

// NetworkFaultExit is the status mw dispatch leaves with when the sync it must
// do first could not resolve a name, even after waiting: this host's own
// network is not back yet, so nothing was claimed. It is a status of its own on
// purpose — 1 is any plain failure, 2 to 4 are beads' and 5 and 6 are other
// waits — so that a timer reading nothing but the number can tell "the network
// is still asleep" from "something is broken".
const NetworkFaultExit = 7

// NameNotResolved is a step that failed because a name could not be resolved:
// git's "Could not resolve host", the resolver's "Temporary failure in name
// resolution". The adapters that run git and bd are the one place that reads
// those words; the application sees only this type.
//
// It is what a host just woken from standby says for a minute or two, and it
// is the one failure a dispatch waits out. Err is the failure as it was,
// unwrapped for whoever wants its exit code or its words.
type NameNotResolved struct {
	// Said is the one line of the tool's output that said so.
	Said string
	Err  error
}

// Error is the failure as the adapter reported it.
func (n *NameNotResolved) Error() string {
	if n.Err != nil {
		return n.Err.Error()
	}
	return n.Said
}

// Unwrap gives back the failure underneath, so that a sync halted by beads is
// still a halt with beads' exit code.
func (n *NameNotResolved) Unwrap() error { return n.Err }

// Unresolved reports whether err is a failure to resolve a name, and what was
// said.
func Unresolved(err error) (*NameNotResolved, bool) {
	var unresolved *NameNotResolved
	return unresolved, errors.As(err, &unresolved)
}

// LocalNetworkFault is a dispatch that waited for a name to resolve and it never
// did. It is not a failed run, and a timer should not treat it as one: it
// carries NetworkFaultExit. The line naming it has already been printed.
type LocalNetworkFault struct {
	// Said is what could not be resolved, in the tool's own words.
	Said string
	// Tries is how many times the sync was tried.
	Tries int
}

// Line is the one line a person, or a timer's log, reads.
func (f *LocalNetworkFault) Line() string {
	return "local network fault: " + f.Said + "; nothing dispatched"
}

// Error is the same line, so a caller that does print an error prints it.
func (f *LocalNetworkFault) Error() string { return f.Line() }

// LocalFault reports whether err is a dispatch that gave up waiting for the
// network, and what it found.
func LocalFault(err error) (*LocalNetworkFault, bool) {
	var fault *LocalNetworkFault
	return fault, errors.As(err, &fault)
}

// waitFor waits d, or until ctx ends, whichever is first. A zero wait does not
// wait at all.
func waitFor(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
