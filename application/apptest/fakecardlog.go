package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// FakeCardLog is an in-memory application.CardLog.
type FakeCardLog struct {
	mu      sync.Mutex
	records []domain.CardRecord
	// Err, when set, is what Append and List fail with.
	Err error
}

var _ application.CardLog = (*FakeCardLog)(nil)

// NewFakeCardLog is an empty card log.
func NewFakeCardLog() *FakeCardLog { return &FakeCardLog{} }

// Append implements application.CardLog.
func (f *FakeCardLog) Append(_ context.Context, record domain.CardRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.records = append(f.records, record)
	return nil
}

// List implements application.CardLog: the records, oldest first.
func (f *FakeCardLog) List(context.Context) ([]domain.CardRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]domain.CardRecord(nil), f.records...), nil
}
