package beads

import (
	"context"
	"database/sql"
)

// closeReasonsQuery is the reason each closed bead was closed for, which the
// kept listing of every bead (beadColumns) leaves out and the triage read needs:
// a landing is told by the commit its close reason names. Read from the issues
// alone, with no join (mw-gq6.1445).
const closeReasonsQuery = "select i.id, i.close_reason from issues i where i.close_reason is not null and i.close_reason != ''"

// triageOverConnection is every bead `bd list --all --exclude-type mail,event
// --json` prints, read over the kept connection: the listing of every bead and
// the close reasons beside it. ok is false when there is no connection or it
// failed, and the caller starts a bd (mw-gq6.1546).
func (g *Gateway) triageOverConnection(ctx context.Context) (all []bead, ok bool) {
	all, ok = g.listAllOverConnection(ctx)
	if !ok {
		return nil, false
	}
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	reasons, err := readCloseReasons(ctx, db)
	if err != nil {
		g.keptFailed(ctx, "the close reasons", err)
		return nil, false
	}
	for i := range all {
		all[i].CloseReason = reasons[all[i].ID]
	}
	return all, true
}

func readCloseReasons(ctx context.Context, db *sql.DB) (map[string]string, error) {
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, closeReasonsQuery)
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
