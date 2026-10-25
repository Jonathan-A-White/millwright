package beads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// The follower builds the Governor's view every ten seconds and reads the
// beads' feed on every head, and each of those reads started a bd: a view was
// two, and every bd costs the server several new connections (mw-gq6.600, 400
// to 1000 a minute). A Gateway given a database (WithHeadDB) reads them over
// the connection it already keeps for the head, and starts a bd only when that
// connection fails.

// keptDB is the kept connection, or nil when there is none or it failed lately
// and is being left alone.
func (g *Gateway) keptDB() *sql.DB {
	if g.headDB == nil {
		return nil
	}
	g.headMu.Lock()
	defer g.headMu.Unlock()
	if g.clock().Before(g.headSkipUntil) {
		return nil
	}
	return g.headDB
}

// keptFailed leaves the kept connection alone for HeadConnRetryWait, unless
// the caller's own context ended it, and says in the Gateway's log (WithKeptSaid)
// that bd reads in the meantime.
func (g *Gateway) keptFailed(ctx context.Context, what string, err error) {
	if ctx.Err() != nil {
		return
	}
	g.headMu.Lock()
	first := !g.clock().Before(g.headSkipUntil)
	g.headSkipUntil = g.clock().Add(HeadConnRetryWait)
	g.headMu.Unlock()
	if first && g.keptSaid != nil {
		fmt.Fprintf(g.keptSaid, "mw: the kept beads connection failed reading %s (%v); bd reads for the next %s\n", what, err, HeadConnRetryWait)
	}
}

// sqlRows is `bd sql --json query` as the JSON array of rows bd prints, read
// over the kept connection when there is one that answers, and by bd when there
// is not. The rows read the same either way: a number is a number, a time is
// RFC 3339, anything else a string, and a null is null.
func (g *Gateway) sqlRows(ctx context.Context, query string) ([]byte, error) {
	if db := g.keptDB(); db != nil {
		out, err := keptRowsJSON(ctx, db, query)
		if err == nil {
			return out, nil
		}
		g.keptFailed(ctx, "sql", err)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return g.call(ctx, "sql", "--json", query)
}

// keptRowsJSON runs query on db and prints its rows as bd sql --json does.
func keptRowsJSON(ctx context.Context, db *sql.DB, query string) ([]byte, error) {
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		raw := make([]sql.RawBytes, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c.Name()] = jsonOfColumn(c.DatabaseTypeName(), raw[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// keptQueryBound is how long one read over the kept connection may take: a
// listing of every bead is a large read.
const keptQueryBound = 60 * time.Second

// jsonOfColumn is a column's value as bd sql --json prints it. raw is nil for a
// null; the text protocol sends every value as text, so the column's type says
// which are numbers.
func jsonOfColumn(typ string, raw sql.RawBytes) any {
	if raw == nil {
		return nil
	}
	text := string(raw)
	switch strings.TrimPrefix(strings.ToUpper(typ), "UNSIGNED ") {
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT":
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return n
		}
	case "DATETIME", "TIMESTAMP":
		return rfc3339Of(text)
	}
	return text
}

// rfc3339Of is a datetime as the server prints it ("2006-01-02 15:04:05", UTC)
// in the form bd prints it; text it cannot read is left as it is.
func rfc3339Of(text string) string {
	t, err := time.ParseInLocation(time.DateTime, text, time.UTC)
	if err != nil {
		return text
	}
	return t.Format(time.RFC3339)
}

// beadsListQuery, beadLabelsQuery and beadEdgesQuery are `bd list --all` as the
// served database holds it: every bead with its lease and a count of its
// comments, every label, every dependency (an edge to a bead, a wisp or an
// external reference, whichever it names).
const (
	beadsListQuery  = beadColumns + " order by i.created_at, i.id"
	beadLabelsQuery = "select issue_id, label from labels order by issue_id, label"
	beadEdgesQuery  = "select issue_id, coalesce(depends_on_issue_id, depends_on_wisp_id, depends_on_external) as depends_on_id, type from dependencies" +
		" order by issue_id, created_at"
)

// beadColumns is a bead as a listing reads it, its lease and the count of its
// comments beside it, for a where and an order to follow.
const beadColumns = "select i.id as id, i.title as title, i.description as description, i.acceptance_criteria as acceptance_criteria," +
	" i.status as status, i.issue_type as issue_type, i.assignee as assignee, i.estimated_minutes as estimated_minutes," +
	" i.created_at as created_at, i.started_at as started_at, i.updated_at as updated_at, i.closed_at as closed_at," +
	" i.priority as priority, i.metadata as metadata, le.lease_expires_at as lease_expires_at, coalesce(cc.n, 0) as comment_count" +
	" from issues i left join leases le on le.issue_id = i.id" +
	" left join (select issue_id, count(*) as n from comments group by issue_id) cc on cc.issue_id = i.id"

// listingQueries are the three reads of a listing: its beads, their labels and
// their edges.
type listingQueries struct{ beads, labels, edges string }

// listAllOverConnection is the beads `bd list --all --json` prints, read over
// the kept connection in three queries; ok is false when there is no
// connection or it failed, and the caller starts a bd.
func (g *Gateway) listAllOverConnection(ctx context.Context) (all []bead, ok bool) {
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	all, err := readListing(ctx, db, listingQueries{beadsListQuery, beadLabelsQuery, beadEdgesQuery}, nil)
	if err != nil {
		g.keptFailed(ctx, "every bead", err)
		return nil, false
	}
	return all, true
}

// readListing reads the beads of a listing from db, with their labels and
// dependencies. An edges query with a fourth column, the status of the bead an
// edge names, leaves it in targets by that bead's id.
func readListing(ctx context.Context, db *sql.DB, q listingQueries, targets map[string]string) ([]bead, error) {
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()

	rows, err := db.QueryContext(bounded, q.beads)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []bead
	at := map[string]int{}
	for rows.Next() {
		var (
			b                                      bead
			assignee, started, closed, lease, meta sql.NullString
			created, updated                       sql.NullString
			minutes                                sql.NullInt64
			priority                               sql.NullInt64
		)
		if err := rows.Scan(&b.ID, &b.Title, &b.Description, &b.Acceptance, &b.Status, &b.Type, &assignee, &minutes,
			&created, &started, &updated, &closed, &priority, &meta, &lease, &b.CommentCount); err != nil {
			return nil, err
		}
		b.Assignee = assignee.String
		b.EstimatedMinutes = int(minutes.Int64)
		b.CreatedAt, b.UpdatedAt = rfc3339Of(created.String), rfc3339Of(updated.String)
		if started.Valid {
			b.StartedAt = rfc3339Of(started.String)
		}
		if closed.Valid {
			b.ClosedAt = rfc3339Of(closed.String)
		}
		if lease.Valid {
			b.LeaseExpiresAt = rfc3339Of(lease.String)
		}
		if priority.Valid {
			p := int(priority.Int64)
			b.Priority = &p
		}
		if meta.Valid && meta.String != "" {
			var m map[string]any
			if json.Unmarshal([]byte(meta.String), &m) == nil && len(m) > 0 {
				b.Metadata = m
			}
		}
		at[b.ID] = len(all)
		all = append(all, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	labels, err := db.QueryContext(bounded, q.labels)
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
			all[i].Labels = append(all[i].Labels, label)
		}
	}
	if err := labels.Err(); err != nil {
		return nil, err
	}

	edges, err := db.QueryContext(bounded, q.edges)
	if err != nil {
		return nil, err
	}
	defer edges.Close()
	cols, err := edges.Columns()
	if err != nil {
		return nil, err
	}
	for edges.Next() {
		var e edge
		var target, status sql.NullString
		into := []any{&e.Issue, &target, &e.Kind}
		if len(cols) > 3 {
			into = append(into, &status)
		}
		if err := edges.Scan(into...); err != nil {
			return nil, err
		}
		e.DependsOn = target.String
		if targets != nil && e.DependsOn != "" {
			targets[e.DependsOn] = status.String
		}
		i, held := at[e.Issue]
		if !held {
			continue
		}
		printed, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		all[i].Dependencies = append(all[i].Dependencies, printed)
		if e.Kind == "parent-child" {
			all[i].Parent = e.DependsOn
		}
	}
	return all, edges.Err()
}

// queuedStoriesQuery is a row for each story in hand that carries the
// run:queued label: its id, its own metadata, and the metadata of the epic it
// hangs from, which hold the Path the batch is landed by. An epic is not a
// story a batch lands.
const queuedStoriesQuery = "select i.id as id, i.metadata as metadata, p.metadata as parent_metadata" +
	" from issues i join labels l on l.issue_id = i.id" +
	" left join dependencies d on d.issue_id = i.id and d.type = 'parent-child'" +
	" left join issues p on p.id = d.depends_on_issue_id" +
	" where l.label = 'run:queued' and i.status = 'in_progress' and i.issue_type <> 'epic'" +
	" order by i.id"

// QueuedStories implements application.QueuedStoryReader over the kept
// connection: one query in place of the listings of the work in hand. With no
// connection, or when it fails (said in the follower's log), it errs and the
// caller lists the work.
func (g *Gateway) QueuedStories(ctx context.Context) ([]application.QueuedStory, error) {
	db := g.keptDB()
	if db == nil {
		return nil, errNoKeptConnection
	}
	stories, err := readQueuedStories(ctx, db)
	if err != nil {
		g.keptFailed(ctx, "the queued stories", err)
		return nil, err
	}
	return stories, nil
}

// readQueuedStories reads the queued stories from db. A story whose Path is not
// a whole one is left out, as a listing of the work in hand leaves it out.
func readQueuedStories(ctx context.Context, db *sql.DB) ([]application.QueuedStory, error) {
	bounded, cancel := context.WithTimeout(ctx, headQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, queuedStoriesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stories := []application.QueuedStory{}
	for rows.Next() {
		var id string
		var own, parent sql.NullString
		if err := rows.Scan(&id, &own, &parent); err != nil {
			return nil, err
		}
		story := bead{ID: id, Metadata: metadataOf(own)}
		path, err := domain.Story{Overrides: domain.PathFromMetadata(story.pathMetadata())}.
			PathFrom(domain.PathFromMetadata(bead{Metadata: metadataOf(parent)}.pathMetadata()))
		if err != nil {
			continue
		}
		stories = append(stories, application.QueuedStory{ID: id, Rig: path.Rig, QueuedAt: story.queuedAt()})
	}
	return stories, rows.Err()
}

// metadataOf is a bead's metadata column read as bd prints it: an object, or
// nothing when the column is null or holds anything else.
func metadataOf(column sql.NullString) map[string]any {
	var metadata map[string]any
	if column.Valid && json.Unmarshal([]byte(column.String), &metadata) != nil {
		return nil
	}
	return metadata
}

var errNoKeptConnection = errors.New("there is no kept connection to the beads server")

var _ application.QueuedStoryReader = (*Gateway)(nil)

// todoDueCardsQuery is a row for each open To do card that is not an epic: its
// id and its own metadata, which holds the due time and the one it was last
// reminded for.
const todoDueCardsQuery = "select i.id as id, i.metadata as metadata" +
	" from issues i join labels l on l.issue_id = i.id" +
	" where l.label = " + "'" + application.LabelHitlTodo + "'" + " and i.status = 'open' and i.issue_type <> 'epic'" +
	" order by i.id"

// TodoDueCards implements application.TodoDueReader over the kept connection:
// the open To do cards that carry a due time, in one query, in place of the
// listing of every ready card. With no connection, or when it fails (said in
// the follower's log), it errs and the caller lists the ready cards.
func (g *Gateway) TodoDueCards(ctx context.Context) ([]application.TodoDueCard, error) {
	db := g.keptDB()
	if db == nil {
		return nil, errNoKeptConnection
	}
	cards, err := readTodoDueCards(ctx, db)
	if err != nil {
		g.keptFailed(ctx, "the To do cards", err)
		return nil, err
	}
	return cards, nil
}

// readTodoDueCards reads the To do cards with a due time from db.
func readTodoDueCards(ctx context.Context, db *sql.DB) ([]application.TodoDueCard, error) {
	bounded, cancel := context.WithTimeout(ctx, headQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, todoDueCardsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []application.TodoDueCard{}
	for rows.Next() {
		var id string
		var own sql.NullString
		if err := rows.Scan(&id, &own); err != nil {
			return nil, err
		}
		card := bead{ID: id, Metadata: metadataOf(own)}
		due := strings.TrimSpace(card.text(application.TodoDueField))
		if due == "" {
			continue
		}
		cards = append(cards, application.TodoDueCard{ID: id, Due: due, Reminded: strings.TrimSpace(card.text(application.TodoRemindedField))})
	}
	return cards, rows.Err()
}

var _ application.TodoDueReader = (*Gateway)(nil)

// noteOverConnection is `bd kv get key` over the kept connection: the note's
// value, "" for a key that is not there; ok is false when there is no
// connection or it failed, and the caller starts a bd.
func (g *Gateway) noteOverConnection(ctx context.Context, key string) (value string, ok bool) {
	db := g.keptDB()
	if db == nil {
		return "", false
	}
	bounded, cancel := context.WithTimeout(ctx, headQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, "select `value` from config where `key` = "+sqlQuoted("kv."+key))
	if err != nil {
		g.keptFailed(ctx, "a note", err)
		return "", false
	}
	defer rows.Close()
	var found sql.NullString
	if rows.Next() {
		if err := rows.Scan(&found); err != nil {
			g.keptFailed(ctx, "a note", err)
			return "", false
		}
	}
	if err := rows.Err(); err != nil {
		g.keptFailed(ctx, "a note", err)
		return "", false
	}
	return strings.TrimSpace(found.String), true
}

// notesOverConnection is `bd kv list` over the kept connection, narrowed to the
// keys with prefix; ok is false when there is no connection or it failed.
func (g *Gateway) notesOverConnection(ctx context.Context, prefix string) (map[string]string, bool) {
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	bounded, cancel := context.WithTimeout(ctx, keptQueryBound)
	defer cancel()
	rows, err := db.QueryContext(bounded, "select substr(`key`, 4) as k, `value` as v from config where `key` like 'kv.%'")
	if err != nil {
		g.keptFailed(ctx, "the notes", err)
		return nil, false
	}
	defer rows.Close()
	found := map[string]string{}
	for rows.Next() {
		var key string
		var value sql.NullString
		if err := rows.Scan(&key, &value); err != nil {
			g.keptFailed(ctx, "the notes", err)
			return nil, false
		}
		if strings.HasPrefix(key, prefix) && value.Valid {
			found[key] = value.String
		}
	}
	if err := rows.Err(); err != nil {
		g.keptFailed(ctx, "the notes", err)
		return nil, false
	}
	return found, true
}

// inboxWhere is the open mail assigned to a mailbox, as `bd list --type mail
// --assignee <box> --status open` lists it.
func inboxWhere(mailbox string) string {
	return "i.issue_type = 'mail' and i.assignee = " + sqlQuoted(mailbox) + " and i.status = 'open'"
}

// inboxOverConnection is the open mail of a mailbox as `bd list` prints it, read
// over the kept connection; ok is false when there is no connection or it
// failed, and the caller starts a bd.
func (g *Gateway) inboxOverConnection(ctx context.Context, mailbox string) (found []bead, ok bool) {
	db := g.keptDB()
	if db == nil {
		return nil, false
	}
	where := inboxWhere(mailbox)
	found, err := readListing(ctx, db, listingQueries{
		beads: beadColumns + " where " + where + " order by i.created_at, i.id",
		labels: "select l.issue_id, l.label from labels l join issues i on i.id = l.issue_id where " + where +
			" order by l.issue_id, l.label",
		edges: "select d.issue_id, coalesce(d.depends_on_issue_id, d.depends_on_wisp_id, d.depends_on_external) as depends_on_id, d.type" +
			" from dependencies d join issues i on i.id = d.issue_id where " + where + " order by d.issue_id, d.created_at",
	}, nil)
	if err != nil {
		g.keptFailed(ctx, "a mailbox", err)
		return nil, false
	}
	return found, true
}
