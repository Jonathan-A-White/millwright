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

	// Details are the beads ShowBeads answers for besides those of Work, by id.
	Details map[string]application.StoryDetail

	// Written are the comments written, in order; Closed beads are in Shut.
	Written []VerifyWrite
	// Shut are the beads closed, in order, with the reason (Text).
	Shut []VerifyWrite
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

// ShowBeads implements application.VerifyConfirmBeads: the beads of Details and
// of Work, as they stand now; an id nobody holds is left out.
func (f *FakeVerifyBeads) ShowBeads(_ context.Context, ids []string) ([]application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []application.StoryDetail
	for _, id := range ids {
		if d, ok := f.Details[id]; ok {
			out = append(out, d)
			continue
		}
		for _, list := range [][]application.StoryDetail{f.Work.Ready, f.Work.Running, f.Work.OpenClaimed} {
			for _, d := range list {
				if d.Story.ID == id {
					out = append(out, d)
				}
			}
		}
	}
	return out, nil
}

// StoryComments implements application.TidyBeads.
func (f *FakeVerifyBeads) StoryComments(_ context.Context, id string) ([]application.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]application.Comment(nil), f.Comments[id]...), nil
}

// CloseStory implements application.VerifyConfirmBeads: it keeps the close and
// shows the bead closed from then on.
func (f *FakeVerifyBeads) CloseStory(_ context.Context, id, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Shut = append(f.Shut, VerifyWrite{ID: id, Text: reason})
	if f.Details == nil {
		f.Details = map[string]application.StoryDetail{}
	}
	d, ok := f.Details[id]
	if !ok {
		for _, list := range [][]application.StoryDetail{f.Work.Ready, f.Work.Running, f.Work.OpenClaimed} {
			for _, w := range list {
				if w.Story.ID == id {
					d = w
				}
			}
		}
	}
	d.Status = application.StatusClosed
	f.Details[id] = d
	return nil
}
