package application

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// poolSession is a WarmSession that answers nothing and says whether it was
// retired.
type poolSession struct {
	mu      sync.Mutex
	retired bool
}

func (s *poolSession) Answer(context.Context, string, time.Duration) (SessionResult, error) {
	return SessionResult{}, errors.New("not here")
}
func (s *poolSession) Exited() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.retired }
func (s *poolSession) Retire()      { s.mu.Lock(); defer s.mu.Unlock(); s.retired = true }

type poolWarmer struct{ calls []GrindCall }

func (w *poolWarmer) Warm(call GrindCall) (WarmSession, error) {
	w.calls = append(w.calls, call)
	return &poolSession{}, nil
}

type poolBoard struct {
	saved []GristPoolView
	err   error
}

func (b *poolBoard) SavePool(_ context.Context, view GristPoolView) error {
	if b.err != nil {
		return b.err
	}
	b.saved = append(b.saved, view)
	return nil
}

func (b *poolBoard) Pool(context.Context) (GristPoolView, error) {
	if len(b.saved) == 0 {
		return GristPoolView{}, b.err
	}
	return b.saved[len(b.saved)-1], b.err
}

var poolSpec = gristWarmSpec{app: "lampas", kind: "bible-talk", v: "1", model: "sonnet", effort: "low", instructions: "Talk.", schema: `{"type":"object"}`}

// A kind with more grists in flight at once than the mill has slots keeps
// only as many warm sessions as slots; each warm session runs in a private
// directory of its own, which is gone once it is retired (a session taken is
// its grind's to remove).
func TestAKindKeepsNoMoreWarmSessionsThanSlots(t *testing.T) {
	ctx, now := context.Background(), roomNowFor(t)
	warmer := &poolWarmer{}
	pool := &GristPool{Warmer: warmer, Slots: 2, TempDir: t.TempDir(), Now: func() time.Time { return now }}
	for range 3 {
		pool.take(ctx, poolSpec, BillingSubscription)
	}
	view := pool.View()
	if len(view.Kinds) != 1 || view.Kinds[0].Warm != 2 || view.Kinds[0].Busy != 3 {
		t.Fatalf("expected 2 warm and 3 busy, got %+v", view)
	}
	dirs := map[string]bool{}
	for _, call := range warmer.calls {
		if _, err := os.Stat(call.Dir); err != nil || dirs[call.Dir] {
			t.Fatalf("expected each warm session a directory of its own, got %q (%v)", call.Dir, err)
		}
		dirs[call.Dir] = true
	}
	var idle []string
	for _, w := range pool.kinds[gristPoolKey{"lampas", "bible-talk"}].idle {
		idle = append(idle, w.dir)
	}
	pool.Close()
	for _, dir := range idle {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed once its session was retired, got %v", dir, err)
		}
	}
}

// The board is saved whenever what the pool holds changes, at least every
// heartbeat while the pool is ticked, and not otherwise; Close leaves it empty.
func TestThePoolSavesItsBoardOnChangeAndHeartbeat(t *testing.T) {
	ctx, now := context.Background(), roomNowFor(t)
	board := &poolBoard{}
	pool := &GristPool{Warmer: &poolWarmer{}, Slots: 2, TempDir: t.TempDir(), Board: board, Now: func() time.Time { return now }}
	pool.take(ctx, poolSpec, BillingSubscription)
	pool.ended(ctx, poolSpec)
	if len(board.saved) != 2 {
		t.Fatalf("expected the board saved at the take and at the end, got %d", len(board.saved))
	}
	now = now.Add(30 * time.Second)
	pool.Tick(ctx)
	if len(board.saved) != 2 {
		t.Fatalf("expected no save within the heartbeat, got %d", len(board.saved))
	}
	now = now.Add(GristPoolHeartbeat)
	pool.Tick(ctx)
	if len(board.saved) != 3 || !board.saved[2].At.Equal(now) {
		t.Fatalf("expected the heartbeat saved, got %+v", board.saved)
	}
	pool.Close()
	if last := board.saved[len(board.saved)-1]; len(last.Kinds) != 0 {
		t.Fatalf("expected an empty board after Close, got %+v", last)
	}
}

// mw status reads a board saved within GristPoolStale and says it in one
// line; an older board is no running pool's, and says nothing.
func TestStatusReadsTheBoardOnlyWhileItIsFresh(t *testing.T) {
	now := roomNowFor(t)
	board := &poolBoard{saved: []GristPoolView{{At: now.Add(-time.Minute), Kinds: []GristPoolKind{
		{App: "cairn", Kind: "sweep", Warm: 0, Busy: 1}, {App: "lampas", Kind: "bible-talk", Warm: 2, Busy: 1},
	}}}}
	room := GristRoom{Pool: board, Now: func() time.Time { return now }}
	pulse, err := room.Pulse(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pulse.PoolLine(), "grist pool: cairn/sweep 0 warm, 1 busy; lampas/bible-talk 2 warm, 1 busy"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	board.saved[0].At = now.Add(-GristPoolStale)
	if pulse, _ := room.Pulse(context.Background(), 4); pulse.PoolLine() != "" {
		t.Fatalf("expected a stale board to say nothing, got %q", pulse.PoolLine())
	}
	if got := (GristPoolView{At: now}).Line(); got != "grist pool: none warm" {
		t.Fatalf("expected an empty pool said, got %q", got)
	}
}

// A pool that cannot start a warm session tries again only at the kind's
// next grist, never at each tick.
func TestAWarmSessionThatCannotStartIsNotRetriedEachTick(t *testing.T) {
	ctx, now := context.Background(), roomNowFor(t)
	warmer := &failingWarmer{}
	pool := &GristPool{Warmer: warmer, Slots: 2, TempDir: t.TempDir(), Now: func() time.Time { return now }}
	pool.take(ctx, poolSpec, BillingSubscription)
	for range 5 {
		pool.Tick(ctx)
	}
	if warmer.tries != 1 {
		t.Fatalf("expected one try, got %d", warmer.tries)
	}
	pool.take(ctx, poolSpec, BillingSubscription)
	if warmer.tries != 2 {
		t.Fatalf("expected one more try at the next grist, got %d", warmer.tries)
	}
}

type failingWarmer struct{ tries int }

func (w *failingWarmer) Warm(GrindCall) (WarmSession, error) {
	w.tries++
	return nil, errors.New("claude: not logged in")
}

func roomNowFor(*testing.T) time.Time { return time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC) }
