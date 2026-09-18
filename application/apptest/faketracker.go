// Package apptest holds in-memory stand-ins for the application's ports, so
// that a use case can be tested without a beads database, a git worktree or a
// harness. It is test support that ships in the module: any package's tests
// may import it.
package apptest

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// Statuses a story in the fake tracker can be in. They are beads' own names.
const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusClosed     = "closed"
)

// Actor is the assignee the fake records when a story is claimed.
const Actor = "fake"

// FakeTracker is an in-memory application.WorkTracker. Stories are listed in
// the order they were added, so a test can assert on the whole list.
//
// It does not model dependencies between stories: a story it holds is ready as
// soon as it is open, unclaimed, under the epic asked about and pointed at the
// host asked about. Beads itself also withholds blocked stories.
type FakeTracker struct {
	mu sync.Mutex

	defaults map[string]domain.Path // epic id -> its default path
	stories  map[string]*fakeStory
	order    []string

	// Err, when set, is returned by every method instead of doing the work.
	Err error
}

// fakeStory is one story as the fake remembers it.
type fakeStory struct {
	detail      application.StoryDetail
	metadata    map[string]string
	comments    []string
	closeReason string
	touched     time.Time
}

// NewFakeTracker returns an empty fake work tracker.
func NewFakeTracker() *FakeTracker {
	return &FakeTracker{
		defaults: map[string]domain.Path{},
		stories:  map[string]*fakeStory{},
	}
}

// AddEpic records an epic and the default Path its stories inherit.
func (f *FakeTracker) AddEpic(id string, defaults domain.Path) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaults[id] = defaults
}

// AddStory records an open, unclaimed story under an epic. Its Overrides are
// the fields of the epic's default Path it departs from.
func (f *FakeTracker) AddStory(epicID string, story domain.Story) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, seen := f.stories[story.ID]; !seen {
		f.order = append(f.order, story.ID)
	}
	f.stories[story.ID] = &fakeStory{
		detail: application.StoryDetail{
			Story:    story,
			Defaults: f.defaults[epicID],
			EpicID:   epicID,
			Status:   StatusOpen,
		},
		metadata: map[string]string{},
		touched:  time.Now(),
	}
}

// Touched backdates a story's last activity, so that StaleClaims can be told
// about a session that went away.
func (f *FakeTracker) Touched(id string, when time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.stories[id]; ok {
		s.touched = when
	}
}

// Comments reports the comments left on a story, oldest first.
func (f *FakeTracker) Comments(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.stories[id]
	if !ok {
		return nil
	}
	return append([]string(nil), s.comments...)
}

// CloseReason reports why a story was closed, or "" if it is still open.
func (f *FakeTracker) CloseReason(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.stories[id]; ok {
		return s.closeReason
	}
	return ""
}

// Metadata reports the metadata fields written onto a story.
func (f *FakeTracker) Metadata(id string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	if s, ok := f.stories[id]; ok {
		for k, v := range s.metadata {
			out[k] = v
		}
	}
	return out
}

// ShowStory implements application.WorkTracker.
func (f *FakeTracker) ShowStory(_ context.Context, id string) (application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return application.StoryDetail{}, f.Err
	}
	s, ok := f.stories[id]
	if !ok {
		return application.StoryDetail{}, fmt.Errorf("no story %q", id)
	}
	return s.detail, nil
}

// ReadyStories implements application.WorkTracker.
func (f *FakeTracker) ReadyStories(_ context.Context, epicID, host string) ([]application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	var ready []application.StoryDetail
	for _, id := range f.order {
		s := f.stories[id]
		switch {
		case s.detail.EpicID != epicID,
			s.detail.Status != StatusOpen,
			s.detail.Assignee != "",
			s.detail.Merged().Host != host:
			continue
		}
		ready = append(ready, s.detail)
	}
	return ready, nil
}

// ClaimStory implements application.WorkTracker.
func (f *FakeTracker) ClaimStory(_ context.Context, id string) error {
	return f.write(id, func(s *fakeStory) error {
		if s.detail.Assignee != "" && s.detail.Assignee != Actor {
			return fmt.Errorf("story %q is already claimed by %s", id, s.detail.Assignee)
		}
		s.detail.Assignee = Actor
		s.detail.Status = StatusInProgress
		return nil
	})
}

// SetStoryMetadata implements application.WorkTracker.
func (f *FakeTracker) SetStoryMetadata(_ context.Context, id string, fields map[string]string) error {
	return f.write(id, func(s *fakeStory) error {
		for k, v := range fields {
			s.metadata[k] = v
			// Path fields are metadata: keep the story's overrides in step.
			_ = s.detail.Story.Overrides.Set(k, v)
		}
		return nil
	})
}

// CommentOnStory implements application.WorkTracker.
func (f *FakeTracker) CommentOnStory(_ context.Context, id, text string) error {
	return f.write(id, func(s *fakeStory) error {
		s.comments = append(s.comments, text)
		return nil
	})
}

// CloseStory implements application.WorkTracker.
func (f *FakeTracker) CloseStory(_ context.Context, id, reason string) error {
	return f.write(id, func(s *fakeStory) error {
		s.detail.Status = StatusClosed
		s.closeReason = reason
		return nil
	})
}

// StaleClaims implements application.WorkTracker.
func (f *FakeTracker) StaleClaims(_ context.Context, days int) ([]application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	if days < 1 {
		return nil, fmt.Errorf("stale claims need at least 1 day, got %d", days)
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	var stale []application.StoryDetail
	for _, id := range f.order {
		s := f.stories[id]
		if s.detail.Status == StatusInProgress && s.touched.Before(cutoff) {
			stale = append(stale, s.detail)
		}
	}
	return stale, nil
}

// write applies a change to one story under the lock.
func (f *FakeTracker) write(id string, change func(*fakeStory) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	s, ok := f.stories[id]
	if !ok {
		return fmt.Errorf("no story %q", id)
	}
	if err := change(s); err != nil {
		return err
	}
	s.touched = time.Now()
	return nil
}

// IDs reports the ids of the stories in a listing, in order — a convenience
// for tests that assert on what came back.
func IDs(details []application.StoryDetail) []string {
	ids := make([]string, 0, len(details))
	for _, d := range details {
		ids = append(ids, d.Story.ID)
	}
	return ids
}

// SortedIDs is IDs, sorted.
func SortedIDs(details []application.StoryDetail) []string {
	ids := IDs(details)
	sort.Strings(ids)
	return ids
}

// FakeTracker satisfies the port.
var _ application.WorkTracker = (*FakeTracker)(nil)
