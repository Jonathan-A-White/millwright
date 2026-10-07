package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeScorer is an in-memory application.Scorer: every reading scores as
// Result (or fails with Err), and each call is kept so a test can see what
// the engine was given.
type FakeScorer struct {
	mu sync.Mutex

	Result application.ReadingResult
	Err    error

	calls []ScorerCall
}

// ScorerCall is one reading a FakeScorer was asked to score.
type ScorerCall struct {
	Audio  []byte
	Mime   string
	Target string
	Lang   string
}

// FakeScorer satisfies the port.
var _ application.Scorer = (*FakeScorer)(nil)

// Score implements application.Scorer.
func (f *FakeScorer) Score(_ context.Context, audio []byte, mime, targetText, lang string) (application.ReadingResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ScorerCall{Audio: audio, Mime: mime, Target: targetText, Lang: lang})
	if f.Err != nil {
		return application.ReadingResult{}, f.Err
	}
	return f.Result, nil
}

// Calls reports every reading scored, in order.
func (f *FakeScorer) Calls() []ScorerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ScorerCall(nil), f.calls...)
}
