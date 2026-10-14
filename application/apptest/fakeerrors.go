package apptest

import (
	"context"
	"path"
	"sync"
)

// FakeErrorFiles is the common-errors register held in memory: the text of
// each class by slug and the text of the kinds file. Every write is recorded,
// in order, in Writes.
type FakeErrorFiles struct {
	mu      sync.Mutex
	Classes map[string]string
	Kinds   string
	Writes  []string
	// ReadErr, when set, is what every read of the register fails with.
	ReadErr error
}

// ReadErrorRegister implements application.ErrorFiles.
func (f *FakeErrorFiles) ReadErrorRegister(_ context.Context, _ string) (map[string]string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ReadErr != nil {
		return nil, "", f.ReadErr
	}
	classes := make(map[string]string, len(f.Classes))
	for slug, text := range f.Classes {
		classes[slug] = text
	}
	return classes, f.Kinds, nil
}

// WriteErrorClass implements application.ErrorFiles.
func (f *FakeErrorFiles) WriteErrorClass(_ context.Context, seat, slug, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Classes == nil {
		f.Classes = map[string]string{}
	}
	f.Classes[slug] = text
	written := path.Join("seats", seat, "common-errors", slug+".md")
	f.Writes = append(f.Writes, written)
	return written, nil
}

// WriteErrorKinds implements application.ErrorFiles.
func (f *FakeErrorFiles) WriteErrorKinds(_ context.Context, seat, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Kinds = text
	written := path.Join("seats", seat, "common-errors", "kinds.toml")
	f.Writes = append(f.Writes, written)
	return written, nil
}
