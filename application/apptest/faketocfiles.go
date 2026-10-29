package apptest

import (
	"fmt"
	"sort"
)

// FakeTocFiles is an in-memory application.TocFiles: Docs holds the text of each
// file by path, and Untracked the paths among them that git does not track.
type FakeTocFiles struct {
	Docs      map[string]string
	Untracked map[string]bool
	// Writes counts the files written.
	Writes int
}

// NewFakeTocFiles is a fake with no files.
func NewFakeTocFiles() *FakeTocFiles {
	return &FakeTocFiles{Docs: map[string]string{}, Untracked: map[string]bool{}}
}

// Tracked is every file that is not Untracked, by path.
func (f *FakeTocFiles) Tracked() ([]string, error) {
	var out []string
	for path := range f.Docs {
		if !f.Untracked[path] {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Read is the text of the file at path.
func (f *FakeTocFiles) Read(path string) (string, error) {
	text, ok := f.Docs[path]
	if !ok {
		return "", fmt.Errorf("%s: no such file", path)
	}
	return text, nil
}

// Write sets the text of the file at path.
func (f *FakeTocFiles) Write(path, text string) error {
	f.Docs[path] = text
	f.Writes++
	return nil
}
