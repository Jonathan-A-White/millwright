package beads

import (
	"sync"
	"time"
)

// The follower reads the same few notes on every pass (the month's review
// filed, the harness hold, the inbox cursor), and each read starts a bd that
// opens several connections to the beads server (mw-gq6.596). A Gateway told
// to keep notes reads each from bd once in a wait.

// WithNoteCache makes Note answer from memory for ttl after it last read a key
// from bd, or last wrote it through this Gateway. A key that is not there is
// kept as not there. What another process writes is seen when the wait ends,
// so a Gateway whose readers cannot wait that long keeps none.
func WithNoteCache(ttl time.Duration) Option {
	return func(g *Gateway) {
		if ttl > 0 {
			g.notes = &noteCache{ttl: ttl, entries: map[string]keptNote{}}
		}
	}
}

type keptNote struct {
	value string
	at    time.Time
}

type noteCache struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]keptNote
}

// get is the kept value of key and whether it is still fresh at now.
func (c *noteCache) get(key string, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	kept, ok := c.entries[key]
	if !ok || now.Sub(kept.at) >= c.ttl {
		return "", false
	}
	return kept.value, true
}

func (c *noteCache) put(key, value string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = keptNote{value: value, at: now}
}

func (c *noteCache) forget(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
