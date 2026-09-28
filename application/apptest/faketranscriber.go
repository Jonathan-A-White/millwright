package apptest

import (
	"context"
	"os"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeTranscriber is an in-memory application.PosternTranscriber: it hears
// Transcript in whatever it is given, and remembers every file it was asked
// to hear and what that file held when it was asked.
type FakeTranscriber struct {
	mu sync.Mutex

	// Transcript is what every call hears.
	Transcript string
	// Err, when set, is returned instead of a transcript.
	Err error

	paths    []string
	contents [][]byte
}

// FakeTranscriber satisfies the port.
var _ application.PosternTranscriber = (*FakeTranscriber)(nil)

// Transcribe implements application.PosternTranscriber.
func (f *FakeTranscriber) Transcribe(_ context.Context, audioPath string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, _ := os.ReadFile(audioPath)
	f.paths = append(f.paths, audioPath)
	f.contents = append(f.contents, data)
	if f.Err != nil {
		return "", f.Err
	}
	return f.Transcript, nil
}

// Heard reports the files Transcribe was asked to hear, in order, and what
// each held.
func (f *FakeTranscriber) Heard() ([]string, [][]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...), append([][]byte(nil), f.contents...)
}
