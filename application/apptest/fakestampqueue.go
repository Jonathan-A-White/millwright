package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// FakeStampQueue is an in-memory application.StampQueue: what is pending and
// what is sent, in order, with the same rules as the file adapter's.
type FakeStampQueue struct {
	mu      sync.Mutex
	pending []application.QueuedStamp
	sent    []application.SentStamp

	// AppendErr, when set, is returned by Append instead of queueing.
	AppendErr error
	// Err, when set, is returned by Pending, MarkFailed and MarkSent.
	Err error
}

// FakeStampQueue satisfies the port.
var _ application.StampQueue = (*FakeStampQueue)(nil)

// NewFakeStampQueue returns an empty queue.
func NewFakeStampQueue() *FakeStampQueue { return &FakeStampQueue{} }

// Append implements application.StampQueue.
func (f *FakeStampQueue) Append(_ context.Context, stamp domain.Stamp) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AppendErr != nil {
		return f.AppendErr
	}
	f.pending = append(f.pending, application.QueuedStamp{Stamp: stamp})
	return nil
}

// Pending implements application.StampQueue.
func (f *FakeStampQueue) Pending(_ context.Context) ([]application.QueuedStamp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]application.QueuedStamp(nil), f.pending...), nil
}

// MarkFailed implements application.StampQueue.
func (f *FakeStampQueue) MarkFailed(_ context.Context, stamp domain.Stamp, why string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	for i := range f.pending {
		if f.pending[i].Stamp == stamp {
			f.pending[i].Attempts++
			f.pending[i].LastError = why
		}
	}
	return nil
}

// MarkSent implements application.StampQueue.
func (f *FakeStampQueue) MarkSent(_ context.Context, stamp domain.Stamp, txid string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	for i := range f.pending {
		if f.pending[i].Stamp == stamp {
			f.sent = append(f.sent, application.SentStamp{
				Stamp: stamp, Txid: txid, SentAt: at, Attempts: f.pending[i].Attempts + 1,
			})
			f.pending = append(f.pending[:i], f.pending[i+1:]...)
			return nil
		}
	}
	return nil
}

// Queued reports what is pending, oldest first.
func (f *FakeStampQueue) Queued() []application.QueuedStamp {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]application.QueuedStamp(nil), f.pending...)
}

// Sent reports what has been sent, in the order it was.
func (f *FakeStampQueue) Sent() []application.SentStamp {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]application.SentStamp(nil), f.sent...)
}
