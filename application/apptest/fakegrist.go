package apptest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeGrinder is an in-memory application.Grinder: every grind reports
// Result (or Err), and each call is kept with the files its directory held
// while it ran, so a test can see the photos a grind was given after the
// directory is gone. Grinds run side by side: nothing is held between calls.
type FakeGrinder struct {
	mu sync.Mutex

	Result application.SessionResult
	Err    error

	// Entered, when set, is sent on as each grind starts, so a test can wait
	// for one without sleeping. Hold, when set, keeps each grind running until
	// a value is received from it (or it is closed): the test lets one end at
	// a time. A grind whose context ends stops waiting.
	Entered chan struct{}
	Hold    chan struct{}
	// Together, when above 1, keeps each of the first Together grinds running
	// until that many are running at once, so a test shows grinds overlapping
	// without sleeping or racing a clock; the grinds after them are not held.
	Together int

	calls  []GrindSeen
	active int
	most   int
	joined chan struct{}
}

// GrindSeen is one call a FakeGrinder took, and the files in its directory
// then, by name.
type GrindSeen struct {
	Call  application.GrindCall
	Files map[string][]byte
}

// FakeGrinder satisfies the port.
var _ application.Grinder = (*FakeGrinder)(nil)

// Grind implements application.Grinder.
func (f *FakeGrinder) Grind(ctx context.Context, call application.GrindCall) (application.SessionResult, error) {
	f.mu.Lock()
	seen := GrindSeen{Call: call, Files: map[string][]byte{}}
	entries, _ := os.ReadDir(call.Dir)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(call.Dir, e.Name()))
		seen.Files[e.Name()] = data
	}
	f.calls = append(f.calls, seen)
	f.active++
	f.most = max(f.most, f.active)
	var joined chan struct{}
	if f.Together > 1 && len(f.calls) <= f.Together {
		if f.joined == nil {
			f.joined = make(chan struct{})
		}
		joined = f.joined
		if len(f.calls) == f.Together {
			close(f.joined)
		}
	}
	entered, hold := f.Entered, f.Hold
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()

	if entered != nil {
		entered <- struct{}{}
	}
	if joined != nil {
		select {
		case <-joined:
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
			return application.SessionResult{}, fmt.Errorf("the grinds never ran %d at once", f.Together)
		}
	}
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
		}
	}
	if f.Err != nil {
		return application.SessionResult{}, f.Err
	}
	return f.Result, nil
}

// Calls reports every grind run, in order.
func (f *FakeGrinder) Calls() []GrindSeen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]GrindSeen(nil), f.calls...)
}

// MostAtOnce reports the most grinds that were running at the same moment.
func (f *FakeGrinder) MostAtOnce() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.most
}

// FakeGrinds is an in-memory application.GrindSource: each checkout's main
// is at one commit, holding files by path.
type FakeGrinds struct {
	mu sync.Mutex

	commits map[string]string
	files   map[string]map[string][]byte
	// Err, when set, is returned by every method.
	Err error
}

// FakeGrinds satisfies the port.
var _ application.GrindSource = (*FakeGrinds)(nil)

// NewFakeGrinds returns a source with no checkouts.
func NewFakeGrinds() *FakeGrinds {
	return &FakeGrinds{commits: map[string]string{}, files: map[string]map[string][]byte{}}
}

// SetFile puts data at path in the checkout, whose main is at commit.
func (f *FakeGrinds) SetFile(checkout, commit, path string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits[checkout] = commit
	if f.files[checkout] == nil {
		f.files[checkout] = map[string][]byte{}
	}
	f.files[checkout][path] = append([]byte(nil), data...)
}

// Commit implements application.GrindSource.
func (f *FakeGrinds) Commit(_ context.Context, checkout string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	commit, ok := f.commits[checkout]
	if !ok {
		return "", fmt.Errorf("%s is not a checkout", checkout)
	}
	return commit, nil
}

// ReadAt implements application.GrindSource.
func (f *FakeGrinds) ReadAt(_ context.Context, checkout, commit, path string) ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, false, f.Err
	}
	if f.commits[checkout] != commit {
		return nil, false, fmt.Errorf("%s has no commit %s", checkout, commit)
	}
	data, ok := f.files[checkout][path]
	return append([]byte(nil), data...), ok, nil
}

// FakeGristState is an in-memory application.GristState.
type FakeGristState struct {
	mu sync.Mutex

	cursor      int64
	lines       []application.GrindLine
	undelivered map[string]application.GristUndelivered
	// AppendErr, when set, is returned by Append.
	AppendErr error
}

// FakeGristState satisfies the port.
var _ application.GristState = (*FakeGristState)(nil)

// NewFakeGristState returns a state that has read nothing.
func NewFakeGristState() *FakeGristState {
	return &FakeGristState{undelivered: map[string]application.GristUndelivered{}}
}

// Cursor implements application.GristState.
func (f *FakeGristState) Cursor(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cursor, nil
}

// SetCursor implements application.GristState.
func (f *FakeGristState) SetCursor(_ context.Context, seq int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cursor = seq
	return nil
}

// Lines implements application.GristState.
func (f *FakeGristState) Lines(context.Context) ([]application.GrindLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]application.GrindLine(nil), f.lines...), nil
}

// Append implements application.GristState.
func (f *FakeGristState) Append(_ context.Context, line application.GrindLine) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AppendErr != nil {
		return f.AppendErr
	}
	f.lines = append(f.lines, line)
	return nil
}

// KeepUndelivered implements application.GristState.
func (f *FakeGristState) KeepUndelivered(_ context.Context, answer application.GristUndelivered) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.undelivered[answer.Txid] = answer
	return nil
}

// Undelivered implements application.GristState.
func (f *FakeGristState) Undelivered(context.Context) ([]application.GristUndelivered, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := make([]application.GristUndelivered, 0, len(f.undelivered))
	for _, u := range f.undelivered {
		kept = append(kept, u)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Txid < kept[j].Txid })
	return kept, nil
}

// Delivered implements application.GristState.
func (f *FakeGristState) Delivered(_ context.Context, txid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.undelivered, txid)
	return nil
}

// FakeGristLock is an in-memory application.GristLock.
type FakeGristLock struct {
	mu   sync.Mutex
	held bool
	// Taken counts the times it was taken.
	Taken int
}

// FakeGristLock satisfies the port.
var _ application.GristLock = (*FakeGristLock)(nil)

// Hold holds the lock, as another pass or grind would.
func (f *FakeGristLock) Hold() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.held = true
}

// Free lets go of a lock that Hold held, as the other pass or grind ending would.
func (f *FakeGristLock) Free() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.held = false
}

// TryTake implements application.GristLock.
func (f *FakeGristLock) TryTake(context.Context) (func(), bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held {
		return nil, false, nil
	}
	f.held = true
	f.Taken++
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.held = false
	}, true, nil
}

// Held implements application.GristLock.
func (f *FakeGristLock) Held(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.held, nil
}
