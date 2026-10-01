package apptest

import (
	"context"
	"sync"
)

// FakeHead is a BeadsHead that reads out a script: each call returns the
// next of Heads, with the next of Errs as its error when that is not nil, and
// then the last head again. Calls counts the reads.
type FakeHead struct {
	mu    sync.Mutex
	Heads []string
	Errs  []error
	calls int
}

// Head returns the next scripted head.
func (f *FakeHead) Head(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	f.calls++
	var err error
	if i < len(f.Errs) {
		err = f.Errs[i]
	}
	if len(f.Heads) == 0 {
		return "", err
	}
	if i >= len(f.Heads) {
		i = len(f.Heads) - 1
	}
	return f.Heads[i], err
}

// Calls is how many times Head was read.
func (f *FakeHead) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
