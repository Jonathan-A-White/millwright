package beads_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
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
	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "dolt_hashof_db"):
		return &keptRows{cols: []string{"head"}, rows: [][]driver.Value{{"hash-of-the-beads"}}}, nil
	case strings.Contains(q, "from issues i") && strings.Contains(q, "leases"):
		cols := []string{"id", "title", "description", "acceptance_criteria", "status", "issue_type", "assignee",
			"estimated_minutes", "created_at", "started_at", "updated_at", "closed_at", "priority", "metadata",
			"lease_expires_at", "comment_count"}
		return &keptRows{cols: cols, rows: [][]driver.Value{
			{"t-1", "Epic", "", "", "open", "epic", nil, nil, "2026-10-01 10:00:00", nil, "2026-10-01 10:00:00", nil, int64(1), `{"rig":"millwright"}`, nil, int64(0)},
			{"t-1.1", "By hand", "do it", "1. it works", "in_progress", "task", "mw@laptop", int64(30), "2026-10-01 10:05:00", "2026-10-01 11:00:00", "2026-10-01 11:00:00", nil, int64(2), `{}`, "2026-10-01 11:05:00", int64(1)},
			{"t-0", "Long gone", "", "", "closed", "task", nil, nil, "2026-08-01 00:00:00", nil, "2026-08-01 00:00:00", "2026-08-01 00:00:00", int64(2), `{}`, nil, int64(3)},
		}}, nil
	case strings.Contains(q, "run:queued"):
		if c.s.queued.Load() {
			return &keptRows{cols: []string{"queued"}, rows: [][]driver.Value{{int64(1)}}}, nil
		}
		return &keptRows{cols: []string{"queued"}}, nil
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

// The follower's batch-land job asks every pass whether a story is queued: one
// query over the kept connection, no bd, and no listing of the work in hand.
func TestAnyQueuedForBatchIsAskedOverTheKeptConnectionWithoutStartingABd(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))

	for i := 0; i < 20; i++ {
		queued, err := g.AnyQueuedForBatch(context.Background())
		if err != nil || queued {
			t.Fatalf("ask %d: expected nothing queued, got %v, %v", i, queued, err)
		}
	}
	s.queued.Store(true)
	if queued, err := g.AnyQueuedForBatch(context.Background()); err != nil || !queued {
		t.Fatalf("expected a queued story found, got %v, %v", queued, err)
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Fatalf("expected no bd started, got %d", got)
	}
	if got := s.opens.Load(); got != 1 {
		t.Fatalf("expected the one connection, got %d", got)
	}
}

func TestAnyQueuedForBatchErrsWithoutAConnectionSoTheWorkIsListed(t *testing.T) {
	g := beads.New(t.TempDir())
	if _, err := g.AnyQueuedForBatch(context.Background()); err == nil {
		t.Fatal("expected an error where there is no kept connection, so the caller lists the work")
	}
	s := &keptServer{}
	s.fail.Store(true)
	g = beads.New(t.TempDir(), beads.WithHeadDB(s.open(t)))
	if _, err := g.AnyQueuedForBatch(context.Background()); err == nil {
		t.Fatal("expected an error where the connection fails")
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
