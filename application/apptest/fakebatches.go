package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeBatches is an in-memory application.GristBatches: the batches it was
// told carried a story, by rig and story, and none for any other.
type FakeBatches struct {
	mu      sync.Mutex
	batches map[[2]string]int
	Err     error
}

var _ application.GristBatches = (*FakeBatches)(nil)

// NewFakeBatches returns batches that carried no story.
func NewFakeBatches() *FakeBatches {
	return &FakeBatches{batches: map[[2]string]int{}}
}

// Carry records that batch number of rig landed story.
func (f *FakeBatches) Carry(rig, story string, number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches[[2]string{rig, story}] = number
}

// BatchOf implements application.GristBatches.
func (f *FakeBatches) BatchOf(_ context.Context, rig, story string) (int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return 0, false, f.Err
	}
	number, ok := f.batches[[2]string{rig, story}]
	return number, ok, nil
}
