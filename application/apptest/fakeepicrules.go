package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// FakeEpicRules is an in-memory application.EpicRules: the requirements each rig
// was given, and none for a rig that was not.
type FakeEpicRules struct {
	mu    sync.Mutex
	rigs  map[string]domain.EpicRequirements
	Err   error
	reads int
}

var _ application.EpicRules = (*FakeEpicRules)(nil)

// NewFakeEpicRules returns rules that require nothing of any rig.
func NewFakeEpicRules() *FakeEpicRules {
	return &FakeEpicRules{rigs: map[string]domain.EpicRequirements{}}
}

// Require sets what a rig requires of its epics.
func (f *FakeEpicRules) Require(rig string, requirements domain.EpicRequirements) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rigs[rig] = requirements
}

// Reads reports how many times a rig's requirements were read.
func (f *FakeEpicRules) Reads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// EpicRequirements implements application.EpicRules.
func (f *FakeEpicRules) EpicRequirements(_ context.Context, rig string) (domain.EpicRequirements, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.Err != nil {
		return domain.EpicRequirements{}, f.Err
	}
	return f.rigs[rig], nil
}
