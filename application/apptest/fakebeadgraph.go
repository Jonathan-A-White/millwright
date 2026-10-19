package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// FakeBeadGraph is an in-memory application.BeadGraph: the beads a test adds,
// in the order it adds them.
type FakeBeadGraph struct {
	mu    sync.Mutex
	beads []domain.GraphBead
	reads int
	// Err, when set, is what BeadGraph reports instead of the beads.
	Err error
}

// Add puts a bead in the graph, with the links it carries.
func (g *FakeBeadGraph) Add(id, title, status, kind string, labels []string, edges ...domain.BeadEdge) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.beads = append(g.beads, domain.GraphBead{ID: id, Title: title, Status: status, Type: kind, Labels: labels, Edges: edges})
}

// SetPriority gives a bead already in the graph a priority; a bead added
// without one has priority 0.
func (g *FakeBeadGraph) SetPriority(id string, priority int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := range g.beads {
		if g.beads[i].ID == id {
			g.beads[i].Priority = priority
		}
	}
}

// AddFiled puts a bead in the graph as Add does, filed at the time given.
func (g *FakeBeadGraph) AddFiled(id, title, status, kind string, labels []string, created time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.beads = append(g.beads, domain.GraphBead{ID: id, Title: title, Status: status, Type: kind, Labels: labels, Created: created})
}

// Reads is how many times the graph was read.
func (g *FakeBeadGraph) Reads() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reads
}

// BeadGraph implements application.BeadGraph.
func (g *FakeBeadGraph) BeadGraph(context.Context) ([]domain.GraphBead, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reads++
	if g.Err != nil {
		return nil, g.Err
	}
	return append([]domain.GraphBead(nil), g.beads...), nil
}
