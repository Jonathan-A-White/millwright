package beads

import (
	"context"
	"database/sql"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// closedStoriesQuery and closedEdgesQuery are `bd list --status closed
// --closed-after --exclude-type epic` as the served database holds it: the
// closed beads, less the epics, the mail and the event beads (none of them is a
// story that landed; the kept listing leaves mail and events out too), with the
// two edges a ClosedStory reads, the epic a bead hangs from and the bead it
// supersedes. A bd list of the fortnight's closed beads was 3 to 8 s on a loaded
// laptop, nearly all of it bd starting and printing 20,000 beads whole
// (mw-gq6.1286). The edges are read from the dependencies alone and readClosed
// keeps those of the closed stories it holds: a join with the closed condition
// took 0.9 to 1.0 s of the server at load 12 and over (mw-gq6.1445).
const (
	closedWhere = "i.status = 'closed' and i.issue_type not in ('epic', 'mail', 'event') and i.closed_at >= ?"

	closedStoriesQuery = "select i.id as id, i.title as title, i.issue_type as issue_type, i.closed_at as closed_at," +
		" i.close_reason as close_reason, i.metadata as metadata from issues i where " + closedWhere
	closedEdgesQuery = "select d.issue_id, coalesce(d.depends_on_issue_id, d.depends_on_wisp_id, d.depends_on_external), d.type" +
		" from dependencies d where d.type in ('parent-child', 'supersedes')"
)

// closedOverConnection is ClosedStories read over the kept connection in two
// queries; ok is false when there is no connection or it failed, and the caller
// starts a bd.
func (g *Gateway) closedOverConnection(ctx context.Context, since time.Time) (closed []application.ClosedStory, ok bool) {
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	closed, err := readClosed(ctx, db, since)
	if err != nil {
		g.keptFailed(ctx, "the closed stories", err)
		return nil, false
	}
	return closed, true
}

func readClosed(ctx context.Context, db *sql.DB, since time.Time) ([]application.ClosedStory, error) {
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()
	from := since.UTC().Format(time.DateTime)

	rows, err := db.QueryContext(bounded, closedStoriesQuery, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	closed := []application.ClosedStory{}
	at := map[string]int{}
	for rows.Next() {
		var story application.ClosedStory
		var closedAt, reason, meta sql.NullString
		if err := rows.Scan(&story.ID, &story.Title, &story.Type, &closedAt, &reason, &meta); err != nil {
			return nil, err
		}
		story.CloseReason = reason.String
		story.ClosedAt = bead{ClosedAt: rfc3339Of(closedAt.String)}.closedAt()
		path := bead{Metadata: metadataOf(meta)}.pathMetadata()
		story.Rig, story.Host = path["rig"], path["host"]
		at[story.ID] = len(closed)
		closed = append(closed, story)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	edges, err := db.QueryContext(bounded, closedEdgesQuery)
	if err != nil {
		return nil, err
	}
	defer edges.Close()
	for edges.Next() {
		var issue, kind string
		var target sql.NullString
		if err := edges.Scan(&issue, &target, &kind); err != nil {
			return nil, err
		}
		i, held := at[issue]
		if !held {
			continue
		}
		switch kind {
		case "parent-child":
			closed[i].EpicID = target.String
		case "supersedes":
			closed[i].Superseded = true
		}
	}
	return closed, edges.Err()
}
