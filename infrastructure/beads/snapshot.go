package beads

import (
	"context"
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// Gateway satisfies the port.
var _ application.TrackerSnapshot = (*Gateway)(nil)

// Snapshot implements application.TrackerSnapshot: one bd list reads every bead,
// closed ones included, and the snapshot answers LiveEpics, ShowEpics and
// ShowBeads from it, where the gateway's own reads start a bd process for every
// epic's children (mw-jrx0s.17). bd prints a listing's dependencies as edges
// and `bd list --parent` prints each bead exactly as the listing does, so what
// a snapshot reports is what the gateway's own reads report. Whatever the
// listing does not hold, and every other read, goes to the gateway.
func (g *Gateway) Snapshot(ctx context.Context) (application.WorkTracker, error) {
	out, err := g.call(ctx, "list", "--all", "--limit", "0", "--json")
	if err != nil {
		return nil, err
	}
	all, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading every bead: %w", err)
	}
	s := &snapshot{
		WorkTracker: g, beads: all,
		byID: make(map[string]bead, len(all)), children: map[string][]bead{},
	}
	for _, b := range all {
		s.byID[b.ID] = b
		if b.Parent != "" {
			s.children[b.Parent] = append(s.children[b.Parent], b)
		}
	}
	return s, nil
}

// snapshot is every bead as one listing printed them.
type snapshot struct {
	application.WorkTracker
	beads    []bead
	byID     map[string]bead
	children map[string][]bead
}

// LiveEpics is Gateway.LiveEpics read from the listing.
func (s *snapshot) LiveEpics(context.Context) ([]string, error) {
	var live []bead
	for _, b := range s.beads {
		if b.Type == TypeEpic && (b.Status == StatusOpen || b.Status == StatusInProgress) {
			live = append(live, b)
		}
	}
	live = inFiledOrder(live)
	ids := make([]string, 0, len(live))
	for _, b := range live {
		ids = append(ids, b.ID)
	}
	return ids, nil
}

// ShowEpics is Gateway.ShowEpics read from the listing; an epic the listing
// does not hold sends the whole read to the gateway, to report it as it does.
func (s *snapshot) ShowEpics(ctx context.Context, ids []string) ([]application.EpicDetail, error) {
	out := make([]application.EpicDetail, 0, len(ids))
	for _, id := range ids {
		epic, ok := s.byID[id]
		if !ok {
			return s.WorkTracker.ShowEpics(ctx, ids)
		}
		if epic.Type != "" && epic.Type != TypeEpic {
			return nil, fmt.Errorf("%s is a %s, not an epic", id, epic.Type)
		}
		defaults := domain.PathFromMetadata(epic.pathMetadata())

		var filed []application.StoryDetail
		for _, story := range inSiblingOrder(append([]bead(nil), s.children[id]...)) {
			filed = append(filed, story.detail(defaults))
		}
		out = append(out, application.EpicDetail{
			ID: epic.ID, Title: epic.Title, Status: epic.Status, Priority: epic.priority(), Defaults: defaults,
			Bead: s.shown(epic, domain.Path{}), Stories: filed,
		})
	}
	return out, nil
}

// ShowBeads is Gateway.ShowBeads read from the listing, with the beads it does
// not hold read through the gateway.
func (s *snapshot) ShowBeads(ctx context.Context, ids []string) ([]application.StoryDetail, error) {
	var missing []string
	for _, id := range ids {
		if _, ok := s.byID[id]; !ok {
			missing = append(missing, id)
		}
	}
	elsewhere := map[string]application.StoryDetail{}
	if len(missing) > 0 {
		read, err := s.WorkTracker.ShowBeads(ctx, missing)
		if err != nil {
			return nil, err
		}
		for _, detail := range read {
			elsewhere[detail.Story.ID] = detail
		}
	}
	out := make([]application.StoryDetail, 0, len(ids))
	for _, id := range ids {
		if b, ok := s.byID[id]; ok {
			var defaults domain.Path
			if parent, held := s.byID[b.Parent]; held {
				defaults = domain.PathFromMetadata(parent.pathMetadata())
			}
			out = append(out, s.shown(b, defaults))
		} else if detail, ok := elsewhere[id]; ok {
			out = append(out, detail)
		}
	}
	return out, nil
}

// shown is the bead as bd show prints it: what it waits on narrowed to the
// beads not yet closed, which a listing's edges cannot say but the listing
// itself can.
func (s *snapshot) shown(b bead, defaults domain.Path) application.StoryDetail {
	detail := b.detail(defaults)
	var open []string
	for _, need := range detail.Needs {
		if blocker, held := s.byID[need]; held && blocker.Status == StatusClosed {
			continue
		}
		open = append(open, need)
	}
	detail.Needs = open
	return detail
}
