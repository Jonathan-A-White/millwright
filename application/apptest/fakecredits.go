package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeCreditKey is an in-memory application.GristCreditKey: Key is the key it
// reads, or Refused the sentence for why there is none.
type FakeCreditKey struct {
	Key     string
	Refused string
}

var _ application.GristCreditKey = (*FakeCreditKey)(nil)

// Read implements application.GristCreditKey.
func (f *FakeCreditKey) Read(context.Context) (string, string) { return f.Key, f.Refused }

// FakeCreditState is an in-memory application.GristCreditState.
type FakeCreditState struct {
	mu     sync.Mutex
	mark   application.GristCreditMark
	found  bool
	Spends int
	Clears int
}

var _ application.GristCreditState = (*FakeCreditState)(nil)

// Set puts a mark in the state, as an earlier pass wrote it.
func (f *FakeCreditState) Set(mark application.GristCreditMark) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mark, f.found = mark, true
}

// Mark implements application.GristCreditState.
func (f *FakeCreditState) Mark(context.Context) (application.GristCreditMark, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mark, f.found, nil
}

// Spend implements application.GristCreditState.
func (f *FakeCreditState) Spend(_ context.Context, mark application.GristCreditMark) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mark, f.found = mark, true
	f.Spends++
	return nil
}

// Clear implements application.GristCreditState.
func (f *FakeCreditState) Clear(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mark, f.found = application.GristCreditMark{}, false
	f.Clears++
	return nil
}
