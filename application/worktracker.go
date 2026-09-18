package application

import (
	"context"

	"github.com/Jonathan-A-White/millwright/domain"
)

// StoryDetail is everything the work tracker knows about one story: the story
// itself with the Path overrides it carries, and the default Path of the epic
// it belongs to. The Path the story is actually worked by is the two overlaid,
// which is what Path reports.
type StoryDetail struct {
	Story      domain.Story
	Defaults   domain.Path
	EpicID     string
	Status     string
	Assignee   string
	Acceptance string
	// EstimateMinutes is the Mayor's estimate in minutes; zero when unset.
	EstimateMinutes int
}

// Merged is the epic's defaults overlaid with the story's own overrides,
// whether or not what comes out is a complete Path. Use it to read one field
// of a story that may not be fully planned yet; use Path before working it.
func (d StoryDetail) Merged() domain.Path {
	return d.Defaults.Overlay(d.Story.Overrides)
}

// Path is the Path this story is worked by, or the reason it has none.
func (d StoryDetail) Path() (domain.Path, error) {
	return d.Story.PathFrom(d.Defaults)
}

// WorkTracker is the port the factory reads and writes stories through. One
// adapter talks to beads; an in-memory one stands in for it in tests.
//
// Implementations serialise their own calls: the beads database takes a
// single-writer lock, so no two of these may be in flight at once.
type WorkTracker interface {
	// ShowStory reports one story with its epic's default Path overlaid.
	ShowStory(ctx context.Context, id string) (StoryDetail, error)

	// ReadyStories lists the stories of an epic that can be started on a host
	// right now: open, unclaimed, unblocked, and whose Path names that host.
	ReadyStories(ctx context.Context, epicID, host string) ([]StoryDetail, error)

	// ClaimStory takes a story: it becomes assigned and in progress, and stops
	// being ready. Claiming a story already claimed by this actor is harmless.
	ClaimStory(ctx context.Context, id string) error

	// SetStoryMetadata writes metadata fields onto a story, leaving the fields
	// it does not name alone. Path fields are metadata like any other.
	SetStoryMetadata(ctx context.Context, id string, fields map[string]string) error

	// CommentOnStory appends one comment to a story.
	CommentOnStory(ctx context.Context, id, text string) error

	// CloseStory closes a story with the reason it was closed for.
	CloseStory(ctx context.Context, id, reason string) error

	// StaleClaims lists claimed stories untouched for at least days days — the
	// sessions that went away without closing or handing back. days must be at
	// least 1. The stories come back without an epic's defaults overlaid.
	StaleClaims(ctx context.Context, days int) ([]StoryDetail, error)
}
