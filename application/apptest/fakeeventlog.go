package apptest

import (
	"context"
	"sort"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// FakeEventLog is an in-memory application.EventLog that numbers and checks
// events as the file adapter does.
type FakeEventLog struct {
	mu       sync.Mutex
	evs      []events.Event
	archived []events.Event
	days     []string
	head     uint64
	failErr  error
}

var _ application.EventLog = (*FakeEventLog)(nil)
var _ application.EventArchive = (*FakeEventLog)(nil)

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
	seq := f.head
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
	f.head = seq
	return seq, nil
}

// Since implements application.EventLog.
func (f *FakeEventLog) Since(_ context.Context, seq uint64) ([]events.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return after(f.evs, seq), nil
}

// after is the events of evs with a seq above seq.
func after(evs []events.Event, seq uint64) []events.Event {
	var out []events.Event
	for _, e := range evs {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out
}

// Head implements application.EventLog.
func (f *FakeEventLog) Head(context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.head, nil
}

// Trim implements application.EventArchive: events up to seq upTo go to the
// archive, which the fake keeps as one list, noting the day of each trim.
func (f *FakeEventLog) Trim(_ context.Context, upTo uint64, day string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var kept []events.Event
	moved := 0
	for _, e := range f.evs {
		if e.Seq <= upTo {
			f.archived = append(f.archived, e)
			moved++
		} else {
			kept = append(kept, e)
		}
	}
	f.evs = kept
	if moved > 0 {
		f.days = append(f.days, day)
	}
	return moved, nil
}

// First implements application.EventArchive.
func (f *FakeEventLog) First(context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.evs) == 0 {
		return 0, nil
	}
	return f.evs[0].Seq, nil
}

// Archived implements application.EventArchive.
func (f *FakeEventLog) Archived(_ context.Context, seq uint64) ([]events.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return after(f.archived, seq), nil
}

// TrimDays is the day given to each Trim that moved something, oldest first.
func (f *FakeEventLog) TrimDays() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.days...)
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
	if len(c.Verified) > 0 {
		out.Verified = map[string]bool{}
		for k := range c.Verified {
			out.Verified[k] = true
		}
	}
	return out
}

// FakeShipStates is an in-memory application.ShipStates.
type FakeShipStates struct {
	mu    sync.Mutex
	saved application.ShipState
}

var _ application.ShipStates = (*FakeShipStates)(nil)

// Load implements application.ShipStates.
func (f *FakeShipStates) Load(context.Context) (application.ShipState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.saved
	out.Pending = append([]application.ShipRange(nil), f.saved.Pending...)
	return out, nil
}

// Save implements application.ShipStates.
func (f *FakeShipStates) Save(_ context.Context, s application.ShipState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = s
	f.saved.Pending = append([]application.ShipRange(nil), s.Pending...)
	return nil
}

// FakeSubscribeFiles is an in-memory application.SubscribeFiles: the text of
// each seat's subscribe file, by seat.
type FakeSubscribeFiles struct {
	mu    sync.Mutex
	files map[string]string
}

var _ application.SubscribeFiles = (*FakeSubscribeFiles)(nil)

// Set puts a seat's subscribe file in place.
func (f *FakeSubscribeFiles) Set(seat, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.files == nil {
		f.files = map[string]string{}
	}
	f.files[seat] = text
}

// SubscribedSeats implements application.SubscribeFiles.
func (f *FakeSubscribeFiles) SubscribedSeats(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var seats []string
	for seat := range f.files {
		seats = append(seats, seat)
	}
	sort.Strings(seats)
	return seats, nil
}

// SubscribeFile implements application.SubscribeFiles.
func (f *FakeSubscribeFiles) SubscribeFile(_ context.Context, seat string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	text, ok := f.files[seat]
	return text, ok, nil
}

// FakeNudgeCursors is an in-memory application.NudgeCursors.
type FakeNudgeCursors struct {
	mu    sync.Mutex
	saved map[string]uint64
}

var _ application.NudgeCursors = (*FakeNudgeCursors)(nil)

// Load implements application.NudgeCursors.
func (f *FakeNudgeCursors) Load(context.Context) (map[string]uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]uint64, len(f.saved))
	for k, v := range f.saved {
		out[k] = v
	}
	return out, nil
}

// Save implements application.NudgeCursors.
func (f *FakeNudgeCursors) Save(_ context.Context, cursors map[string]uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = make(map[string]uint64, len(cursors))
	for k, v := range cursors {
		f.saved[k] = v
	}
	return nil
}
