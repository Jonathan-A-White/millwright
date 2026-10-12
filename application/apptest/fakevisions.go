package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeVisions is an in-memory application.VisionSource: each checkout holds a
// VISION.md or does not.
type FakeVisions struct {
	mu    sync.Mutex
	texts map[string]string
	// Err, when set, is returned by Read.
	Err error
}

// FakeVisions satisfies the port.
var _ application.VisionSource = (*FakeVisions)(nil)

// NewFakeVisions returns a source in which no checkout has a VISION.md.
func NewFakeVisions() *FakeVisions {
	return &FakeVisions{texts: map[string]string{}}
}

// Set gives the checkout a VISION.md holding text.
func (f *FakeVisions) Set(checkout, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts[checkout] = text
}

// Read implements application.VisionSource.
func (f *FakeVisions) Read(_ context.Context, checkout string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", false, f.Err
	}
	text, ok := f.texts[checkout]
	return text, ok, nil
}
