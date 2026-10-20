package beads

import (
	"context"

	"github.com/Jonathan-A-White/millwright/domain"
)

// BeadGraph implements application.BeadGraph: one bd list of every bead,
// closed ones included, whose dependencies bd prints as edges. Mail, event and
// molecule beads are left out: they are never an epic, a map or a ticket, and
// they are most of the database. Over a kept connection it is three narrow
// queries and no bd (mw-gq6.612).
func (g *Gateway) BeadGraph(ctx context.Context) ([]domain.GraphBead, error) {
	if graph, ok := g.graphOverConnection(ctx); ok {
		return graph, nil
	}
	beads, err := g.listing(ctx, TypeMail, TypeEvent, TypeMolecule)
	if err != nil {
		return nil, err
	}
	graph := make([]domain.GraphBead, 0, len(beads))
	for _, b := range beads {
		one := domain.GraphBead{ID: b.ID, Title: b.Title, Status: b.Status, Type: b.Type, Labels: b.Labels, Priority: b.priority(), Created: b.created()}
		for _, e := range b.edges() {
			if e.Issue == b.ID {
				one.Edges = append(one.Edges, domain.BeadEdge{To: e.DependsOn, Kind: e.Kind})
			}
		}
		graph = append(graph, one)
	}
	return graph, nil
}
