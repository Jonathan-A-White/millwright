package beads

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw dispatch reads what is running and what is ready every minute, and mw
// doctor the work in hand and every bead, and each of those reads started a bd:
// after mw-gq6.608 the desktop's dispatch still started about 8 a minute and its
// doctor 4, every one several new connections to the beads server (mw-gq6.612).
// A Gateway given a database (WithHeadDB) reads them over the kept connection,
// from the live beads only, and starts a bd only when that connection fails.

// liveIDs are the ids of the beads the work is read from: every one that is not
// closed, mail and events left out (there are thousands of each, and none is
// work), and every epic, closed or not, for the defaults its stories inherit.
// "Not closed" is two ranges of the status on either side of 'closed', never
// `status <> 'closed'`: the server cannot use the status index for a not-equal
// joined with an or, and read every bead, 760 to 850 ms at load 12 to 13 for
// 1,160 beads, where the three reads of ranges and the join by id take a
// quarter to a half of that (mw-gq6.1446). A list of the statuses mw knows would be as
// quick, but would leave out one it does not. The three are disjoint (a
// closed epic is neither below nor above 'closed'), so no bead is read twice.
const liveIDs = "(select id from issues where status < 'closed' and issue_type not in ('mail', 'event')" +
	" union all select id from issues where status > 'closed' and issue_type not in ('mail', 'event')" +
	" union all select id from issues where status = 'closed' and issue_type = 'epic') live"

// liveBeadsQuery and liveLabelsQuery are the live beads with their labels, as
// beadsListQuery and beadLabelsQuery read every bead; liveEdgesOf reads their
// edges, and an edge carries the status of the bead it names, so that a wait on a
// bead closed long ago is known to be over without reading that bead.
var (
	liveBeadsQuery = strings.Replace(beadColumns, " from issues i left join leases",
		" from "+liveIDs+" join issues i on i.id = live.id left join leases", 1) + " order by i.created_at, i.id"
	liveLabelsQuery = "select l.issue_id, l.label from labels l join " + liveIDs + " on live.id = l.issue_id" +
		" order by l.issue_id, l.label"
)

// liveEdgesOf is the edges of the beads ids names: the server reads each
// bead's edges by the index on the bead's id, where the join with the live
// condition read every dependency there is, 1.0 to 1.4 s of the 90,000 at load
// 14 and 0.4 s this way (mw-gq6.1340).
func liveEdgesOf(ids []string) string {
	return "select d.issue_id, coalesce(d.depends_on_issue_id, d.depends_on_wisp_id, d.depends_on_external) as depends_on_id," +
		" d.type, t.status as target_status from dependencies d" +
		" left join issues t on t.id = d.depends_on_issue_id where " + idsIn("d.issue_id", ids) +
		" order by d.issue_id, d.created_at"
}

// withoutTexts is a listing's query with the description, the acceptance
// criteria of each bead left out (read as empty): they are most of its 2.9 MB,
// and with the count of comments (a listing that prints no texts asks for none,
// see readLiveOverConnection), they were over half of the server's time at load
// 14 (mw-gq6.1340).
func withoutTexts(query string) string {
	return strings.NewReplacer("i.description as description", "'' as description",
		"i.acceptance_criteria as acceptance_criteria", "'' as acceptance_criteria",
	).Replace(query)
}

// liveListing is the live beads as one read over the kept connection found
// them.
type liveListing struct {
	beads []bead
	byID  map[string]bead
	// targets is the status of every bead an edge names, "" for one that is no
	// bead (a wisp, an outside reference, or one not there).
	targets map[string]string
}

// liveOverConnection reads the live beads over the kept connection; ok is false
// when there is no connection or it failed, and the caller starts a bd. A
// Gateway given a run cache (WithRunCache) reads them once, until it writes:
// mw queue asks for what is ready, blocked and running, and each was a whole
// read of 2.9 MB (mw-gq6.1340).
func (g *Gateway) liveOverConnection(ctx context.Context) (*liveListing, bool) {
	if g.cache == nil {
		return g.readLiveOverConnection(ctx)
	}
	live, err := g.cache.live.get(func() (*liveListing, error) {
		l, ok := g.readLiveOverConnection(ctx)
		if !ok {
			return nil, errNoKeptConnection
		}
		return l, nil
	})
	return live, err == nil
}

// readLiveOverConnection is one whole read of the live beads over the kept
// connection.
func (g *Gateway) readLiveOverConnection(ctx context.Context) (*liveListing, bool) {
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	targets := map[string]string{}
	queries := listingQueries{beads: liveBeadsQuery, labels: liveLabelsQuery, edgesOf: liveEdgesOf, commentsOf: commentCountsOf}
	if g.withoutTexts {
		queries.beads, queries.commentsOf = withoutTexts(queries.beads), nil
	}
	all, err := readListing(ctx, db, queries, targets)
	if err != nil {
		g.keptFailed(ctx, "the live beads", err)
		return nil, false
	}
	l := &liveListing{beads: all, byID: make(map[string]bead, len(all)), targets: targets}
	for _, b := range all {
		l.byID[b.ID] = b
	}
	return l, true
}

// waiting says whether b has a blocking edge to a bead that is not closed, or
// to one that is no bead, as bd's own idea of blocked has it.
func (l *liveListing) waiting(b bead) bool {
	for _, e := range b.edges() {
		if e.Issue != b.ID || !slices.Contains(blockingKinds, e.Kind) {
			continue
		}
		if l.targets[e.DependsOn] != StatusClosed {
			return true
		}
	}
	return false
}

// blocked is waiting for b or any bead it hangs from: bd holds back the
// stories of an epic that waits.
func (l *liveListing) blocked(b bead) bool {
	seen := map[string]bool{}
	for {
		if l.waiting(b) {
			return true
		}
		parent, held := l.byID[b.Parent]
		if b.Parent == "" || !held || seen[b.Parent] {
			return false
		}
		seen[b.Parent] = true
		b = parent
	}
}

// shown is b as a story, with its epic's defaults and what it waits on
// narrowed to the beads not yet closed, as bd show prints it.
func (l *liveListing) shown(b bead) application.StoryDetail {
	var defaults domain.Path
	if parent, held := l.byID[b.Parent]; held {
		defaults = domain.PathFromMetadata(parent.pathMetadata())
	}
	detail := b.detail(defaults)
	var open []string
	for _, need := range detail.Needs {
		if l.targets[need] != StatusClosed {
			open = append(open, need)
		}
	}
	detail.Needs = open
	return detail
}

// ready is what `bd ready --unassigned --exclude-type epic` lists: the open
// beads nobody holds, no epic, no molecule, nothing blocked, the most urgent
// first and the oldest first among those.
func (l *liveListing) ready() []application.StoryDetail {
	return l.readyWhere(func(b bead) bool { return b.Assignee == "" })
}

// blockedForHost is what the gateway's `bd blocked` and bd show come to: the
// open, unclaimed beads that are no epic and no step of a poured formula,
// waiting on a bead not closed, on host, as snapshot.BlockedForHost reads them:
// the most urgent first, the newest first among those as urgent.
func (l *liveListing) blockedForHost(host string) []application.StoryDetail {
	var waiting []bead
	for _, b := range l.beads {
		if b.Type == TypeEpic || b.Status != StatusOpen || b.Assignee != "" || isPouredStep(b.ID) || !l.waiting(b) {
			continue
		}
		waiting = append(waiting, b)
	}
	sort.SliceStable(waiting, func(i, j int) bool {
		if waiting[i].priority() != waiting[j].priority() {
			return waiting[i].priority() < waiting[j].priority()
		}
		return waiting[i].created().After(waiting[j].created())
	})
	return onHostExactly(l.details(waiting), host)
}

// onHostExactly narrows details to the ones whose merged Path names host.
func onHostExactly(details []application.StoryDetail, host string) []application.StoryDetail {
	var on []application.StoryDetail
	for _, detail := range details {
		if detail.Merged().Host == host {
			on = append(on, detail)
		}
	}
	return on
}

// readyWithLabel is what `bd ready --label label --exclude-type epic` lists:
// ready does not ask for the unassigned, so a bead somebody holds that is still
// open is there too.
func (l *liveListing) readyWithLabel(label string) []application.StoryDetail {
	return l.readyWhere(func(b bead) bool { return slices.Contains(b.Labels, label) })
}

// readyWhere is the open beads that are no epic and no molecule, nothing
// blocked, that keep accepts, the most urgent first and the oldest first among
// those.
func (l *liveListing) readyWhere(keep func(bead) bool) []application.StoryDetail {
	var found []bead
	for _, b := range l.beads {
		if b.Status != StatusOpen || b.Type == TypeEpic || b.Type == "molecule" || !keep(b) || l.blocked(b) {
			continue
		}
		found = append(found, b)
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].priority() != found[j].priority() {
			return found[i].priority() < found[j].priority()
		}
		return found[i].created().Before(found[j].created())
	})
	details := l.details(found)
	children := l.childCounts()
	for i := range details {
		details[i].Children = children[details[i].Story.ID]
	}
	return details
}

// childCounts is how many beads that are not closed hang from each bead the
// live beads name as a parent.
func (l *liveListing) childCounts() map[string]int {
	counts := map[string]int{}
	for _, b := range l.beads {
		if b.Parent != "" && b.Status != StatusClosed {
			counts[b.Parent]++
		}
	}
	return counts
}

// withStatus is the beads with status that are no epic, in the order filed.
func (l *liveListing) withStatus(status string) []bead {
	var found []bead
	for _, b := range l.beads {
		if b.Status == status && b.Type != TypeEpic {
			found = append(found, b)
		}
	}
	return found
}

func (l *liveListing) details(beads []bead) []application.StoryDetail {
	out := make([]application.StoryDetail, 0, len(beads))
	for _, b := range beads {
		out = append(out, l.shown(b))
	}
	return out
}

// workInHand is Gateway.WorkInHand read from the listing.
func (l *liveListing) workInHand() application.WorkInHand {
	var stale []bead
	for _, b := range l.withStatus(StatusOpen) {
		if application.ClaimHost(b.Assignee) != "" {
			stale = append(stale, b)
		}
	}
	children := l.childCounts()
	counted := func(beads []bead) []application.StoryDetail {
		details := l.details(beads)
		for i := range details {
			details[i].Children = children[details[i].Story.ID]
		}
		return details
	}
	return application.WorkInHand{Ready: l.ready(), Running: counted(l.withStatus(StatusInProgress)), OpenClaimed: counted(stale), Held: counted(l.withStatus(StatusDeferred))}
}

// onHost narrows details as Gateway.onHost does.
func onHost(details []application.StoryDetail, host string, anyHost bool) []application.StoryDetail {
	var on []application.StoryDetail
	for _, detail := range details {
		merged := detail.Merged().Host
		if merged == "" || merged != host && !(anyHost && merged == domain.HostAuto) {
			continue
		}
		on = append(on, detail)
	}
	return on
}

// elsewhere narrows details to what the hosts other than host hold.
func elsewhere(details []application.StoryDetail, host string) []application.StoryDetail {
	var away []application.StoryDetail
	for _, detail := range details {
		if detail.ClaimedAwayFrom(host) != "" {
			away = append(away, detail)
		}
	}
	return away
}

// stateOverConnection is the value of the label id carries under dimension
// ("running" for run:running), as `bd state id dimension` prints it, read over
// the kept connection; ok is false when there is no connection or it failed, and
// the caller starts a bd. A bead with no such label reads as "" and ok.
func (g *Gateway) stateOverConnection(ctx context.Context, id, dimension string) (value string, ok bool) {
	db := g.keptDB()
	if db == nil {
		return "", false
	}
	prefix := dimension + ":"
	query := "select label from labels where issue_id = " + sqlQuoted(id) +
		" and label like " + sqlQuoted(strings.NewReplacer("%", `\%`, "_", `\_`).Replace(prefix)+"%") + " order by label"
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, query)
	if err != nil {
		g.keptFailed(ctx, "a story's state", err)
		return "", false
	}
	defer rows.Close()
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			g.keptFailed(ctx, "a story's state", err)
			return "", false
		}
		if value, found := strings.CutPrefix(label, prefix); found {
			return value, true
		}
	}
	if err := rows.Err(); err != nil {
		g.keptFailed(ctx, "a story's state", err)
		return "", false
	}
	return "", true
}

// statesOverConnection is stateOverConnection for several beads at once, one
// query for each commentsChunk of ids: the value each carries under dimension,
// "" for one with none. ok is false when there is no connection or it failed,
// and the caller starts a bd for each.
func (g *Gateway) statesOverConnection(ctx context.Context, ids []string, dimension string) (states map[string]string, ok bool) {
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	prefix := dimension + ":"
	like := sqlQuoted(strings.NewReplacer("%", `\%`, "_", `\_`).Replace(prefix) + "%")
	states = make(map[string]string, len(ids))
	for _, id := range ids {
		states[id] = ""
	}
	for chunk := range slices.Chunk(ids, commentsChunk) {
		quoted := make([]string, 0, len(chunk))
		for _, id := range chunk {
			quoted = append(quoted, sqlQuoted(id))
		}
		query := "select issue_id, label from labels where label like " + like +
			" and issue_id in (" + strings.Join(quoted, ", ") + ") order by issue_id, label"
		if err := readStates(ctx, db, query, prefix, states); err != nil {
			g.keptFailed(ctx, "the states of several stories", err)
			return nil, false
		}
	}
	return states, true
}

// readStates sets, in states, the first label of each bead query names that
// carries prefix, to the rest of the label.
func readStates(ctx context.Context, db *sql.DB, query, prefix string, states map[string]string) error {
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var id, label string
		if err := rows.Scan(&id, &label); err != nil {
			return err
		}
		if value, found := strings.CutPrefix(label, prefix); found && !seen[id] {
			seen[id] = true
			states[id] = value
		}
	}
	return rows.Err()
}

// moleculesOverConnection is OpenMolecule for several roots from one read of the
// live beads over the kept connection: a root that is open, with its steps not
// closed in the order they are worked. A root the live beads lack is closed, a
// wisp or not there, and is left out for the caller to ask OpenMolecule, which
// can tell them apart: a pass that took a molecule for missing would pour a
// second. ok is false when there is no connection or it failed, and the caller
// starts the bds.
func (g *Gateway) moleculesOverConnection(ctx context.Context, rootIDs []string) (open map[string]application.Molecule, ok bool) {
	live, ok := g.liveOverConnection(ctx)
	if !ok {
		return nil, false
	}
	steps := map[string][]bead{}
	for _, b := range live.beads {
		if b.Parent != "" {
			steps[b.Parent] = append(steps[b.Parent], b)
		}
	}
	open = make(map[string]application.Molecule, len(rootIDs))
	for _, rootID := range rootIDs {
		if root, held := live.byID[rootID]; held && root.Status != StatusClosed {
			open[rootID] = application.Molecule{RootID: rootID, Steps: inWorkedOrder(steps[rootID])}
		}
	}
	return open, true
}

// graphWhere is the beads BeadGraph reads, as its bd list leaves out mail,
// events and molecules. Its edges are read from the dependencies alone and
// readGraph keeps those of the beads it holds (mw-gq6.1445); the rows are the
// whole table, read once for the graph and the listing of every bead
// (wholeEdges, mw-gq6.1626).
const graphWhere = "i.issue_type not in ('mail', 'event', 'molecule')"

const (
	graphBeadsQuery  = "select i.id, i.title, i.status, i.issue_type, i.priority, i.created_at from issues i where " + graphWhere + " order by i.created_at, i.id"
	graphLabelsQuery = "select l.issue_id, l.label from labels l join issues i on i.id = l.issue_id where " + graphWhere +
		" order by l.issue_id, l.label"
)

// graphOverConnection is BeadGraph over the kept connection, in three narrow
// queries; ok is false when there is no connection or it failed.
func (g *Gateway) graphOverConnection(ctx context.Context) ([]domain.GraphBead, bool) {
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	graph, err := readGraph(ctx, db, g.wholeEdges)
	if err != nil {
		g.keptFailed(ctx, "the bead graph", err)
		return nil, false
	}
	return graph, true
}

func readGraph(ctx context.Context, db *sql.DB, edgeRows func(context.Context, *sql.DB) ([]edgeRow, error)) ([]domain.GraphBead, error) {
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, graphBeadsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	graph := []domain.GraphBead{}
	at := map[string]int{}
	for rows.Next() {
		var one domain.GraphBead
		var created sql.NullString
		var priority sql.NullInt64
		if err := rows.Scan(&one.ID, &one.Title, &one.Status, &one.Type, &priority, &created); err != nil {
			return nil, err
		}
		one.Priority = application.DefaultPriority
		if priority.Valid {
			one.Priority = int(priority.Int64)
		}
		one.Created = bead{CreatedAt: rfc3339Of(created.String)}.created()
		at[one.ID] = len(graph)
		graph = append(graph, one)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	labels, err := db.QueryContext(bounded, graphLabelsQuery)
	if err != nil {
		return nil, err
	}
	defer labels.Close()
	for labels.Next() {
		var id, label string
		if err := labels.Scan(&id, &label); err != nil {
			return nil, err
		}
		if i, held := at[id]; held {
			graph[i].Labels = append(graph[i].Labels, label)
		}
	}
	if err := labels.Err(); err != nil {
		return nil, err
	}
	labels.Close()

	edges, err := edgeRows(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, e := range edges {
		if i, held := at[e.issue]; held && e.to != "" {
			graph[i].Edges = append(graph[i].Edges, domain.BeadEdge{To: e.to, Kind: e.kind})
		}
	}
	return graph, nil
}
