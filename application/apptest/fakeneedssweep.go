package apptest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeNeedsSweep is an in-memory application.NeedsSweepBeads: the beads and
// comments a test sets, and every comment the run wrote.
type FakeNeedsSweep struct {
	mu sync.Mutex

	Beads    []application.TriageBead
	Comments map[string][]application.Comment
	// Now stamps the comments written.
	Now time.Time
	// FailOn names a bead whose comment write fails.
	FailOn string
	// Written are the comments written, in order.
	Written []VerifyWrite
}

var _ application.NeedsSweepBeads = (*FakeNeedsSweep)(nil)

// TriageBeads implements application.TriageSource.
func (f *FakeNeedsSweep) TriageBeads(context.Context) ([]application.TriageBead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]application.TriageBead(nil), f.Beads...), nil
}

// StoriesComments implements application.NeedsSweepBeads.
func (f *FakeNeedsSweep) StoriesComments(_ context.Context, ids []string) (map[string][]application.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]application.Comment{}
	for _, id := range ids {
		out[id] = append([]application.Comment(nil), f.Comments[id]...)
	}
	return out, nil
}

// CommentOnStory implements application.NeedsSweepBeads.
func (f *FakeNeedsSweep) CommentOnStory(_ context.Context, id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id == f.FailOn {
		return fmt.Errorf("bd comments add %s: exit status 1", id)
	}
	if f.Comments == nil {
		f.Comments = map[string][]application.Comment{}
	}
	f.Comments[id] = append(f.Comments[id], application.Comment{Author: "mw", Created: f.Now, Text: text})
	f.Written = append(f.Written, VerifyWrite{ID: id, Text: text})
	return nil
}
