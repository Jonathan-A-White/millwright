package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.ContactBook = (*FakeContactBook)(nil)

// FakeContactBook is his address book in memory: Find answers Matches[name]
// (the whole name as it was asked) or Err, and records every name it was asked.
type FakeContactBook struct {
	Matches map[string][]application.Contact
	Err     error

	mu    sync.Mutex
	asked []string
}

// Find implements application.ContactBook.
func (f *FakeContactBook) Find(_ context.Context, name string) ([]application.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, name)
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Matches[name], nil
}

// Asked is every name Find was given, in order.
func (f *FakeContactBook) Asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}
