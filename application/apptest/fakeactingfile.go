package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeActingFile is an in-memory application.ActingFile: the text last
// written to each seat's acting file.
type FakeActingFile struct {
	mu    sync.Mutex
	texts map[string]string
	// Err, when set, is what WriteActing returns.
	Err error
}

var _ application.ActingFile = (*FakeActingFile)(nil)

// WriteActing implements application.ActingFile.
func (f *FakeActingFile) WriteActing(_ context.Context, seat, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	if f.texts == nil {
		f.texts = map[string]string{}
	}
	f.texts[seat] = text
	return nil
}

// Text is what the seat's acting file holds, empty when nothing was written.
func (f *FakeActingFile) Text(seat string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.texts[seat]
}
