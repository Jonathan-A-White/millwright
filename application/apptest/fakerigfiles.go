package apptest

import (
	"path"
	"strings"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// FakeRigFiles is an in-memory application.RigFiles: the paths each checkout
// holds, by the directory the checkout is at.
type FakeRigFiles struct {
	mu    sync.Mutex
	files map[string]map[string]bool
	// callers is the test callers each checkout is said to have, and the
	// stems asked for the last time.
	callers map[string]int
	asked   []string
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
// holds a file it matches as path.Match has it, or a directory: one of the
// directories a file it holds is under.
func (f *FakeRigFiles) Exists(rigDir, pattern string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name := range f.files[rigDir] {
		for held := name; held != "." && held != "/"; held = path.Dir(held) {
			if ok, _ := path.Match(pattern, held); ok {
				return true
			}
		}
	}
	return false
}

// FilesIn implements application.RigFiles: the files the checkout holds under
// dir, counted no further than one past most; dir is a directory when it holds
// any.
func (f *FakeRigFiles) FilesIn(rigDir, dir string, most int) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int
	for name := range f.files[rigDir] {
		if dir == "" || strings.HasPrefix(name, dir+"/") {
			n++
		}
	}
	return min(n, most+1), n > 0
}

// Matching implements application.RigFiles: the files the checkout holds that
// the glob covers, counted no further than one past most.
func (f *FakeRigFiles) Matching(rigDir, pattern string, most int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int
	for name := range f.files[rigDir] {
		if domain.Covers(pattern, name) {
			n++
		}
	}
	return min(n, most+1)
}

// SetTestCallers says how many test files the checkout at rigDir has that name
// anything the files asked for declare.
func (f *FakeRigFiles) SetTestCallers(rigDir string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.callers == nil {
		f.callers = map[string]int{}
	}
	f.callers[rigDir] = n
}

// AskedFiles are the files the last TestCallers was asked for.
func (f *FakeRigFiles) AskedFiles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// TestCallers implements application.RigFiles.
func (f *FakeRigFiles) TestCallers(rigDir string, files []string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append([]string(nil), files...)
	return f.callers[rigDir], nil
}
