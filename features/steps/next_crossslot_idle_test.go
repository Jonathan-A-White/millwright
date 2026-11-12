package steps

import (
	"strings"
	"testing"
	"time"
)

// A landing that keeps moving is waited for however long it takes: the other
// host's landing is slow on a loaded host (mw-gq6.907: load 35 to 55 of 16 cores
// passed the old fixed 15 s), and a fixed wall-clock deadline failed the
// scenario on that, not on the other host being wrong.
func TestIdleWaitsForALandingThatKeepsMakingProgress(t *testing.T) {
	// The stall limit is far over the 50 ms between steps: a goroutine starved for
	// a few hundred ms on a loaded host is not a stalled landing (mw-gq6.1155).
	l := &otherLander{stall: 5 * time.Second, busy: true}
	go func() {
		for i := 0; i < 12; i++ {
			time.Sleep(50 * time.Millisecond)
			l.mu.Lock()
			l.progress++
			l.mu.Unlock()
		}
		l.mu.Lock()
		l.busy = false
		l.progress++
		l.mu.Unlock()
	}()
	if err := l.idle(); err != nil {
		t.Fatalf("a landing that moved every 50 ms for 600 ms failed a 5 s stall limit: %v", err)
	}
}

// idle itself can be starved: on a busy host its goroutine may not run for longer
// than the stall limit, and a landing that moved meanwhile is not a stalled one
// (mw-gq6.1155). The pause stands in for that starvation, deterministically: the
// other host takes a step during it and finishes just after, and idle is not
// scheduled again until the whole stall limit has gone by.
func TestIdleDoesNotCallAStarvedWatcherAStalledLanding(t *testing.T) {
	l := &otherLander{stall: 100 * time.Millisecond, busy: true}
	paused := 0
	l.pause = func() {
		paused++
		if paused == 1 {
			time.Sleep(300 * time.Millisecond)
			l.mu.Lock()
			l.progress++
			l.mu.Unlock()
			return
		}
		l.mu.Lock()
		l.busy = false
		l.progress++
		l.mu.Unlock()
	}
	if err := l.idle(); err != nil {
		t.Fatalf("a landing that took a step while idle was not scheduled for 300 ms failed a 100 ms stall limit: %v", err)
	}
}

// A landing that stops moving is still failed, after the stall limit.
func TestIdleFailsALandingThatStopsMoving(t *testing.T) {
	l := &otherLander{stall: 100 * time.Millisecond, busy: true}
	start := time.Now()
	err := l.idle()
	if err == nil || !strings.Contains(err.Error(), "still landing") {
		t.Fatalf("a landing that never finished passed: %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("a stuck landing took %s to be failed", took)
	}
}
