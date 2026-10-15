package apptest

import (
	"path"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeRigFiles is an in-memory application.RigFiles: the paths each checkout
// holds, by the directory the checkout is at.
type FakeRigFiles struct {
	mu    sync.Mutex
	files map[string]map[string]bool
}

var _ application.RigFiles = (*FakeRigFiles)(nil)

// NewFakeRigFiles returns checkouts that hold nothing.
func NewFakeRigFiles() *FakeRigFiles {
	return &FakeRigFiles{files: map[string]map[string]bool{}}
}

// Add puts files in the checkout at rigDir.
func (f *FakeRigFiles) Add(rigDir string, names ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.files[rigDir] == nil {
		f.files[rigDir] = map[string]bool{}
	}
	for _, name := range names {
		f.files[rigDir][name] = true
	}
}

// Exists implements application.RigFiles: a pattern is held when the checkout
// holds a file it matches as path.Match has it.
func (f *FakeRigFiles) Exists(rigDir, pattern string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name := range f.files[rigDir] {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}
