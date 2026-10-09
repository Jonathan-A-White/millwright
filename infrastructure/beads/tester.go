package beads

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// Gateway satisfies the port.
var _ application.TesterStories = (*Gateway)(nil)

// StoriesSince implements application.TesterStories: one bd list of every
// bead, closed ones included, read as stories with their parent's defaults
// overlaid; epics, mail, events and molecules are left out, and so is a story
// whose times bd gives are all before since.
func (g *Gateway) StoriesSince(ctx context.Context, since time.Time) ([]application.StoryDetail, error) {
	out, err := g.call(ctx, "list", "--all", "--limit", "0", "--exclude-type", "mail,event,molecule", "--json")
	if err != nil {
		return nil, err
	}
	all, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading every bead: %w", err)
	}
	byID := make(map[string]bead, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	var touched []application.StoryDetail
	for _, b := range inFiledOrder(all) {
		if b.Type == TypeEpic {
			continue
		}
		var defaults domain.Path
		if parent, held := byID[b.Parent]; held {
			defaults = domain.PathFromMetadata(parent.pathMetadata())
		}
		d := b.detail(defaults)
		known, recent := false, false
		for _, at := range []time.Time{d.Created, d.Updated, d.ClosedAt} {
			known = known || !at.IsZero()
			recent = recent || (!at.IsZero() && !at.Before(since))
		}
		if !known || recent {
			touched = append(touched, d)
		}
	}
	return touched, nil
}
