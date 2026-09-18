package apptest

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeRunner is an in-memory application.Runner. Nothing runs: a test writes
// the session's output itself with Write and ends it with Exit, so that a use
// case can be walked through a session's whole life without a terminal.
type FakeRunner struct {
	mu       sync.Mutex
	sessions map[string]*fakeSession
	order    []string

	// Err, when set, is returned by every method instead of doing the work.
	Err error
}

// fakeSession is one session as the fake remembers it.
type fakeSession struct {
	spec   application.SessionSpec
	output strings.Builder
	input  []string
	status application.SessionStatus
	// done is closed when the session's command exits, so that Wait can block
	// on it rather than poll.
	done chan struct{}
}

// NewFakeRunner returns a runner with no sessions in it.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{sessions: map[string]*fakeSession{}}
}

// FakeRunner satisfies the port.
var _ application.Runner = (*FakeRunner)(nil)

// Start implements application.Runner.
func (f *FakeRunner) Start(_ context.Context, spec application.SessionSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	if _, running := f.sessions[spec.Name]; running {
		return fmt.Errorf("session %q is already running", spec.Name)
	}
	f.sessions[spec.Name] = &fakeSession{
		spec:   spec,
		status: application.SessionStatus{Name: spec.Name, State: application.StateRunning},
		done:   make(chan struct{}),
	}
	f.order = append(f.order, spec.Name)
	return nil
}

// Send implements application.Runner.
func (f *FakeRunner) Send(_ context.Context, name, input string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	s, ok := f.sessions[name]
	if !ok {
		return fmt.Errorf("no session %q", name)
	}
	s.input = append(s.input, input)
	return nil
}

// Output implements application.Runner.
func (f *FakeRunner) Output(_ context.Context, name string, lines int) (string, error) {
	if lines < 1 {
		return "", fmt.Errorf("reading a session needs at least 1 line, got %d", lines)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	s, ok := f.sessions[name]
	if !ok {
		return "", fmt.Errorf("no session %q", name)
	}
	return application.RecentLines(s.output.String(), lines), nil
}

// Status implements application.Runner.
func (f *FakeRunner) Status(_ context.Context, name string) (application.SessionStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return application.SessionStatus{}, f.Err
	}
	return f.status(name), nil
}

// Wait implements application.Runner.
func (f *FakeRunner) Wait(ctx context.Context, name string) (application.SessionStatus, error) {
	f.mu.Lock()
	if f.Err != nil {
		err := f.Err
		f.mu.Unlock()
		return application.SessionStatus{}, err
	}
	s, ok := f.sessions[name]
	if !ok {
		status := f.status(name)
		f.mu.Unlock()
		return status, nil
	}
	done := s.done
	f.mu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
		status, err := f.Status(ctx, name)
		if err != nil {
			return application.SessionStatus{}, err
		}
		return status, ctx.Err()
	}
	return f.Status(ctx, name)
}

// Close implements application.Runner.
func (f *FakeRunner) Close(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	if s, ok := f.sessions[name]; ok {
		f.finish(s, s.status.ExitCode)
		delete(f.sessions, name)
	}
	return nil
}

// Write adds to what a session has printed. A test calls it to stand in for the
// command's output.
func (f *FakeRunner) Write(name, output string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[name]; ok {
		s.output.WriteString(output)
	}
}

// Exit ends a session's command with an exit status, as if it had finished. The
// session stays, with its output, until it is closed.
func (f *FakeRunner) Exit(name string, exitCode int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[name]; ok {
		f.finish(s, exitCode)
	}
}

// Spec reports how a session was started, and whether it is there at all.
func (f *FakeRunner) Spec(name string) (application.SessionSpec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[name]
	if !ok {
		return application.SessionSpec{}, false
	}
	return s.spec, true
}

// Input reports what was sent to a session, oldest first.
func (f *FakeRunner) Input(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[name]
	if !ok {
		return nil
	}
	return append([]string(nil), s.input...)
}

// Names reports the sessions the runner holds, in the order they were started.
func (f *FakeRunner) Names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.sessions))
	for _, name := range f.order {
		if _, ok := f.sessions[name]; ok {
			names = append(names, name)
		}
	}
	return names
}

// status reports a session's status under the lock; a session the fake does not
// hold is gone.
func (f *FakeRunner) status(name string) application.SessionStatus {
	s, ok := f.sessions[name]
	if !ok {
		return application.SessionStatus{Name: name, State: application.StateGone}
	}
	return s.status
}

// finish marks a session's command as exited, once, under the lock.
func (f *FakeRunner) finish(s *fakeSession, exitCode int) {
	if s.status.State == application.StateRunning {
		s.status.State = application.StateExited
		s.status.ExitCode = exitCode
		close(s.done)
	}
}
