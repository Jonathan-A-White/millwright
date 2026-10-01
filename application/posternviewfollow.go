package application

import (
	"context"
	"fmt"
	"io"
	"time"
)

// DefaultViewFollowEvery is how often PosternViewFollow reads the beads' head:
// the Governor's tap shows within two seconds of the bead changing.
const DefaultViewFollowEvery = time.Second

// BeadsHead is the cheapest read that says whether any bead has changed: a
// hash of the beads' database that differs after every write, and is the same
// while nothing is written. It is an opaque string: only equality means
// anything.
type BeadsHead interface {
	Head(ctx context.Context) (string, error)
}

// PosternViewFollow republishes the live view whenever the beads change, so a
// tap of the Governor's shows at once instead of at the next timer tick. Each
// pass reads the head and, when it is not the one the last successful publish
// was made at, calls Publish (the existing PosternView build and write). A
// pass whose head is unchanged reads nothing else and costs no tokens.
//
// A failure — of the head read or of Publish — is said on Err and the loop
// goes on; a failed publish is retried on the next pass, since the head it
// was made at is not recorded until it succeeds, and a failure that repeats
// word for word is said once. The head is read before the view is built, so a
// bead that changes during the build is caught by the next pass.
type PosternViewFollow struct {
	Head BeadsHead
	// Publish builds the view and writes it where the backend serves it.
	Publish func(ctx context.Context) error
	// Every is the wait between two passes; zero means DefaultViewFollowEvery.
	Every time.Duration
	// Sleep waits d, returning early with the context's error when ctx ends;
	// a test replaces it. Nil waits on a timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// Err is where failures are said. A nil Err says nothing.
	Err io.Writer
}

// Run follows the beads until ctx ends, which is a clean stop: it returns nil.
func (f PosternViewFollow) Run(ctx context.Context) error {
	if f.Head == nil || f.Publish == nil {
		return fmt.Errorf("mw postern view --follow: no beads head or no view to publish")
	}
	every := f.Every
	if every <= 0 {
		every = DefaultViewFollowEvery
	}
	sleep := f.Sleep
	if sleep == nil {
		sleep = sleepFor
	}
	var published, lastSaid string
	say := func(err error) {
		if f.Err != nil && err.Error() != lastSaid {
			fmt.Fprintf(f.Err, "mw postern view --follow: %v\n", err)
		}
		lastSaid = err.Error()
	}
	for ctx.Err() == nil {
		head, err := f.Head.Head(ctx)
		switch {
		case ctx.Err() != nil:
		case err != nil:
			say(fmt.Errorf("reading the beads' head: %w", err))
		case head != published:
			if err := f.Publish(ctx); err != nil {
				if ctx.Err() == nil {
					say(fmt.Errorf("publishing the view: %w", err))
				}
			} else {
				published, lastSaid = head, ""
			}
		default:
			lastSaid = ""
		}
		if sleep(ctx, every) != nil {
			break
		}
	}
	return nil
}

// sleepFor waits d or until ctx ends, whichever is first, returning the
// context's error in the second case.
func sleepFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
