package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.GhostBlog = (*FakeGhostBlog)(nil)

// FakeGhostBlog is a blog in memory: Posts by slug. Created and Updated keep
// what Create and Update were given, in order; Err, when set, is what each
// method returns.
type FakeGhostBlog struct {
	Posts   map[string]application.GhostPost
	Created []application.GhostDraftPost
	Updated []application.GhostDraftPost
	Err     error
}

// Find implements application.GhostBlog.
func (f *FakeGhostBlog) Find(_ context.Context, slug string) (application.GhostPost, bool, error) {
	if f.Err != nil {
		return application.GhostPost{}, false, f.Err
	}
	post, ok := f.Posts[slug]
	return post, ok, nil
}

// Create implements application.GhostBlog: the new post is a draft, as the
// real blog makes it.
func (f *FakeGhostBlog) Create(_ context.Context, draft application.GhostDraftPost) (application.GhostPost, error) {
	if f.Err != nil {
		return application.GhostPost{}, f.Err
	}
	f.Created = append(f.Created, draft)
	post := application.GhostPost{ID: "new1", Slug: draft.Slug, Title: draft.Title, Status: application.GhostStatusDraft, EditorURL: "https://blog.test/ghost/#/editor/post/new1"}
	if f.Posts == nil {
		f.Posts = map[string]application.GhostPost{}
	}
	f.Posts[draft.Slug] = post
	return post, nil
}

// Update implements application.GhostBlog.
func (f *FakeGhostBlog) Update(_ context.Context, post application.GhostPost, draft application.GhostDraftPost) (application.GhostPost, error) {
	if f.Err != nil {
		return application.GhostPost{}, f.Err
	}
	f.Updated = append(f.Updated, draft)
	return post, nil
}
