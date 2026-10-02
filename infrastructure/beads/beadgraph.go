package beads

import (
	"context"
	"fmt"

	"github.com/Jonathan-A-White/millwright/domain"
)

// BeadGraph implements application.BeadGraph: one bd list of every bead,
// closed ones included, whose dependencies bd prints as edges. Mail, event and
// molecule beads are left out: they are never an epic, a map or a ticket, and
// they are most of the database.
func (g *Gateway) BeadGraph(ctx context.Context) ([]domain.GraphBead, error) {
	out, err := g.call(ctx, "list", "--all", "--limit", "0", "--exclude-type", "mail,event,molecule", "--json")
	if err != nil {
		return nil, err
	}
	beads, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading every bead: %w", err)
	}
	graph := make([]domain.GraphBead, 0, len(beads))
	for _, b := range beads {
		one := domain.GraphBead{ID: b.ID, Title: b.Title, Status: b.Status, Type: b.Type, Labels: b.Labels}
		for _, e := range b.edges() {
			if e.Issue == b.ID {
				one.Edges = append(one.Edges, domain.BeadEdge{To: e.DependsOn, Kind: e.Kind})
			}
		}
		graph = append(graph, one)
	}
	return graph, nil
}
