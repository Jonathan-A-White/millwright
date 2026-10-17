package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeBenchFlags is an in-memory application.BenchFlagMemory: the flag text
// last printed for each kind and when.
type FakeBenchFlags struct {
	mu   sync.Mutex
	said map[string]fakeFlagSaid

	// Err, when set, is returned instead of reading or writing.
	Err error
}

type fakeFlagSaid struct {
	text string
	at   time.Time
}

var _ application.BenchFlagMemory = (*FakeBenchFlags)(nil)

// NewFakeBenchFlags is a memory that has printed nothing.
func NewFakeBenchFlags() *FakeBenchFlags { return &FakeBenchFlags{said: map[string]fakeFlagSaid{}} }

// Said implements application.BenchFlagMemory.
func (f *FakeBenchFlags) Said(_ context.Context, kind string) (string, time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", time.Time{}, false, f.Err
	}
	s, ok := f.said[kind]
	return s.text, s.at, ok, nil
}

// Say implements application.BenchFlagMemory.
func (f *FakeBenchFlags) Say(_ context.Context, kind, text string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.said[kind] = fakeFlagSaid{text: text, at: at}
	return nil
}
