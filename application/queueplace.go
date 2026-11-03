package application

import (
	"context"
	"sync"
	"time"
)

// A foreground tool call of a Builder is cut off at 590 seconds with no words, so
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
