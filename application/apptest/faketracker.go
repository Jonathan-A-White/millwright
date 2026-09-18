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
	StatusDeferred   = "deferred"
	StatusClosed     = "closed"
)

// Actor is the assignee the fake records when a story is claimed.
const Actor = "fake"

// FakeTracker is an in-memory application.WorkTracker. Stories are listed in
// the order they were added, so a test can assert on the whole list.
//
// A story it holds is ready when it is open (neither held nor claimed nor
// closed), under the epic asked about, pointed at the host asked about, and
// every story it waits on is closed — which is what beads does too.
type FakeTracker struct {
	mu sync.Mutex

	defaults map[string]domain.Path // epic id -> its default path
	stories  map[string]*fakeStory
	order    []string
	epics    []string

	notes map[string]string
	syncs int

	// Err, when set, is returned by every method instead of doing the work.
	Err error
	// SyncErr, when set, is what Sync reports instead of synchronising. Use
	// SyncExits to make it the halt a beads exit code stands for.
	SyncErr error
}

// fakeStory is one story as the fake remembers it.
type fakeStory struct {
	detail      application.StoryDetail
	metadata    map[string]string
	needs       []string
	comments    []string
	closeReason string
	touched     time.Time
}

// NewFakeTracker returns an empty fake work tracker.
func NewFakeTracker() *FakeTracker {
	return &FakeTracker{
		defaults: map[string]domain.Path{},
		stories:  map[string]*fakeStory{},
		notes:    map[string]string{},
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

// CreateEpic implements application.WorkTracker. The fake mints ids the way
// beads does — f-1 for an epic, f-1.1 for its first story — so that a test can
// read a tree without knowing them in advance.
func (f *FakeTracker) CreateEpic(_ context.Context, epic application.NewEpic) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if epic.Title == "" {
		return "", fmt.Errorf("an epic needs a title")
	}
	id := fmt.Sprintf("f-%d", len(f.epics)+1)
	f.epics = append(f.epics, id)
	f.defaults[id] = epic.Defaults
	return id, nil
}

// CreateStory implements application.WorkTracker. The story is filed held, and
// stays that way until it is released.
func (f *FakeTracker) CreateStory(_ context.Context, story application.NewStory) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	switch {
	case story.Title == "":
		return "", fmt.Errorf("a story needs a title")
	case story.EpicID == "":
		return "", fmt.Errorf("a story needs an epic to be filed under")
	}
	for _, need := range story.Needs {
		if _, filed := f.stories[need]; !filed {
			return "", fmt.Errorf("story %q waits on %q, which is not filed", story.Title, need)
		}
	}

	under := 0
	for _, id := range f.order {
		if f.stories[id].detail.EpicID == story.EpicID {
			under++
		}
	}
	id := fmt.Sprintf("%s.%d", story.EpicID, under+1)

	f.order = append(f.order, id)
	f.stories[id] = &fakeStory{
		detail: application.StoryDetail{
			Story: domain.Story{
				ID:        id,
				Title:     story.Title,
				Overrides: story.Overrides,
			},
			Defaults:        f.defaults[story.EpicID],
			EpicID:          story.EpicID,
			Status:          StatusDeferred,
			Description:     story.Description,
			Acceptance:      story.Acceptance,
			EstimateMinutes: story.EstimateMinutes,
		},
		metadata: story.Overrides.Metadata(),
		needs:    append([]string(nil), story.Needs...),
		touched:  time.Now(),
	}
	return id, nil
}

// ReleaseStory implements application.WorkTracker.
func (f *FakeTracker) ReleaseStory(_ context.Context, id string) error {
	return f.write(id, func(s *fakeStory) error {
		if s.detail.Status == StatusDeferred {
			s.detail.Status = StatusOpen
		}
		return nil
	})
}

// Epics reports the ids of the epics filed, in the order they were filed.
func (f *FakeTracker) Epics() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.epics...)
}

// Stories reports the ids of every story the fake holds, in the order they
// arrived — so that a test can say that nothing at all was written.
func (f *FakeTracker) Stories() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.order...)
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
			s.detail.Merged().Host != host,
			f.waiting(s):
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

// Sync implements application.TrackerSync. Nothing is synchronised: the fake
// counts the cycle and reports SyncErr, so that a use case can be walked
// through a halted sync without a database or a remote.
func (f *FakeTracker) Sync(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.syncs++
	return f.SyncErr
}

// Note implements application.TrackerSync.
func (f *FakeTracker) Note(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	return f.notes[key], nil
}

// SetNote implements application.TrackerSync.
func (f *FakeTracker) SetNote(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	if key == "" {
		return fmt.Errorf("a note needs a key")
	}
	f.notes[key] = value
	return nil
}

// SyncExits makes the next Sync halt the way a beads exit code says it did.
// Code 0 clears it.
func (f *FakeTracker) SyncExits(code int, said string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if code == 0 {
		f.SyncErr = nil
		return
	}
	f.SyncErr = &application.SyncHalt{Code: code, Said: said}
}

// Syncs reports how many synchronisation cycles were asked for, so that a test
// can say a halt was not retried.
func (f *FakeTracker) Syncs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.syncs
}

// waiting reports whether this story still waits on another that is not
// finished. The lock is held by the caller.
func (f *FakeTracker) waiting(s *fakeStory) bool {
	for _, need := range s.needs {
		blocker, filed := f.stories[need]
		if !filed || blocker.detail.Status != StatusClosed {
			return true
		}
	}
	return false
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

// FakeTracker satisfies the ports the beads gateway stands behind.
var (
	_ application.WorkTracker = (*FakeTracker)(nil)
	_ application.TrackerSync = (*FakeTracker)(nil)
)
