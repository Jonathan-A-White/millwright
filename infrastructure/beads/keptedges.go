package beads

import (
	"context"
	"database/sql"
)

// edgeRow is one row of the dependencies as the whole-table read has it: the
// bead, what it depends on (empty when the edge names none) and the kind.
type edgeRow struct{ issue, to, kind string }

// wholeEdges is every row of the dependencies, 4.1 MB and 1 to 3.6 s of the
// server at load. The listing of every bead and the bead graph each keep the
// edges of the beads they hold and each used to read the table itself, twice in
// one mw doctor --dry-run (mw-gq6.1626). Under a run cache it is read once and
// both are answered from the rows; the slice is read, never changed, by them.
func (g *Gateway) wholeEdges(ctx context.Context, db *sql.DB) ([]edgeRow, error) {
	if g.cache == nil {
		return readWholeEdges(ctx, db)
	}
	return g.cache.edges.get(func() ([]edgeRow, error) { return readWholeEdges(ctx, db) })
}

func readWholeEdges(ctx context.Context, db *sql.DB) ([]edgeRow, error) {
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, beadEdgesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []edgeRow
	for rows.Next() {
		var e edgeRow
		var to sql.NullString
		if err := rows.Scan(&e.issue, &to, &e.kind); err != nil {
			return nil, err
		}
		e.to = to.String
		all = append(all, e)
	}
	return all, rows.Err()
}
