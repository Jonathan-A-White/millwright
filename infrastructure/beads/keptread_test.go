package beads_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// keptServer is a stand-in for the beads server behind the kept connection: it
// answers each read the Gateway makes with the rows of a small vault, as the
// server's text protocol would (a value is a string, a number or nil), counts
// the connections opened to it, and fails every query when fail is set.
type keptServer struct {
	opens atomic.Int32
	fail  atomic.Bool
	// queued says that a story in hand carries the run:queued label.
	queued atomic.Bool
	// mailed says that the vault also holds a mail bead and an event bead, which
	// a query that leaves both types out does not return.
	mailed atomic.Bool

	mu   sync.Mutex
	seen []string
}

// asked is every query the server was sent, in order.
func (s *keptServer) asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

// listingQueries is the queries sent for the beads, the labels and the edges of
// a listing, in the order they were sent.
func (s *keptServer) listingQueries() (beads, labels, edges []string) {
	for _, q := range s.asked() {
		l := strings.ToLower(q)
		switch {
		case strings.Contains(l, "from issues i") && strings.Contains(l, "leases"):
			beads = append(beads, q)
		case strings.Contains(l, "from labels"):
			labels = append(labels, q)
		case strings.Contains(l, "from dependencies"):
			edges = append(edges, q)
		}
	}
	return beads, labels, edges
}

type keptConn struct{ s *keptServer }
type keptRows struct {
	cols []string
	rows [][]driver.Value
	next int
}

func (s *keptServer) open(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("kept-stand-in-%d", headDriverSeq.Add(1))
	sql.Register(name, s)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("opening the stand-in database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func (s *keptServer) Open(string) (driver.Conn, error) {
	s.opens.Add(1)
	return keptConn{s}, nil
}
func (c keptConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (c keptConn) Close() error                        { return nil }
func (c keptConn) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }
func (c keptConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.s.fail.Load() {
		return nil, errors.New("server went away")
	}
	c.s.mu.Lock()
	c.s.seen = append(c.s.seen, query)
	c.s.mu.Unlock()
	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "dolt_hashof_db"):
		return &keptRows{cols: []string{"head"}, rows: [][]driver.Value{{"hash-of-the-beads"}}}, nil
	case strings.Contains(q, "from issues i") && strings.Contains(q, "leases"):
		cols := []string{"id", "title", "description", "acceptance_criteria", "status", "issue_type", "assignee",
			"estimated_minutes", "created_at", "started_at", "updated_at", "closed_at", "priority", "metadata",
			"lease_expires_at", "comment_count"}
		rows := [][]driver.Value{
			{"t-1", "Epic", "", "", "open", "epic", nil, nil, "2026-10-01 10:00:00", nil, "2026-10-01 10:00:00", nil, int64(1), `{"rig":"millwright"}`, nil, int64(0)},
			{"t-1.1", "By hand", "do it", "1. it works", "in_progress", "task", "mw@laptop", int64(30), "2026-10-01 10:05:00", "2026-10-01 11:00:00", "2026-10-01 11:00:00", nil, int64(2), `{}`, "2026-10-01 11:05:00", int64(1)},
			{"t-0", "Long gone", "", "", "closed", "task", nil, nil, "2026-08-01 00:00:00", nil, "2026-08-01 00:00:00", "2026-08-01 00:00:00", int64(2), `{}`, nil, int64(3)},
		}
		if c.s.mailed.Load() {
			rows = append(rows,
				[]driver.Value{"m-1", "A mail", "long body", "", "open", "mail", "mw@laptop", nil, "2026-10-01 12:00:00", nil, "2026-10-01 12:00:00", nil, int64(2), `{}`, nil, int64(0)},
				[]driver.Value{"e-1", "An event", "long body", "", "closed", "event", nil, nil, "2026-10-01 12:01:00", nil, "2026-10-01 12:01:00", "2026-10-01 12:01:00", int64(4), `{}`, nil, int64(0)})
		}
		if leavesOut(q, "mail", "event") {
			kept := rows[:0:0]
			for _, row := range rows {
				if row[5] != "mail" && row[5] != "event" {
					kept = append(kept, row)
				}
			}
			rows = kept
		}
		return &keptRows{cols: cols, rows: rows}, nil
	case strings.Contains(q, "hitl:todo"):
		return &keptRows{cols: []string{"id", "metadata"}, rows: [][]driver.Value{
			{"td-1", `{"todo_due":"2026-10-14T08:00:00Z"}`},
			{"td-2", `{"todo_due":"2026-10-14T09:00:00Z","todo_reminded":"2026-10-14T09:00:00Z"}`},
			{"td-3", `{}`},                // no due time: not a card the reminder is after
			{"td-4", nil},                 // no metadata at all
			{"td-5", `{"todo_due":"  "}`}, // a blank due is none
		}}, nil
	case strings.Contains(q, "run:queued"):
		cols := []string{"id", "metadata", "parent_metadata", "status", "claimed", "blocked"}
		if c.s.queued.Load() {
			epic := `{"rig":"lampas","branch":"main","harness":"claude","model":"opus","effort":"high","host":"desktop"}`
			return &keptRows{cols: cols, rows: [][]driver.Value{
				{"lp-2", `{"queued_at":"2026-10-18T09:30:00Z"}`, epic, "open", int64(0), int64(1)},
				{"lp-1", `{"queued_at":"2026-10-18T09:00:00Z","size":"S"}`, epic, "in_progress", int64(1), int64(0)},
				{"lp-3", `{}`, nil, "open", int64(0), int64(0)}, // no epic, so no path: not a story a rig can land
				{"mw-1", `{"rig":"millwright"}`, epic, "open", int64(1), int64(1)},
			}}, nil
		}
		return &keptRows{cols: cols}, nil
	case strings.Contains(q, "from labels"):
		return &keptRows{cols: []string{"issue_id", "label"}, rows: [][]driver.Value{{"t-1.1", "hitl"}, {"t-1.1", "run:running"}}}, nil
	case strings.Contains(q, "from dependencies"):
		return &keptRows{cols: []string{"issue_id", "depends_on_id", "type"}, rows: [][]driver.Value{{"t-1.1", "t-1", "parent-child"}, {"t-1.1", "t-0", "blocks"}}}, nil
	case strings.Contains(q, "where `key` = 'kv.some.note'"):
		return &keptRows{cols: []string{"value"}, rows: [][]driver.Value{{"its value\n"}}}, nil
	case strings.Contains(q, "where `key` = 'kv."):
		return &keptRows{cols: []string{"value"}}, nil
	case strings.Contains(q, "`value` as v from config"):
		return &keptRows{cols: []string{"k", "v"}, rows: [][]driver.Value{{"some.note", "its value"}, {"other.note", "x"}, {"some.more", nil}}}, nil
	case strings.Contains(q, "from config"):
		return &keptRows{cols: []string{"k", "id", "a", "b", "t", "at"}, rows: [][]driver.Value{
			{"c", int64(9), "t-1.1", "mw@laptop", "RAN step x", "2026-10-01 11:30:00"},
			{"k", "", "some.note", "its value", "", nil},
		}}, nil
	case strings.Contains(q, "from events e"):
		return &keptRows{cols: []string{"k", "id", "what", "actor", "text", "at", "type", "status", "assignee", "labels"}, rows: [][]driver.Value{
			{"7", "t-1.1", "updated", "mw@laptop", nil, "2026-10-01 11:00:00", "task", "in_progress", "mw@laptop", "run:running"},
		}}, nil
	case strings.Contains(q, "from comments c join"):
		return &keptRows{cols: []string{"k", "id", "actor", "text", "at", "type", "status", "assignee", "labels"}, rows: nil}, nil
	}
	return nil, fmt.Errorf("unexpected query %q", query)
}

// leavesOut says whether the query's where leaves out the types, as `i.issue_type
// not in ('mail', 'event')` does.
func leavesOut(lowered string, types ...string) bool {
	where, found := strings.CutPrefix(lowered[strings.Index(lowered, " where ")+1:], "where ")
	if !found {
		return false
	}
	for _, t := range types {
		if !strings.Contains(where, "'"+t+"'") || !strings.Contains(where, "not in") {
			return false
		}
	}
	return true
}

func (r *keptRows) Columns() []string { return r.cols }
func (r *keptRows) Close() error      { return nil }
func (r *keptRows) Next(dest []driver.Value) error {
	if r.next >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}

// bdListing is what a stand-in bd answers a list with when the kept connection
// fails: the same vault as keptServer holds, as bd prints it.
const bdListing = `[
 {"id":"t-1","title":"Epic","status":"open","issue_type":"epic","priority":1,"comment_count":0},
 {"id":"t-1.1","title":"By hand","status":"in_progress","issue_type":"task","parent":"t-1","labels":["hitl","run:running"],"comment_count":1}
]`

const bdTable = `[{"k":"k","id":"","a":"some.note","b":"its value","t":"","at":null}]`

func bdsStarted(t *testing.T, asked string) int {
	t.Helper()
	data, err := os.ReadFile(asked)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("reading what the stand-in recorded: %v", err)
	}
	return strings.Count(string(data), "\n")
}

// The follower builds the Governor's view every ten seconds, and a view built
// through a snapshot read the beads with a bd list and a bd sql: about 40 new
// connections to the beads server a minute for the one build, and 400 to 1000
// with the follower's other reads (mw-gq6.600). With a connection kept open the
// snapshot is read over it, and no bd is started.
func TestSnapshotIsReadOverTheKeptConnectionWithoutStartingABd(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))

	tracker, err := g.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Fatalf("expected no bd started while the connection answers, got %d", got)
	}
	ids, err := tracker.LiveEpics(context.Background())
	if err != nil || len(ids) != 1 || ids[0] != "t-1" {
		t.Fatalf("expected the live epic t-1, got %v, %v", ids, err)
	}
	epics, err := tracker.ShowEpics(context.Background(), ids)
	if err != nil || len(epics) != 1 || len(epics[0].Stories) != 1 {
		t.Fatalf("expected the epic with its one story, got %+v, %v", epics, err)
	}
	story := epics[0].Stories[0]
	if story.Story.ID != "t-1.1" || !story.Hitl() || story.Assignee != "mw@laptop" || story.Description != "do it" || story.Acceptance != "1. it works" || story.EpicID != "t-1" {
		t.Fatalf("the story is not what the server held: %+v", story)
	}
	if !story.LeaseExpires.Equal(time.Date(2026, 10, 1, 11, 5, 0, 0, time.UTC)) || story.CommentCount != 1 {
		t.Fatalf("expected the lease the server held to be on the story: %+v", story)
	}
	if len(story.Needs) != 1 || story.Needs[0] != "t-0" {
		t.Fatalf("expected the blocks edge the server held, got needs %v", story.Needs)
	}
	notes, ok := tracker.(application.SnapshotNotes)
	if !ok {
		t.Fatal("expected a snapshot that holds the notes")
	}
	if all, held := notes.AllNotes(); !held || all["some.note"] != "its value" {
		t.Fatalf("expected the note the server held, got %v, %v", all, held)
	}
	said, err := tracker.StoriesComments(context.Background(), []string{"t-1.1"})
	if err != nil || len(said["t-1.1"]) != 1 || said["t-1.1"][0].Text != "RAN step x" {
		t.Fatalf("expected the comment the server held, got %v, %v", said, err)
	}
	if got := s.opens.Load(); got != 1 {
		t.Fatalf("expected the one connection, got %d", got)
	}
}

// mw-gq6.600: a view publish starts no bd while a kept connection is present.
func TestAViewPublishStartsNoBdWhileTheConnectionIsKept(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))
	view := application.PosternView{
		Tracker: g, Notes: g, Host: "laptop",
		Now: func() time.Time { return time.Date(2026, 10, 18, 7, 0, 0, 0, time.UTC) },
	}

	for i := 0; i < 3; i++ {
		doc, err := view.Build(context.Background())
		if err != nil {
			t.Fatalf("build %d: %v", i, err)
		}
		if len(doc.Beads) == 0 {
			t.Fatalf("build %d: expected the epic and its story in the view", i)
		}
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Fatalf("expected a view built with no bd started, got %d", got)
	}
}

// The follower's feed (the beads' changes since a time) is `bd sql` too, read
// once a pass: it goes over the connection as well.
func TestTheFeedIsReadOverTheKeptConnectionWithoutStartingABd(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))

	changes, err := g.BeadChanges(context.Background(), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if got := bdsStarted(t, asked); got != 0 {
		t.Fatalf("expected no bd started, got %d (the read said %v)", got, err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Bead.ID != "t-1.1" || changes[0].Bead.Run != "running" || changes[0].At.Hour() != 11 {
		t.Fatalf("expected the one change the server held, got %+v", changes)
	}
}

// A connection that fails is not a view that fails: bd reads the snapshot, and
// the follower's log says it did.
func TestSnapshotFallsBackToBdAndSaysSoWhenTheConnectionFails(t *testing.T) {
	s := &keptServer{}
	s.fail.Store(true)
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	var said strings.Builder
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)), beads.WithKeptSaid(&said))

	tracker, err := g.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("a failing connection must not fail the snapshot: %v", err)
	}
	if ids, _ := tracker.LiveEpics(context.Background()); len(ids) != 1 {
		t.Fatalf("expected the epic bd listed, got %v", ids)
	}
	if got := bdsStarted(t, asked); got != 2 {
		t.Fatalf("expected the list and the sql by bd, got %d bd", got)
	}
	if !strings.Contains(said.String(), "server went away") || !strings.Contains(said.String(), "bd") {
		t.Fatalf("expected the log to say the connection failed and bd read instead, got %q", said.String())
	}
}

// The follower's batch-land job reads the queue every pass: one query over the
// kept connection, no bd, and no listing of the work in hand (mw-gq6.600,
// mw-gq6.605). The rig of a story is its own, over its epic's default.
func TestQueuedStoriesAreReadOverTheKeptConnectionWithoutStartingABd(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))

	for i := 0; i < 20; i++ {
		queued, err := g.QueuedStories(context.Background())
		if err != nil || len(queued) != 0 {
			t.Fatalf("read %d: expected nothing queued, got %v, %v", i, queued, err)
		}
	}
	s.queued.Store(true)
	queued, err := g.QueuedStories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []application.QueuedStory{
		{ID: "lp-2", Rig: "lampas", QueuedAt: time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC), Status: "open", Blocked: true},
		{ID: "lp-1", Rig: "lampas", QueuedAt: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC), Status: "in_progress", Claimed: true},
		{ID: "mw-1", Rig: "millwright", Status: "open", Claimed: true, Blocked: true},
	}
	if !reflect.DeepEqual(queued, want) {
		t.Fatalf("expected the two lampas stories and the millwright one, got %+v", queued)
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Fatalf("expected no bd started, got %d", got)
	}
	if got := s.opens.Load(); got != 1 {
		t.Fatalf("expected the one connection, got %d", got)
	}
}

func TestQueuedStoriesErrWithoutAConnectionSoTheWorkIsListed(t *testing.T) {
	g := beads.New(t.TempDir())
	if _, err := g.QueuedStories(context.Background()); err == nil {
		t.Fatal("expected an error where there is no kept connection, so the caller lists the work")
	}
	s := &keptServer{}
	s.fail.Store(true)
	var said strings.Builder
	g = beads.New(t.TempDir(), beads.WithHeadDB(s.open(t)), beads.WithKeptSaid(&said))
	if _, err := g.QueuedStories(context.Background()); err == nil {
		t.Fatal("expected an error where the connection fails")
	}
	if !strings.Contains(said.String(), "the queued stories") || !strings.Contains(said.String(), "bd reads") {
		t.Fatalf("expected the follower's log to say the queue read failed and bd reads instead, got %q", said.String())
	}
}

// The follower's jobs read a few notes every pass, each by a bd once in a
// minute, and the view's notes by another: over the kept connection they are
// rows of the config table, and no bd is started.
func TestNotesAreReadOverTheKeptConnectionWithoutStartingABd(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))

	if got, err := g.Note(context.Background(), "some.note"); err != nil || got != "its value" {
		t.Fatalf("expected the note's value, got %q, %v", got, err)
	}
	if got, err := g.Note(context.Background(), "no.such.note"); err != nil || got != "" {
		t.Fatalf("expected a key that is not there to come back empty, got %q, %v", got, err)
	}
	all, err := g.NotesWithPrefix(context.Background(), "some.")
	if err != nil || len(all) != 1 || all["some.note"] != "its value" {
		t.Fatalf("expected the one note under the prefix (a null value is no note), got %v, %v", all, err)
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Fatalf("expected no bd started, got %d", got)
	}
}

// The follower looks for a To do card past due every minute: the cards with a
// due time are one query over the kept connection, no bd (mw-gq6.608).
func TestTodoDueCardsAreReadOverTheKeptConnectionWithoutStartingABd(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))

	for i := 0; i < 20; i++ {
		cards, err := g.TodoDueCards(context.Background())
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		want := []application.TodoDueCard{
			{ID: "td-1", Due: "2026-10-14T08:00:00Z"},
			{ID: "td-2", Due: "2026-10-14T09:00:00Z", Reminded: "2026-10-14T09:00:00Z"},
		}
		if !reflect.DeepEqual(cards, want) {
			t.Fatalf("read %d: expected the two cards with a due time, got %+v", i, cards)
		}
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Fatalf("expected no bd started, got %d", got)
	}
	if got := s.opens.Load(); got != 1 {
		t.Fatalf("expected the one connection, got %d", got)
	}
}

func TestTodoDueCardsErrWithoutAConnectionSoTheReadyCardsAreListed(t *testing.T) {
	g := beads.New(t.TempDir())
	if _, err := g.TodoDueCards(context.Background()); err == nil {
		t.Fatal("expected an error where there is no kept connection")
	}
	s := &keptServer{}
	s.fail.Store(true)
	var said strings.Builder
	g = beads.New(t.TempDir(), beads.WithHeadDB(s.open(t)), beads.WithKeptSaid(&said))
	if _, err := g.TodoDueCards(context.Background()); err == nil {
		t.Fatal("expected an error where the connection fails")
	}
	if !strings.Contains(said.String(), "the To do cards") || !strings.Contains(said.String(), "bd reads") {
		t.Fatalf("expected the log to say the read failed and bd reads instead, got %q", said.String())
	}
}

// mw-t5shinh.13: the listing read every bead, 14,323 mails and 10,295 events
// among them (38% of the rows), on every view the follower built. A listing
// read over the kept connection leaves both types out in each of its three
// queries, as the live work's reads do, and so holds no mail and no event.
func TestAListingOverTheKeptConnectionLeavesMailAndEventsOutOfItsThreeQueries(t *testing.T) {
	s := &keptServer{}
	s.mailed.Store(true)
	program, _ := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))

	if _, err := g.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	queries, labels, edges := s.listingQueries()
	for name, sent := range map[string][]string{"beads": queries, "labels": labels, "edges": edges} {
		if len(sent) != 1 {
			t.Fatalf("expected one %s query, got %d", name, len(sent))
		}
		lowered := strings.ToLower(sent[0])
		where := lowered[strings.Index(lowered, " where ")+1:]
		if !strings.Contains(where, "'mail'") || !strings.Contains(where, "'event'") {
			t.Errorf("expected the %s query to name mail and event in its where, got %q", name, sent[0])
		}
	}
}

// A snapshot holds no mail bead, and a view that asks for one by id is answered
// by the gateway.
func TestASnapshotAsksTheGatewayForAMailBeadItDoesNotHold(t *testing.T) {
	s := &keptServer{}
	s.mailed.Store(true)
	dir := t.TempDir()
	asked := filepath.Join(dir, "asked")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %[1]q
case "$*" in
*" show "*) echo '[{"id":"m-1","title":"A mail","description":"long body","status":"open","issue_type":"mail","assignee":"mw@laptop","priority":2}]' ;;
*) echo '[]' ;;
esac
`, asked)
	program := filepath.Join(dir, "bd-show")
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))
	tracker, err := g.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	details, err := tracker.ShowBeads(context.Background(), []string{"t-1.1", "m-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 2 || details[0].Story.ID != "t-1.1" || details[1].Story.ID != "m-1" || details[1].Description != "long body" {
		t.Fatalf("expected the story and the mail bead, in the order asked, got %+v", details)
	}
	if got := bdsStarted(t, asked); got != 1 {
		t.Errorf("expected the one bd show for the mail bead, got %d bd", got)
	}
}

// The Postern view built from a vault that holds mail and event beads is the
// view built from one that holds none: they were never in it.
func TestTheViewBuiltFromAVaultWithMailAndEventsIsTheViewWithoutThem(t *testing.T) {
	build := func(mailed bool) string {
		s := &keptServer{}
		s.mailed.Store(mailed)
		program, _ := standInSnapshotProgram(t, bdListing, bdTable)
		g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))
		view := application.PosternView{
			Tracker: g, Notes: g, Host: "laptop",
			Now: func() time.Time { return time.Date(2026, 10, 18, 7, 0, 0, 0, time.UTC) },
		}
		doc, err := view.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	with, without := build(true), build(false)
	if !strings.Contains(without, "t-1.1") {
		t.Fatalf("expected the story in the view, got %s", without)
	}
	if with != without {
		t.Errorf("expected the same view with mail and events in the vault:\n with:    %s\n without: %s", with, without)
	}
}

// Under a run cache the snapshot is the cache's one listing, not a second whole
// read beside it.
func TestUnderARunCacheSnapshotAndTheCacheShareOneListingRead(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)), beads.WithRunCache())

	if _, err := g.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := g.BeadGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := g.StoriesSince(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	queries, labels, edges := s.listingQueries()
	if len(queries) != 1 || len(labels) != 1 || len(edges) != 1 {
		t.Errorf("expected one read of the beads, labels and edges, got %d, %d and %d", len(queries), len(labels), len(edges))
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Errorf("expected no bd while the connection answers, got %d", got)
	}
}

// A kept read that fails mid-run leaves the rest of the run reading by bd, and
// the connection says so once.
func TestAKeptReadThatFailsMidRunLeavesTheRestOfTheRunToBd(t *testing.T) {
	s := &keptServer{}
	dir := t.TempDir()
	asked := filepath.Join(dir, "asked")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %[1]q
case "$*" in
*" kv "*) echo '{"some.note":"by bd"}' ;;
*" list "*) cat <<'LISTING'
%[2]s
LISTING
;;
*) echo '[]' ;;
esac
`, asked, bdListing)
	program := filepath.Join(dir, "bd-fallback")
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var said strings.Builder
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)),
		beads.WithKeptSaid(&said), beads.WithRunCache())
	ctx := context.Background()

	notes, err := g.NotesWithPrefix(ctx, "some.")
	if err != nil || notes["some.note"] != "its value" {
		t.Fatalf("expected the note over the connection, got %v, %v", notes, err)
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Fatalf("expected no bd while the connection answers, got %d", got)
	}
	s.fail.Store(true)
	if _, err := g.Snapshot(ctx); err != nil {
		t.Fatalf("a failing connection must not fail the read: %v", err)
	}
	notes, err = g.NotesWithPrefix(ctx, "some.")
	if err != nil || notes["some.note"] != "by bd" {
		t.Fatalf("expected the note by bd for the rest of the run, got %v, %v", notes, err)
	}
	if got := strings.Count(said.String(), "the kept beads connection failed"); got != 1 {
		t.Errorf("expected the one line saying so, got %d: %q", got, said.String())
	}
	if got := bdsStarted(t, asked); got != 3 {
		t.Errorf("expected the one bd list, the snapshot's one bd sql and the one bd kv list, got %d bd", got)
	}
	if _, err := g.StoriesSince(ctx, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if got := bdsStarted(t, asked); got != 3 {
		t.Errorf("expected the listing already read to be shared, got %d bd", got)
	}
}
