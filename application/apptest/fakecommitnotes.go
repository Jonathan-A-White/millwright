package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// AddedNote is one note a FakeCommitNotes was asked to write.
type AddedNote struct {
	RigDir, Ref, Commit, Note string
}

// FakeCommitNotes is an application.CommitNotes that records the notes it is
// asked for and writes nothing.
type FakeCommitNotes struct {
	mu    sync.Mutex
	added []AddedNote

	// Err, when set, is returned by AddNote, after recording the request.
	Err error
}

// FakeCommitNotes satisfies the port.
var _ application.CommitNotes = (*FakeCommitNotes)(nil)

// NewFakeCommitNotes returns a notes fake with nothing recorded.
func NewFakeCommitNotes() *FakeCommitNotes { return &FakeCommitNotes{} }

// AddNote implements application.CommitNotes.
func (f *FakeCommitNotes) AddNote(_ context.Context, rigDir, ref, commit, note string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, AddedNote{RigDir: rigDir, Ref: ref, Commit: commit, Note: note})
	return f.Err
}

// Added reports the notes asked for, in order.
func (f *FakeCommitNotes) Added() []AddedNote {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]AddedNote(nil), f.added...)
}
