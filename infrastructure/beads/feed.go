package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Gateway feeds the follower.
var _ application.BeadFeed = (*Gateway)(nil)

// The follower's reads, each one `bd sql` round trip to the served database
// (about a tenth of a second on 2026-10-01, with 4,000 beads). bd's own event
// beads (type event, one per set-state) are left out: a run state's label on
// the bead it is about is what the follower reads. A bead's run state comes
// from its run:* label, joined in.
const (
	feedBead = "i.issue_type as type, i.status as status, i.assignee as assignee, group_concat(l.label) as labels"
	feedRun  = "left join labels l on l.issue_id = i.id and l.label like 'run:%'"

	statesQuery = "select i.id as id, " + feedBead + " from issues i " + feedRun +
		" where i.issue_type <> 'event' group by i.id, i.issue_type, i.status, i.assignee"
	newestQuery = "select max(t.at) as newest from (select max(created_at) as at from events" +
		" union all select max(created_at) as at from comments) t"
	// changesQuery and commentsQuery take the since time for {since}, as bd
	// stores it: UTC, to the second.
	changesQuery = "select e.id as k, e.issue_id as id, e.event_type as what, e.actor as actor, e.created_at as at, " + feedBead +
		" from events e join issues i on i.id = e.issue_id " + feedRun +
		" where e.created_at >= '{since}' and i.issue_type <> 'event'" +
		" group by e.id, e.issue_id, e.event_type, e.actor, e.created_at, i.issue_type, i.status, i.assignee order by e.created_at"
	commentsQuery = "select c.id as k, c.issue_id as id, c.author as actor, c.text as text, c.created_at as at, " + feedBead +
		" from comments c join issues i on i.id = c.issue_id " + feedRun +
		" where c.created_at >= '{since}' and i.issue_type <> 'event'" +
		" group by c.id, c.issue_id, c.author, c.text, c.created_at, i.issue_type, i.status, i.assignee order by c.created_at"
)

// feedRow is one row of any of the follower's reads.
type feedRow struct {
	Key      string  `json:"k"`
	ID       string  `json:"id"`
	What     string  `json:"what"`
	Actor    *string `json:"actor"`
	Text     *string `json:"text"`
	At       string  `json:"at"`
	Type     string  `json:"type"`
	Status   string  `json:"status"`
	Assignee *string `json:"assignee"`
	Labels   *string `json:"labels"`
	Newest   *string `json:"newest"`
}

func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r feedRow) bead() application.BeadNow {
	b := application.BeadNow{ID: r.ID, Type: r.Type, Status: r.Status, Assignee: orEmpty(r.Assignee)}
	for _, label := range strings.Split(orEmpty(r.Labels), ",") {
		if run, ok := strings.CutPrefix(label, application.RunState+":"); ok {
			b.Run = run
			break
		}
	}
	return b
}

// sqlTime reads a time as bd sql --json prints one: RFC 3339, or MySQL's
// "2006-01-02 15:04:05", taken as UTC.
func sqlTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.ParseInLocation(time.DateTime, s, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("bd sql gave the time %q", s)
	}
	return t, nil
}

func (g *Gateway) feedRows(ctx context.Context, query string) ([]feedRow, error) {
	out, err := g.call(ctx, "sql", "--json", query)
	if err != nil {
		return nil, err
	}
	var rows []feedRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parsing bd sql --json: %w", err)
	}
	return rows, nil
}

// BeadStates implements application.BeadFeed: two reads, every bead (bd's
// event beads aside) and the newest change of bd's audit and its comments.
func (g *Gateway) BeadStates(ctx context.Context) ([]application.BeadNow, time.Time, error) {
	rows, err := g.feedRows(ctx, statesQuery)
	if err != nil {
		return nil, time.Time{}, err
	}
	beads := make([]application.BeadNow, 0, len(rows))
	for _, r := range rows {
		beads = append(beads, r.bead())
	}
	newest, err := g.feedRows(ctx, newestQuery)
	if err != nil {
		return nil, time.Time{}, err
	}
	var at time.Time
	if len(newest) == 1 && orEmpty(newest[0].Newest) != "" {
		if at, err = sqlTime(*newest[0].Newest); err != nil {
			return nil, time.Time{}, err
		}
	}
	return beads, at, nil
}

// BeadChanges implements application.BeadFeed: two reads, bd's own audit of
// the beads (its events table) and the comments, each since the time given,
// merged oldest first.
func (g *Gateway) BeadChanges(ctx context.Context, since time.Time) ([]application.BeadChange, error) {
	at := since.UTC().Format(time.DateTime)
	var changes []application.BeadChange
	for _, query := range []string{changesQuery, commentsQuery} {
		rows, err := g.feedRows(ctx, strings.ReplaceAll(query, "{since}", at))
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			when, err := sqlTime(r.At)
			if err != nil {
				return nil, err
			}
			c := application.BeadChange{Key: r.Key, At: when, Actor: orEmpty(r.Actor), What: r.What, Bead: r.bead()}
			if r.Text != nil {
				c.What, c.Comment = application.ChangeComment, *r.Text
			}
			changes = append(changes, c)
		}
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].At.Before(changes[j].At) })
	return changes, nil
}
