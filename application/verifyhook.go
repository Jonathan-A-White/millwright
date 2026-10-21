package application

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
)

// DefaultVerifyHookTimeout bounds one run the hook starts: the match's own
// agent call (verifyTimeout) with its reads and writes around it.
const DefaultVerifyHookTimeout = verifyTimeout + 5*time.Minute

// PicturesHook is what the inbox pass tells of the Governor messages it applied
// with a picture saved: Saved for each one as it is applied, Flush once when the
// pass ends. Neither waits for what the hook goes on to do. Wait returns when
// the run begun has ended: the follower's pass never calls it, and the --apply
// pass, a short process, does before it exits (mw-4xdnis.6).
type PicturesHook interface {
	Saved(txid string)
	Flush(ctx context.Context)
	Wait()
}

// VerifyHook is the real-time half of mw verify match: the txids of the pictures
// a pass saved are matched in a run begun when the pass ends, apart from the
// pass, which never waits on it. One run is in flight at most once; a txid saved
// while it runs is queued, and the next run matches every txid queued, with
// --txid for each. A run that fails is said on Err and costs the pass nothing.
type VerifyHook struct {
	// Match makes one run over txids, as mw verify match --txid does, and says
	// how many proposals it made.
	Match func(ctx context.Context, txids []string) (VerifyResult, error)
	// Timeout bounds a run; zero is DefaultVerifyHookTimeout.
	Timeout time.Duration
	// Err is where a run says what it did, one line each. A nil Err says nothing.
	Err io.Writer
	// Who begins each of those lines: the command that began the run. Empty is
	// the follower's, "mw events follow".
	Who string

	mu      sync.Mutex
	pending []string
	running bool
	wg      sync.WaitGroup
}

// Saved queues txid for the next run; Flush begins it.
func (h *VerifyHook) Saved(txid string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if txid != "" && !slices.Contains(h.pending, txid) {
		h.pending = append(h.pending, txid)
	}
}

// Flush begins a run over the txids queued, unless one is in flight: that one
// takes them when it ends. It returns at once.
func (h *VerifyHook) Flush(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running || len(h.pending) == 0 || ctx.Err() != nil {
		return
	}
	h.running = true
	h.wg.Add(1)
	go h.drain(ctx)
}

// Wait returns when the run in flight, and any queued behind it, has ended.
func (h *VerifyHook) Wait() { h.wg.Wait() }

// drain runs the queue until it is empty.
func (h *VerifyHook) drain(ctx context.Context) {
	defer h.wg.Done()
	for {
		h.mu.Lock()
		txids := h.pending
		h.pending = nil
		if len(txids) == 0 || ctx.Err() != nil {
			h.running = false
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()
		h.run(ctx, txids)
	}
}

func (h *VerifyHook) run(ctx context.Context, txids []string) {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = DefaultVerifyHookTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := h.Match(runCtx, txids)
	if h.Err == nil {
		return
	}
	names, who := strings.Join(txids, " "), h.Who
	if who == "" {
		who = "mw events follow"
	}
	if err != nil {
		fmt.Fprintf(h.Err, "%s: verify match %s: %v\n", who, names, err)
		return
	}
	fmt.Fprintf(h.Err, "%s: verify match %s: %d proposed\n", who, names, len(result.Proposed))
}
