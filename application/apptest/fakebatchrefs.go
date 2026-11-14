package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeBatchRefs is an in-memory application.BatchRefs: the batch directories
// it was told hold no outcome.txt, and none for any other.
type FakeBatchRefs struct {
	mu    sync.Mutex
	dirs  []application.BatchRef
	Err   error
	Reads int
}

var _ application.BatchRefs = (*FakeBatchRefs)(nil)

// Leave records that batch number of rig has a directory and no outcome.txt.
func (f *FakeBatchRefs) Leave(rig string, number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirs = append(f.dirs, application.BatchRef{Rig: rig, Number: number})
}

// WithoutOutcome implements application.BatchRefs.
func (f *FakeBatchRefs) WithoutOutcome(context.Context) ([]application.BatchRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Reads++
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]application.BatchRef(nil), f.dirs...), nil
}
