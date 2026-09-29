package apptest

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.PosternHomeMover = (*FakeHomeMover)(nil)

// FakeHomeMover stands in for ssh to the old home and for mw home move: Answers
// is whether the old home answers, Outcome what every move reports. It runs
// nothing and remembers every move asked of it.
type FakeHomeMover struct {
	mu sync.Mutex

	Answers bool
	Outcome application.HomeMoveOutcome

	moves []string
	spent map[string]bool
}

// Spend implements application.PosternHomeMover.
func (f *FakeHomeMover) Spend(_ context.Context, txid string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.spent[txid] {
		return false, nil
	}
	if f.spent == nil {
		f.spent = map[string]bool{}
	}
	f.spent[txid] = true
	return true, nil
}

// OldHomeAnswers implements application.PosternHomeMover.
func (f *FakeHomeMover) OldHomeAnswers(context.Context, []string, time.Duration) (bool, error) {
	return f.Answers, nil
}

// MoveHome implements application.PosternHomeMover.
func (f *FakeHomeMover) MoveHome(_ context.Context, args []string) (application.HomeMoveOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, strings.Join(args, " "))
	return f.Outcome, nil
}

// Moves reports every move asked for, each its arguments after `mw home move`
// joined by spaces, in order.
func (f *FakeHomeMover) Moves() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.moves...)
}
