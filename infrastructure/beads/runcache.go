package beads

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// WithRunCache makes the Gateway ask bd once, in its life, for what a command
// that only reads needs again and again: the list of every bead, and the table
// of every note. mw status wants the bead list for its snapshot, its bead
// graph, its stuck check and its causes, and the notes for the Mayor's needs,
// the dampers, the icebox and each host's keys; each asked bd afresh, five
// lists of 2 to 4.5 s and eight kv lists a run (mw-gq6.659). With the cache
// those are one read each, and a molecule's open steps, an epic's children,
// BeadGraph, StoriesSince and a note are answered from them.
//
// The cache is for one run: it is never aged out, and a Gateway that is to
// live on (the follower, the postern server) must not be given it. A write
// through the Gateway ends what it kept (reads, in beads.go), so a command that
// reads, writes and reads again sees its own write; a write by another process
// is not seen. The bead list it keeps leaves out mail and event beads, so
// Snapshot holds none: they are never an epic, a story or a step, and a bead
// the snapshot does not hold is read through the gateway when asked for.
func WithRunCache() Option {
	return func(g *Gateway) { g.cache = &runCache{} }
}

// runCache is what WithRunCache keeps.
type runCache struct {
	beads memo[[]bead]
	// index is beads indexed for ShowBeads.
	index memo[*snapshot]
	notes memo[map[string]any]
}

// reset forgets everything kept.
func (c *runCache) reset() {
	c.beads.reset()
	c.index.reset()
	c.notes.reset()
}

// memo is one read made once: the first caller reads it and the ones that come
// while it does wait for that read. A read that failed is made again by the
// next caller.
type memo[T any] struct {
	mu   sync.Mutex
	done bool
	val  T
}

func (m *memo[T]) get(read func() (T, error)) (T, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done {
		return m.val, nil
	}
	val, err := read()
	if err != nil {
		var none T
		return none, err
	}
	m.val, m.done = val, true
	return val, nil
}

func (m *memo[T]) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	var none T
	m.val, m.done = none, false
}

// readsOnly says whether this bd command changes nothing, so that what the
// cache kept stays true after it.
func readsOnly(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "show", "list", "ready", "blocked", "state", "comments", "count", "stats":
		return true
	case "formula":
		return len(args) > 1 && args[1] == "list"
	case "kv":
		return len(args) > 1 && (args[1] == "get" || args[1] == "list")
	case "sql":
		return len(args) > 1 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(args[len(args)-1])), "select")
	}
	return false
}

// cachedLeaves are the types of bead the cache's list leaves out.
var cachedLeaves = []string{TypeMail, TypeEvent}

// listing reads the beads `bd list --all` prints, closed ones included, leaving
// out those of the types in leave. With a run cache it is the one list the
// cache keeps (less mail and event beads), narrowed here; the slice is the
// caller's to reorder.
func (g *Gateway) listing(ctx context.Context, leave ...string) ([]bead, error) {
	if g.cache == nil {
		return g.listFromBd(ctx, leave)
	}
	kept, err := g.cache.beads.get(func() ([]bead, error) {
		if all, ok := g.listAllOverConnection(ctx); ok {
			return withoutTypes(all, cachedLeaves), nil
		}
		return g.listFromBd(ctx, cachedLeaves)
	})
	if err != nil {
		return nil, err
	}
	return withoutTypes(kept, leave), nil
}

// listFromBd is one bd list of every bead but those of the types in leave.
func (g *Gateway) listFromBd(ctx context.Context, leave []string) ([]bead, error) {
	args := []string{"list", "--all", "--limit", "0"}
	if len(leave) > 0 {
		args = append(args, "--exclude-type", strings.Join(leave, ","))
	}
	out, err := g.call(ctx, append(args, "--json")...)
	if err != nil {
		return nil, err
	}
	all, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading every bead: %w", err)
	}
	return all, nil
}

// withoutTypes is a new slice of the beads that are none of the types.
func withoutTypes(all []bead, types []string) []bead {
	kept := make([]bead, 0, len(all))
	for _, b := range all {
		if !slices.Contains(types, b.Type) {
			kept = append(kept, b)
		}
	}
	return kept
}

// childrenInRun is the beads filed under parent, closed ones too when closed is
// set, read from the cache's list; ok is false when the Gateway keeps none or
// the list could not be read, and the caller asks bd.
func (g *Gateway) childrenInRun(ctx context.Context, parent string, closed bool) (children []bead, ok bool) {
	if g.cache == nil {
		return nil, false
	}
	all, err := g.listing(ctx)
	if err != nil {
		return nil, false
	}
	for _, b := range all {
		if b.Parent == parent && (closed || b.Status != StatusClosed) {
			children = append(children, b)
		}
	}
	return children, true
}

// noteTable is the table of every note, as `bd kv list --json` prints it: the
// cache's own when the Gateway keeps one.
func (g *Gateway) noteTable(ctx context.Context) (map[string]any, error) {
	if g.cache == nil {
		return g.readNoteTable(ctx)
	}
	return g.cache.notes.get(func() (map[string]any, error) { return g.readNoteTable(ctx) })
}

// noteInRun is the note key from the cache's table. answered is false when the
// Gateway keeps none, the table could not be read, or the key holds something
// that is not text: the caller asks bd. A key the table lacks is a note not
// there, which reads empty.
func (g *Gateway) noteInRun(ctx context.Context, key string) (value string, answered bool) {
	if g.cache == nil {
		return "", false
	}
	table, err := g.noteTable(ctx)
	if err != nil {
		return "", false
	}
	held, there := table[key]
	if !there {
		return "", true
	}
	if text, isText := held.(string); isText {
		return strings.TrimSpace(text), true
	}
	return "", false
}

// indexInRun is the cache's list indexed; ok is false when the Gateway keeps
// none or the list could not be read, and the caller asks bd.
func (g *Gateway) indexInRun(ctx context.Context) (*snapshot, bool) {
	if g.cache == nil {
		return nil, false
	}
	index, err := g.cache.index.get(func() (*snapshot, error) {
		all, err := g.listing(ctx)
		if err != nil {
			return nil, err
		}
		return newSnapshot(g, all), nil
	})
	return index, err == nil
}

// beadsInRun is the beads ids name, as the cache's list holds them; ok is false
// unless it holds every one of them.
func (g *Gateway) beadsInRun(ctx context.Context, ids []string) (held map[string]bead, ok bool) {
	index, ok := g.indexInRun(ctx)
	if !ok {
		return nil, false
	}
	held = make(map[string]bead, len(ids))
	for _, id := range ids {
		b, there := index.byID[id]
		if !there {
			return nil, false
		}
		held[id] = b
	}
	return held, true
}

// showBeadsInRun is ShowBeads read from the cache's list, in the order of ids,
// as a snapshot reads them; ok is false when the cache does not hold every one
// of ids, and the caller asks bd.
func (g *Gateway) showBeadsInRun(ctx context.Context, ids []string) (details []application.StoryDetail, ok bool) {
	if _, ok := g.beadsInRun(ctx, ids); !ok {
		return nil, false
	}
	index, _ := g.indexInRun(ctx)
	details, err := index.ShowBeads(ctx, ids)
	return details, err == nil
}
