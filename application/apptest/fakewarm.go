package apptest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeWarmGrinder is an in-memory application.WarmGrinder: every session it
// starts is kept, answers with Result (or AnswerErr), and remembers each
// message it was given with the files its directory held then, so a test can
// see that no session answered twice.
type FakeWarmGrinder struct {
	mu sync.Mutex

	// Result is what each session answers with; ResultOf, when set, is asked
	// instead as each answers (a scenario's cold grinder's own result).
	Result   application.SessionResult
	ResultOf func() application.SessionResult
	// StartErr, when set, is returned by Warm, which then starts nothing;
	// AnswerErr by every session given its message.
	StartErr  error
	AnswerErr error
	// Together, when above 1, keeps each of the first Together answers waiting
	// until that many sessions are answering at once, so a test shows grists
	// overlapping without sleeping or racing a clock.
	Together int

	sessions []*FakeWarmSession
	answers  int
	joined   chan struct{}
}

// FakeWarmGrinder satisfies the port.
var _ application.WarmGrinder = (*FakeWarmGrinder)(nil)

// Warm implements application.WarmGrinder.
func (f *FakeWarmGrinder) Warm(call application.GrindCall) (application.WarmSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StartErr != nil {
		return nil, f.StartErr
	}
	s := &FakeWarmSession{g: f, Call: call}
	f.sessions = append(f.sessions, s)
	return s, nil
}

// join counts one answer begun and is what it waits on to have company: nil
// when it is not one of the first Together.
func (f *FakeWarmGrinder) join() chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers++
	if f.Together <= 1 || f.answers > f.Together {
		return nil
	}
	if f.joined == nil {
		f.joined = make(chan struct{})
	}
	if f.answers == f.Together {
		close(f.joined)
	}
	return f.joined
}

// Sessions reports every session started, in order.
func (f *FakeWarmGrinder) Sessions() []*FakeWarmSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*FakeWarmSession(nil), f.sessions...)
}

// FakeWarmSession is one session a FakeWarmGrinder started.
type FakeWarmSession struct {
	g *FakeWarmGrinder
	// Call is what the session was started with: its Prompt is empty.
	Call application.GrindCall

	mu       sync.Mutex
	messages []GrindSeen
	retired  bool
	exited   bool
}

// FakeWarmSession satisfies the port.
var _ application.WarmSession = (*FakeWarmSession)(nil)

// Answer implements application.WarmSession. A session answers however many
// messages it is given, so that a test sees a second one rather than a pool
// that hides it.
func (s *FakeWarmSession) Answer(ctx context.Context, prompt string, _ time.Duration) (application.SessionResult, error) {
	s.mu.Lock()
	call := s.Call
	call.Prompt = prompt
	seen := GrindSeen{Call: call, Files: map[string][]byte{}}
	entries, _ := os.ReadDir(call.Dir)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(call.Dir, e.Name()))
		seen.Files[e.Name()] = data
	}
	s.messages = append(s.messages, seen)
	s.exited = true
	s.mu.Unlock()

	if joined := s.g.join(); joined != nil {
		select {
		case <-joined:
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
			return application.SessionResult{}, fmt.Errorf("the warm sessions never answered %d at once", s.g.Together)
		}
	}
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	if s.g.AnswerErr != nil {
		return application.SessionResult{}, s.g.AnswerErr
	}
	if s.g.ResultOf != nil {
		return s.g.ResultOf(), nil
	}
	return s.g.Result, nil
}

// Exited implements application.WarmSession.
func (s *FakeWarmSession) Exited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

// Retire implements application.WarmSession.
func (s *FakeWarmSession) Retire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retired, s.exited = true, true
}

// Exit ends the session on its own, as a harness that dies would.
func (s *FakeWarmSession) Exit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exited = true
}

// Messages reports each message the session was given, in order.
func (s *FakeWarmSession) Messages() []GrindSeen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]GrindSeen(nil), s.messages...)
}

// Retired says the session was retired.
func (s *FakeWarmSession) Retired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retired
}

// Running says the session is still waiting: not retired, not given a
// message, not exited.
func (s *FakeWarmSession) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.exited
}
