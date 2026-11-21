package beads

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// showOverConnection is ShowBeads read by id over the kept connection, closed
// beads too (the sweep of the Millhand's tick asks after the story of every
// session on the host, and each tick started a bd show for them, mw-gq6.1545);
// ok is false when there is no connection or it failed, and the caller starts a
// bd. A bead the vault has no record of is left out, as bd show leaves it out.
// What bd show adds to a bead is read beside it: the reason it was closed for,
// its parent's Path as the defaults it inherits, and its waits narrowed to the
// beads not yet closed.
func (g *Gateway) showOverConnection(ctx context.Context, ids []string) (details []application.StoryDetail, ok bool) {
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	held := inList(ids)
	targets := map[string]string{}
	found, err := readListing(ctx, db, listingQueries{
		beads:      beadColumns + " where i.id in (" + held + ") order by i.created_at, i.id",
		labels:     "select l.issue_id, l.label from labels l where l.issue_id in (" + held + ") order by l.issue_id, l.label",
		edgesOf:    liveEdgesOf,
		commentsOf: commentCountsOf,
	}, targets)
	if err != nil {
		g.keptFailed(ctx, "the beads by id", err)
		return nil, false
	}
	byID := make(map[string]bead, len(found))
	var parents []string
	for _, b := range found {
		byID[b.ID] = b
		if b.Parent != "" && !slices.Contains(ids, b.Parent) && !slices.Contains(parents, b.Parent) {
			parents = append(parents, b.Parent)
		}
	}
	reasons, err := readCloseReasonsOf(ctx, db, held)
	if err != nil {
		g.keptFailed(ctx, "the close reasons by id", err)
		return nil, false
	}
	parentMetadata := map[string]map[string]any{}
	for id, b := range byID {
		parentMetadata[id] = b.Metadata
	}
	if len(parents) > 0 {
		more, err := readMetadataOf(ctx, db, inList(parents))
		if err != nil {
			g.keptFailed(ctx, "the parents of the beads by id", err)
			return nil, false
		}
		for id, m := range more {
			parentMetadata[id] = m
		}
	}

	details = make([]application.StoryDetail, 0, len(ids))
	for _, id := range ids {
		b, there := byID[id]
		if !there {
			continue
		}
		b.CloseReason = reasons[id]
		var defaults domain.Path
		if m, has := parentMetadata[b.Parent]; has && b.Parent != "" {
			defaults = domain.PathFromMetadata(bead{Metadata: m}.pathMetadata())
		}
		detail := b.detail(defaults)
		var open []string
		for _, need := range detail.Needs {
			if targets[need] != StatusClosed {
				open = append(open, need)
			}
		}
		detail.Needs = open
		details = append(details, detail)
	}
	return details, true
}

// inList is ids quoted for an `in (...)` of a query.
func inList(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = sqlQuoted(id)
	}
	return strings.Join(quoted, ", ")
}

// readCloseReasonsOf is the reason each of the beads in list (quoted ids) was
// closed for, read from the issues alone.
func readCloseReasonsOf(ctx context.Context, db *sql.DB, list string) (map[string]string, error) {
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, "select i.id, i.close_reason from issues i where i.id in ("+list+")"+
		" and i.close_reason is not null and i.close_reason != ''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reasons := map[string]string{}
	for rows.Next() {
		var id string
		var reason sql.NullString
		if err := rows.Scan(&id, &reason); err != nil {
			return nil, err
		}
		reasons[id] = reason.String
	}
	return reasons, rows.Err()
}

// readMetadataOf is the metadata of the beads in list (quoted ids), by id.
func readMetadataOf(ctx context.Context, db *sql.DB, list string) (map[string]map[string]any, error) {
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, "select i.id, i.metadata from issues i where i.id in ("+list+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	held := map[string]map[string]any{}
	for rows.Next() {
		var id string
		var meta sql.NullString
		if err := rows.Scan(&id, &meta); err != nil {
			return nil, err
		}
		var m map[string]any
		if meta.Valid && meta.String != "" {
			_ = json.Unmarshal([]byte(meta.String), &m)
		}
		held[id] = m
	}
	return held, rows.Err()
}
