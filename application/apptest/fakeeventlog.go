package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// FakeEventLog is an in-memory application.EventLog that numbers and checks
// events as the file adapter does.
type FakeEventLog struct {
	mu      sync.Mutex
	evs     []events.Event
	failErr error
}

var _ application.EventLog = (*FakeEventLog)(nil)

// FailNext makes the next Append fail with err, writing nothing.
func (f *FakeEventLog) FailNext(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failErr = err
}

// Append implements application.EventLog.
func (f *FakeEventLog) Append(_ context.Context, evs []events.Event) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failErr; err != nil {
		f.failErr = nil
		return 0, err
	}
	seq := uint64(len(f.evs))
	numbered := make([]events.Event, len(evs))
	for i, e := range evs {
		seq++
		e.Seq = seq
		if err := e.Validate(); err != nil {
			return 0, err
		}
		numbered[i] = e
	}
	f.evs = append(f.evs, numbered...)
	return seq, nil
}

// Since implements application.EventLog.
func (f *FakeEventLog) Since(_ context.Context, seq uint64) ([]events.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if seq >= uint64(len(f.evs)) {
		return nil, nil
	}
	return append([]events.Event(nil), f.evs[seq:]...), nil
}

// Head implements application.EventLog.
func (f *FakeEventLog) Head(context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return uint64(len(f.evs)), nil
}

// All is every event appended, oldest first.
func (f *FakeEventLog) All() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]events.Event(nil), f.evs...)
}

// FakeFollowCursors is an in-memory application.FollowCursors.
type FakeFollowCursors struct {
	mu    sync.Mutex
	saved *application.FollowCursor
}

var _ application.FollowCursors = (*FakeFollowCursors)(nil)

// Load implements application.FollowCursors.
func (f *FakeFollowCursors) Load(context.Context) (application.FollowCursor, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saved == nil {
		return application.FollowCursor{}, false, nil
	}
	return copyCursor(*f.saved), true, nil
}

// Save implements application.FollowCursors.
func (f *FakeFollowCursors) Save(_ context.Context, c application.FollowCursor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	saved := copyCursor(c)
	f.saved = &saved
	return nil
}

// Saved is the cursor last saved, and whether one was.
func (f *FakeFollowCursors) Saved() (application.FollowCursor, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saved == nil {
		return application.FollowCursor{}, false
	}
	return copyCursor(*f.saved), true
}

func copyCursor(c application.FollowCursor) application.FollowCursor {
	out := application.FollowCursor{Since: c.Since, Seen: append([]string(nil), c.Seen...), States: map[string]string{}}
	for k, v := range c.States {
		out.States[k] = v
	}
	return out
}
