package application

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// GristJobName is the name of the follower's grist job in the log: its events'
// actor is grist-grind@host.
const GristJobName = "grist-grind"

// DefaultGristWatchEvery is how often a GristWatch reads the postern backend:
// a grist is ground within seconds of arriving, without a read of the backend
// in every second of the follower's cycle.
const DefaultGristWatchEvery = 5 * time.Second

// gristWatchTimeout is how long one read of the backend may take, so a backend
// that hangs holds the follower's cycle for no longer.
const gristWatchTimeout = 10 * time.Second

// GristWatch notices a new grist record for the mill key at the postern
// backend (postern's docs/protocol.md section 18), for the follower's grist
// job. It reads no grist: whether to answer it is the pass of the mill's.
type GristWatch struct {
	Postern Postern
	// Keys is the mill key file; its public key is the key a grist is for.
	Keys interface {
		PublicKey() (pubKeyHex string, address string, err error)
	}
	// Every is the least time between two reads of the backend; zero is
	// DefaultGristWatchEvery.
	Every time.Duration
	// Now is the clock; nil is time.Now. Err is where a failed read is said,
	// once for each distinct failure; nil says nothing.
	Now func() time.Time
	Err io.Writer

	mu     sync.Mutex
	taken  bool
	newest int64
	last   time.Time
	said   string
}

// Waiting says whether a grist record for the mill key has reached the backend
// since the last time it was asked. The first time only takes the backend's
// bearings: a grist already waiting is left to the dispatch tick. A read that
// fails is not a grist; it is tried again after Every.
func (w *GristWatch) Waiting(ctx context.Context) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	every := w.Every
	if every <= 0 {
		every = DefaultGristWatchEvery
	}
	if w.taken && now().Sub(w.last) < every {
		return "", false
	}
	w.last = now()
	millKey, _, err := w.Keys.PublicKey()
	if err != nil {
		w.say(fmt.Errorf("reading the mill key: %w", err))
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, gristWatchTimeout)
	defer cancel()
	records, err := w.Postern.Messages(ctx, w.newest)
	if err != nil {
		w.say(fmt.Errorf("reading the postern backend for grist: %w", err))
		return "", false
	}
	w.said = ""
	first, found := !w.taken, ""
	w.taken = true
	for _, r := range records {
		w.newest = max(w.newest, r.Seq)
		if r.Class == GristClass && r.To == millKey && r.Txid != "" && found == "" {
			found = "grist " + r.Txid + " arrived"
		}
	}
	return found, found != "" && !first
}

func (w *GristWatch) say(err error) {
	if w.Err != nil && w.said != err.Error() {
		fmt.Fprintf(w.Err, "mw events follow: %v\n", err)
	}
	w.said = err.Error()
}

// GristJob is one pass of the mill (mw grist grind): sprung by a new grist
// record for the mill key, with no clock of its own, for the dispatch tick
// still answers a grist left waiting. A host with no [grist] table has no such
// job. The pass is in flight at most once: a grist that arrives while it runs
// is one more pass when it ends.
func GristJob(watch *GristWatch, run func(context.Context) error) SpringJob {
	return SpringJob{Name: GristJobName, Probe: watch.Waiting, Run: run}
}
