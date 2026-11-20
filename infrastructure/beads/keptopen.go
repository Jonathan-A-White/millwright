package beads

import "context"

// The tidy's two reads, the open mail of every mailbox and the open beads that
// hold the formulas, over the kept connection (mw-gq6.1544): each was a bd of
// its own in every tick of the Millhand, the only two left.
const (
	// openMailWhere is `bd list --type mail --status open`.
	openMailWhere = "i.issue_type = 'mail' and i.status = 'open'"
	// openBeadsWhere is `bd list --exclude-type mail,event`: every bead not
	// closed, mail and events left out.
	openBeadsWhere = "i.status <> 'closed' and i.issue_type not in ('mail', 'event')"
)

// openMailOverConnection is the open mail of every mailbox as `bd list` prints
// it, read over the kept connection; ok is false when there is no connection or
// it failed, and the caller starts a bd.
func (g *Gateway) openMailOverConnection(ctx context.Context) (found []bead, ok bool) {
	return g.whereOverConnection(ctx, openMailWhere, "the open mail")
}

// openBeadsOverConnection is every bead not closed, less mail and events, as
// `bd list --brief` prints it (no description, no acceptance criteria); ok is
// false when there is no connection or it failed, and the caller starts a bd.
func (g *Gateway) openBeadsOverConnection(ctx context.Context) (found []bead, ok bool) {
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	found, err := readListing(ctx, db, listingQueries{
		beads: withoutTexts(beadColumns) + " where " + openBeadsWhere + " order by i.created_at, i.id",
		labels: "select l.issue_id, l.label from labels l join issues i on i.id = l.issue_id where " + openBeadsWhere +
			" order by l.issue_id, l.label",
		edgesOf: liveEdgesOf,
	}, nil)
	if err != nil {
		g.keptFailed(ctx, "the open beads", err)
		return nil, false
	}
	return found, true
}

// whereOverConnection is the beads a condition on the issues (i) holds, with
// their labels and edges, oldest first, read over the kept connection; ok is
// false when there is no connection or it failed, and the caller starts a bd.
func (g *Gateway) whereOverConnection(ctx context.Context, where, what string) (found []bead, ok bool) {
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	found, err := readListing(ctx, db, listingQueries{
		beads: beadColumns + " where " + where + " order by i.created_at, i.id",
		labels: "select l.issue_id, l.label from labels l join issues i on i.id = l.issue_id where " + where +
			" order by l.issue_id, l.label",
		edges: "select d.issue_id, coalesce(d.depends_on_issue_id, d.depends_on_wisp_id, d.depends_on_external) as depends_on_id, d.type" +
			" from dependencies d join issues i on i.id = d.issue_id where " + where + " order by d.issue_id, d.created_at",
	}, nil)
	if err != nil {
		g.keptFailed(ctx, what, err)
		return nil, false
	}
	return found, true
}

// stepsOverConnection is every bead, closed ones too, that hangs from a formula's
// root, as `bd list --parent <root> --all --brief` prints them; ok is false when
// there is no connection or it failed, and the caller starts a bd.
func (g *Gateway) stepsOverConnection(ctx context.Context, root string) (found []bead, ok bool) {
	where := "i.id in (select d.issue_id from dependencies d where d.type = 'parent-child' and d.depends_on_issue_id = " + sqlQuoted(root) + ")"
	return g.whereOverConnection(ctx, where, "the steps of a formula")
}
