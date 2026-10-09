package application

import (
	"context"
	"sync"
	"time"
)

// GrindRefreshEvery is how often the mill asks a rig's remote for its main:
// at most once a minute per rig, however many grists come.
const GrindRefreshEvery = time.Minute

// GrindRefreshes remembers when each rig's checkout was last refreshed, and
// what came of it, so that grists arriving together fetch once.
type GrindRefreshes struct {
	// Every is the least time between two fetches of one rig.
	Every time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu   sync.Mutex
	rigs map[string]*grindRefresh
}

// grindRefresh is one rig's last refresh. Its own lock is held across the
// fetch, so a second grist for the rig waits for the first's answer rather
// than fetching again, while other rigs go on.
type grindRefresh struct {
	mu   sync.Mutex
	at   time.Time
	done bool
	err  error
}

// NewGrindRefreshes is a memory that lets one refresh of a rig through per
// every.
func NewGrindRefreshes(every time.Duration) *GrindRefreshes {
	return &GrindRefreshes{Every: every}
}

// Refresh asks src to refresh checkout unless that was done less than Every
// ago, and reports what the last refresh said: a refresh that failed is
// reported to every grist until the next try, and is not retried sooner.
func (r *GrindRefreshes) Refresh(ctx context.Context, src GrindSource, checkout string) error {
	r.mu.Lock()
	if r.rigs == nil {
		r.rigs = map[string]*grindRefresh{}
	}
	rig, ok := r.rigs[checkout]
	if !ok {
		rig = &grindRefresh{}
		r.rigs[checkout] = rig
	}
	r.mu.Unlock()

	rig.mu.Lock()
	defer rig.mu.Unlock()
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	if rig.done && now().Sub(rig.at) < r.Every {
		return rig.err
	}
	rig.err = src.Refresh(ctx, checkout)
	rig.at, rig.done = now(), true
	return rig.err
}
