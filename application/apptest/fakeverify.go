package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeVerifyBeads is an in-memory application.VerifyBeads: the open demos, the
// closed stories and each bead's comments, set by the test, and every comment
// the run wrote.
type FakeVerifyBeads struct {
	mu sync.Mutex

	Work     application.WorkInHand
	Closed   []application.ClosedStory
	Comments map[string][]application.Comment
	// Now stamps the comments written; zero stamps none.
	Now time.Time

	// Written are the comments written, in order.
	Written []VerifyWrite
}

// VerifyWrite is one comment a FakeVerifyBeads took.
type VerifyWrite struct {
	ID, Text string
}

// FakeVerifyBeads satisfies the port.
var _ application.VerifyBeads = (*FakeVerifyBeads)(nil)

// WorkInHand implements application.VerifyBeads.
func (f *FakeVerifyBeads) WorkInHand(context.Context) (application.WorkInHand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Work, nil
}

// ClosedStories implements application.VerifyBeads.
func (f *FakeVerifyBeads) ClosedStories(_ context.Context, since time.Time) ([]application.ClosedStory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []application.ClosedStory
	for _, c := range f.Closed {
		if !c.ClosedAt.Before(since) {
			out = append(out, c)
		}
	}
	return out, nil
}

// StoriesComments implements application.VerifyBeads.
func (f *FakeVerifyBeads) StoriesComments(_ context.Context, ids []string) (map[string][]application.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]application.Comment{}
	for _, id := range ids {
		out[id] = append([]application.Comment(nil), f.Comments[id]...)
	}
	return out, nil
}

// CommentOnStory implements application.VerifyBeads.
func (f *FakeVerifyBeads) CommentOnStory(_ context.Context, id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Comments == nil {
		f.Comments = map[string][]application.Comment{}
	}
	f.Comments[id] = append(f.Comments[id], application.Comment{Author: "mw", Created: f.Now, Text: text})
	f.Written = append(f.Written, VerifyWrite{ID: id, Text: text})
	return nil
}
