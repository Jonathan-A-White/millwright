package application

import (
	"context"
	"fmt"
	"strings"
)

// failures is the one sentence a dispatch with failures in it ends with.
func (r DispatchReport) failures() string {
	said := make([]string, 0, len(r.Failed))
	for _, failed := range r.Failed {
		said = append(said, failed.StoryID+": "+failed.line())
	}
	return strings.Join(said, "; ")
}

// line is one failure as a person reads it, saying plainly whether the story is
// ready for somebody else again.
func (f Failed) line() string {
	if f.Released {
		return fmt.Sprintf("%v (the claim was given back)", f.Err)
	}
	return fmt.Sprintf("%v", f.Err)
}

// pausedHere reports whether this host is paused: its own log says so, or the
// note the home's follower mirrored does. A read that fails is said in the
// report's notes and does not stop the pass, as a log read that fails never has.
func (d Dispatch) pausedHere(ctx context.Context) (HostPause, bool, error) {
	var failed error
	if d.Events != nil {
		pause, paused, err := PausedHost(ctx, d.Events, d.Host)
		if paused {
			return pause, true, nil
		}
		failed = err
	}
	if d.Memory != nil {
		pause, paused, err := PausedByNote(ctx, d.Memory, d.Host)
		if paused {
			return pause, true, nil
		}
		if failed == nil {
			failed = err
		}
	}
	return HostPause{}, false, failed
}
