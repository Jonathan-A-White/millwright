package apptest

import (
	"context"
	"sort"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeCloseOutMarks is an in-memory application.CloseOutMarks: the marks of the
// close-outs running, by story.
type FakeCloseOutMarks struct {
	mu    sync.Mutex
	marks map[string]application.CloseOutMark

	// Writes is every mark written, in order, so that a test can see a close-out
	// begin and then wait for calm.
	Writes []application.CloseOutMark

	// ReadErr, when set, is returned instead of reading.
	ReadErr error
}

// FakeCloseOutMarks satisfies the port.
var _ application.CloseOutMarks = (*FakeCloseOutMarks)(nil)

// NewFakeCloseOutMarks is a marker holding nothing.
func NewFakeCloseOutMarks() *FakeCloseOutMarks {
	return &FakeCloseOutMarks{marks: map[string]application.CloseOutMark{}}
}

// Write implements application.CloseOutMarks.
func (f *FakeCloseOutMarks) Write(_ context.Context, mark application.CloseOutMark) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marks[mark.Story] = mark
	f.Writes = append(f.Writes, mark)
	return nil
}

// Read implements application.CloseOutMarks.
func (f *FakeCloseOutMarks) Read(_ context.Context) ([]application.CloseOutMark, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ReadErr != nil {
		return nil, f.ReadErr
	}
	var out []application.CloseOutMark
	for _, m := range f.marks {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Story < out[j].Story })
	return out, nil
}

// Clear implements application.CloseOutMarks.
func (f *FakeCloseOutMarks) Clear(_ context.Context, story string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.marks, story)
	return nil
}
