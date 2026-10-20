package application

import (
	"context"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// JobNewest is the newest events of one job actor (<name>@<host>) of each kind
// a reader asks of it: its last event of any state, its newest scheduled and
// running events, and the newest end of a pass (done or failed). An event with
// seq 0 is one the job has not written.
type JobNewest struct {
	Last      events.Event `json:"last"`
	Scheduled events.Event `json:"scheduled"`
	Running   events.Event `json:"running"`
	Ended     events.Event `json:"ended"`
}

// EventNewest is what the log keeps of its events so that a reader of a job's
// current state does not scan the log (mw-gq6.655): the newest events of each
// job actor, the newest event no job wrote, and the first event it was built
// from. It covers the log up to Seq.
type EventNewest struct {
	Seq   uint64               `json:"seq"`
	First events.Event         `json:"first"`
	Other events.Event         `json:"other"`
	Jobs  map[string]JobNewest `json:"jobs"`
}

// Add folds the next event of the log in. Events are added in seq order.
func (n *EventNewest) Add(e events.Event) {
	if e.Seq > n.Seq {
		n.Seq = e.Seq
	}
	if n.First.Seq == 0 {
		n.First = e
	}
	if e.Kind != events.KindJob {
		n.Other = e
		return
	}
	if n.Jobs == nil {
		n.Jobs = map[string]JobNewest{}
	}
	j := n.Jobs[e.Actor]
	j.Last = e
	switch e.To {
	case events.JobScheduled:
		j.Scheduled = e
	case events.JobRunning:
		j.Running = e
	case events.JobDone, events.JobFailed:
		j.Ended = e
	}
	n.Jobs[e.Actor] = j
}

// EventNewestLog is an EventLog that keeps its EventNewest between reads, and
// so answers without scanning: the file adapter keeps it in a file beside the
// log and catches it up from the events since it last looked.
type EventNewestLog interface {
	Newest(ctx context.Context) (EventNewest, error)
}

// NewestOf is what log holds of each job's newest events: from the log's own
// record when it keeps one, else by reading the whole log, which is right but
// grows with it.
func NewestOf(ctx context.Context, log EventLog) (EventNewest, error) {
	if kept, ok := log.(EventNewestLog); ok {
		return kept.Newest(ctx)
	}
	evs, err := log.Since(ctx, 0)
	if err != nil {
		return EventNewest{}, err
	}
	var n EventNewest
	for _, e := range evs {
		n.Add(e)
	}
	return n, nil
}
