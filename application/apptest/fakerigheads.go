package apptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeRigHeads is an application.RigHeads over commits it is told of: each
// rig's directory has a head and any older commits, with the subject
// "Subject of <commit>".
type FakeRigHeads struct {
	mu      sync.Mutex
	heads   map[string]string
	commits map[string]map[string]bool
	// Branch is the default branch every rig reports; "main" when empty.
	Branch string
}

var _ application.RigHeads = (*FakeRigHeads)(nil)

// NewFakeRigHeads returns a fake that knows no rig.
func NewFakeRigHeads() *FakeRigHeads {
	return &FakeRigHeads{heads: map[string]string{}, commits: map[string]map[string]bool{}}
}

// SetHead makes commit the head of the checkout at dir.
func (f *FakeRigHeads) SetHead(dir, commit string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heads[dir] = commit
	f.addLocked(dir, commit)
}

// AddCommit makes commit one the checkout at dir can resolve, not its head.
func (f *FakeRigHeads) AddCommit(dir, commit string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addLocked(dir, commit)
}

func (f *FakeRigHeads) addLocked(dir, commit string) {
	if f.commits[dir] == nil {
		f.commits[dir] = map[string]bool{}
	}
	f.commits[dir][commit] = true
}

// Resolve implements application.RigHeads.
func (f *FakeRigHeads) Resolve(_ context.Context, dir, rev string) (application.RigCommit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	branch := f.Branch
	if branch == "" {
		branch = "main"
	}
	commit := rev
	if rev == "" {
		commit = f.heads[dir]
	}
	if commit == "" || !f.commits[dir][commit] {
		return application.RigCommit{}, fmt.Errorf("no commit %q in %s", rev, dir)
	}
	return application.RigCommit{Branch: branch, Commit: commit, Subject: "Subject of " + commit}, nil
}
