package beads

import (
	"context"
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// Gateway satisfies the port.
var _ application.TriageSource = (*Gateway)(nil)

// TriageBeads implements application.TriageSource: one bd list of every bead,
// closed ones included, with mail and event beads left out. Formula beads stay
// in, because the orphan sweep needs them. A bead's rig is its own, or its
// parent epic's when it names none.
func (g *Gateway) TriageBeads(ctx context.Context) ([]application.TriageBead, error) {
	out, err := g.call(ctx, "list", "--all", "--limit", "0", "--exclude-type", "mail,event", "--json")
	if err != nil {
		return nil, err
	}
	all, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading every bead: %w", err)
	}
	rigOf := make(map[string]string, len(all))
	for _, b := range all {
		rigOf[b.ID] = domain.PathFromMetadata(b.pathMetadata()).Rig
	}
	beads := make([]application.TriageBead, 0, len(all))
	for _, b := range all {
		rig := rigOf[b.ID]
		if rig == "" {
			rig = rigOf[b.Parent]
		}
		beads = append(beads, application.TriageBead{
			ID: b.ID, Title: b.Title, Type: b.Type, Status: b.Status, Priority: b.priority(),
			Labels: append([]string(nil), b.Labels...), Rig: rig,
			Size: b.text(application.TriageSizeField), Parent: b.Parent,
			Description: b.Description,
			Created:     b.created(), Updated: b.updated(), ClosedAt: b.closedAt(),
			Needs:    b.needs(),
			Molecule: b.pathMetadata()[application.MoleculeField],
		})
	}
	return beads, nil
}
