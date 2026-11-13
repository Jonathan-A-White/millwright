package application_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// A release of the ideas that cannot be done is tried again after the wait's
// retry pause, read off the mode's injected clock: the clock moving on past the
// pause tries it again, though no real time has passed (mw-gq6.1218).
func TestAWaitThatCouldNotGiveTheIdeasBackTriesAgainWhenItsInjectedClockPassesTheRetryPause(t *testing.T) {
	until := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	var at atomic.Int64
	at.Store(until.Add(time.Minute).Unix())
	said := &lockedBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		application.WatchIdeasEndFor(ctx, until.Unix(), func() time.Time { return time.Unix(at.Load(), 0) }, writerOf(said))
	}()
	defer func() {
		cancel()
		<-done
	}()

	attempts := func() int { return strings.Count(said.String(), "giving back the ideas") }
	waitForAttempts := func(n int, within time.Duration) {
		t.Helper()
		deadline := time.Now().Add(within)
		for attempts() < n {
			if time.Now().After(deadline) {
				t.Fatalf("expected %d attempts to give the ideas back, saw %d: %q", n, attempts(), said.String())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitForAttempts(1, 30*time.Second)

	// Some looks of the watch later the clock has not passed the pause: no second try.
	time.Sleep(600 * time.Millisecond)
	if n := attempts(); n != 1 {
		t.Fatalf("expected no second attempt before the clock passed the retry pause, saw %d", n)
	}
	at.Add(int64(time.Minute / time.Second))
	// Well inside the real five seconds of the pause, so only the clock can be what passed it.
	waitForAttempts(2, 3*time.Second)
}
