package apptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeTranscriptTail is an in-memory application.TranscriptTail: a test says
// what the harness has recorded in a directory with Record.
type FakeTranscriptTail struct {
	mu       sync.Mutex
	recorded map[string]string
}

// NewFakeTranscriptTail returns a harness that has recorded nothing anywhere.
func NewFakeTranscriptTail() *FakeTranscriptTail {
	return &FakeTranscriptTail{recorded: map[string]string{}}
}

var _ application.TranscriptTail = (*FakeTranscriptTail)(nil)

// Record sets what the harness has recorded of the session run in dir.
func (f *FakeTranscriptTail) Record(dir, transcript string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded[dir] = transcript
}

// Tail implements application.TranscriptTail.
func (f *FakeTranscriptTail) Tail(_ context.Context, dir string, lines int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return application.RecentLines(f.recorded[dir], lines), nil
}

// FakePeekRemote is an in-memory application.PeekRemote: a test says what each
// host prints, or why it cannot be reached, and reads back which were asked.
type FakePeekRemote struct {
	mu      sync.Mutex
	prints  map[string]string
	refuses map[string]error
	asked   []string
}

// NewFakePeekRemote returns a remote that reaches no host.
func NewFakePeekRemote() *FakePeekRemote {
	return &FakePeekRemote{prints: map[string]string{}, refuses: map[string]error{}}
}

var _ application.PeekRemote = (*FakePeekRemote)(nil)

// Prints makes host answer a peek with text.
func (f *FakePeekRemote) Prints(host, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prints[host] = text
}

// Unreachable makes a peek at host fail with err.
func (f *FakePeekRemote) Unreachable(host string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refuses[host] = err
}

// Asked reports the hosts peeked at, oldest first.
func (f *FakePeekRemote) Asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// Peek implements application.PeekRemote.
func (f *FakePeekRemote) Peek(_ context.Context, host, storyID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, host)
	if err := f.refuses[host]; err != nil {
		return "", err
	}
	text, ok := f.prints[host]
	if !ok {
		return "", fmt.Errorf("no way to reach %s", host)
	}
	return text, nil
}
