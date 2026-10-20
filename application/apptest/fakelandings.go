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
	commits  map[string]string
	Err      error
}

var _ application.GristLandings = (*FakeLandings)(nil)

// NewFakeLandings returns landings that know of no story.
func NewFakeLandings() *FakeLandings {
	return &FakeLandings{landings: map[string]application.GristLanding{}, commits: map[string]string{}}
}

// Land records that story landed in version, at commit.
func (f *FakeLandings) Land(story, version, commit string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.landings[story] = application.GristLanding{Version: version, Commit: commit}
}

// LandWithoutVersion records that story landed at commit with a commit of its
// own that carries no version: the landing of a rig that writes none.
func (f *FakeLandings) LandWithoutVersion(story, commit string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits[story] = commit
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

// StoryCommitOf implements application.GristLandings.
func (f *FakeLandings) StoryCommitOf(_ context.Context, _, _, story string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", false, f.Err
	}
	commit, ok := f.commits[story]
	return commit, ok, nil
}
