package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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
//
// A second call, `bd sql`, reads the notes and the comments of the beads a view
// can ask about together, because each bd process pays its start-up (0.1 to
// 0.5 s on the live vault, 2026-10-01) and `bd sql` is the cheapest of them: so
// a build is two bd calls, not four (mw-jrx0s.19). The snapshot answers
// AllNotes and StoriesComments from it.
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
	// The listing is enough to build a view should this read fail, so the
	// snapshot goes without notes and comments and the view asks for them
	// itself.
	notes, comments, held, err := g.readNotesAndComments(ctx, all, time.Now())
	if err == nil {
		s.notes, s.comments, s.commentsHeld = notes, comments, held
	}
	return s, nil
}

// commentWindow is how long after a bead closed the snapshot still reads its
// comments: the view's verify window, which is the only reading of a closed
// bead's comments, and an hour over for the clocks.
const commentWindow = application.PosternViewVerifyWindow + time.Hour

// notesAndCommentsQuery reads every note (the kv rows of bd's config table, key
// less its "kv." prefix, which is `bd kv list`) and the comments of {ids}, in
// the order bd show prints a bead's comments, as one table: kind k for a note
// (a its key, b its value) and c for a comment (a its bead, b its author, t its
// text).
const notesAndCommentsQuery = "select 'c' as k, c.id as id, c.issue_id as a, c.author as b, c.text as t, c.created_at as at" +
	" from comments c where c.issue_id in ({ids})" +
	" union all select 'k', '', substr(`key`, 4), `value`, '', null from config where `key` like 'kv.%'" +
	" order by at, id"

type noteRow struct {
	Kind string  `json:"k"`
	A    string  `json:"a"`
	B    string  `json:"b"`
	Text string  `json:"t"`
	At   *string `json:"at"`
}

// readNotesAndComments is the snapshot's second bd call: every note, and the
// comments of every bead in all that is not closed or closed within
// commentWindow of now (held names them, the ones with no comment as well).
func (g *Gateway) readNotesAndComments(ctx context.Context, all []bead, now time.Time) (notes map[string]string, comments map[string][]application.Comment, held map[string]bool, err error) {
	held = map[string]bool{}
	var quoted []string
	for _, b := range all {
		if b.Status == StatusClosed && now.Sub(b.closedAt()) > commentWindow {
			continue
		}
		held[b.ID] = true
		if b.CommentCount > 0 {
			quoted = append(quoted, "'"+strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(b.ID)+"'")
		}
	}
	if len(quoted) == 0 {
		quoted = append(quoted, "''")
	}
	out, err := g.call(ctx, "sql", "--json", strings.ReplaceAll(notesAndCommentsQuery, "{ids}", strings.Join(quoted, ", ")))
	if err != nil {
		return nil, nil, nil, err
	}
	var rows []noteRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, nil, nil, fmt.Errorf("parsing bd sql --json: %w", err)
	}
	notes = map[string]string{}
	comments = map[string][]application.Comment{}
	for _, r := range rows {
		if r.Kind == "k" {
			notes[r.A] = r.B
			continue
		}
		var created time.Time
		if r.At != nil {
			if created, err = sqlTime(*r.At); err != nil {
				return nil, nil, nil, err
			}
		}
		comments[r.A] = append(comments[r.A], application.Comment{Author: r.B, Created: created, Text: r.Text})
	}
	for _, said := range comments {
		sort.SliceStable(said, func(i, j int) bool { return said[i].Created.Before(said[j].Created) })
	}
	return notes, comments, held, nil
}

// snapshot is every bead as one listing printed them.
type snapshot struct {
	application.WorkTracker
	beads    []bead
	byID     map[string]bead
	children map[string][]bead

	// notes, comments and commentsHeld are what the second call read: nil when
	// it failed. commentsHeld says whose comments comments holds.
	notes        map[string]string
	comments     map[string][]application.Comment
	commentsHeld map[string]bool
}

// AllNotes is every note, read with the snapshot; ok is false when the snapshot
// holds none, and the caller reads them itself.
func (s *snapshot) AllNotes() (notes map[string]string, ok bool) {
	return s.notes, s.notes != nil
}

// StoriesComments is Gateway.StoriesComments read from the snapshot for the
// beads whose comments it holds, and from the gateway for the rest.
func (s *snapshot) StoriesComments(ctx context.Context, ids []string) (map[string][]application.Comment, error) {
	out := make(map[string][]application.Comment, len(ids))
	var elsewhere []string
	for _, id := range ids {
		// A bead the listing counts comments for and the table gave none (one
		// kept elsewhere than the comments table) is read through bd show.
		if s.commentsHeld[id] && (len(s.comments[id]) > 0 || s.byID[id].CommentCount == 0) {
			out[id] = s.comments[id]
			if out[id] == nil {
				out[id] = []application.Comment{}
			}
		} else {
			elsewhere = append(elsewhere, id)
		}
	}
	if len(elsewhere) > 0 {
		read, err := s.WorkTracker.StoriesComments(ctx, elsewhere)
		if err != nil {
			return nil, err
		}
		for id, said := range read {
			out[id] = said
		}
	}
	return out, nil
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
