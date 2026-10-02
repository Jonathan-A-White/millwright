package apptest

import (
	"context"
	"sync"

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
