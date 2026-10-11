package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeLandings is an in-memory application.GristLandings: the landings it was
// told of, by story, and none for any other.
type FakeLandings struct {
	mu       sync.Mutex
	landings map[string]application.GristLanding
	Err      error
}

var _ application.GristLandings = (*FakeLandings)(nil)

// NewFakeLandings returns landings that know of no story.
func NewFakeLandings() *FakeLandings {
	return &FakeLandings{landings: map[string]application.GristLanding{}}
}

// Land records that story landed in version, at commit.
func (f *FakeLandings) Land(story, version, commit string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.landings[story] = application.GristLanding{Version: version, Commit: commit}
}

// LandingOf implements application.GristLandings.
func (f *FakeLandings) LandingOf(_ context.Context, _, _, story string) (application.GristLanding, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return application.GristLanding{}, false, f.Err
	}
	landing, ok := f.landings[story]
	return landing, ok, nil
}
