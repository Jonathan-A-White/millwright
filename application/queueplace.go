package application

import (
	"context"
	"errors"
	"sync"
	"time"
)

// A foreground tool call of a Builder is cut off at 600 seconds (the default cap) with no words, so
// a mw check whose wait for the merge slot or the host's gate lock outlasts one
// ends before that and the Builder runs it again. Run again, it must not join the
// queue at the back, or a check queued behind a long gate would never reach the
// front. So a queue adapter is asked, through the context of the wait, to keep the
// place of the waiter when the wait ends at its deadline, and to take it up again
// for the same waiter's next call (mw-gq6.939). The adapters are the gate lock
// (infrastructure/hostlock) and the merge slot (infrastructure/rig); a ticket
// queue (infrastructure/ticketq) does the keeping.

// QueuePlace is a waiter's ask of a queue to keep its place, and the queue's
// answer. Key names the waiter, the same on every call that is the same wait; For
// is how long a place the waiter left is kept for. The queue fills in Place and Of
// when it kept the place.
type QueuePlace struct {
	Key string
	For time.Duration

	// Late, when it is set, is asked by a queue that has just come to the front
	// and taken what it guards, before the waiter is told: true means the waiter
	// would not use it now (a gate that cannot end inside what is left of the
	// Builder's call, mw-gq6.940). The queue then gives what it took back, keeps
	// the waiter's place instead of dropping the ticket, tells Kept, and ends the
	// wait with ErrPlaceKeptLate.
	Late func() bool

	mu    sync.Mutex
	kept  bool
	place int
	of    int
}

// Kept is told, by the queue, that the waiter's place was kept: it was place
// among of live tickets.
func (p *QueuePlace) Kept(place, of int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kept, p.place, p.of = true, place, of
}

// Result says whether the place was kept, and where it stood.
func (p *QueuePlace) Result() (kept bool, place, of int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kept, p.place, p.of
}

type queuePlaceKey struct{}

// WithQueuePlace asks every queue waited on under ctx to keep the waiter's place
// when the wait ends at the context's deadline.
func WithQueuePlace(ctx context.Context, place *QueuePlace) context.Context {
	return context.WithValue(ctx, queuePlaceKey{}, place)
}

// QueuePlaceOf is the ask under ctx, nil when there is none.
func QueuePlaceOf(ctx context.Context) *QueuePlace {
	place, _ := ctx.Value(queuePlaceKey{}).(*QueuePlace)
	return place
}

// ErrPlaceKeptLate is how a queue ends a wait it had won, when QueuePlace.Late
// said the waiter would not use it: the lock was given back and the waiter's
// place kept (mw-gq6.940).
var ErrPlaceKeptLate = errors.New("the place was won too late to be used in this call, and is kept")
