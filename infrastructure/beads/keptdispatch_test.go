package beads_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// factoryServer is a stand-in for the beads server a whole command reads over
// its kept connection: a small vault of beads, labels, edges, comments and
// notes, answered as the server's text protocol would. The notes are the ones
// it starts with as a stand-in bd's kv set and kv clear have since left them, so
// that a second pass reads what the first wrote. A query it does not know fails,
// which sets the kept connection aside and sends every read to bd, where it is
// counted.
type factoryServer struct {
	asked string // the stand-in bd's record of its calls

	beads    [][]driver.Value // the 16 columns of the listing
	labels   [][2]string
	edges    [][3]string
	comments map[string][][3]string // bead -> author, text, created_at
	notes    map[string]string

	mu      sync.Mutex
	unknown []string
}

type factoryConn struct{ s *factoryServer }

func (s *factoryServer) open(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("factory-stand-in-%d", headDriverSeq.Add(1))
	sql.Register(name, s)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("opening the stand-in database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func (s *factoryServer) Open(string) (driver.Conn, error) { return factoryConn{s}, nil }
func (c factoryConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (c factoryConn) Close() error                        { return nil }
func (c factoryConn) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }

// inboxAsked is the mailbox a listing of the open mail asks for.
var inboxAsked = regexp.MustCompile(`i\.issue_type = 'mail' and i\.assignee = '([^']*)'`)

var keyAsked = regexp.MustCompile("where `key` = '([^']*)'")

func (c factoryConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	s, q := c.s, strings.ToLower(query)
	switch {
	case strings.Contains(q, "from issues i left join leases"):
		rows := [][]driver.Value{}
		for _, b := range s.beads {
			if s.listed(q, b[0].(string)) {
				rows = append(rows, b)
			}
		}
		return &keptRows{cols: []string{"id", "title", "description", "acceptance_criteria", "status", "issue_type", "assignee",
			"estimated_minutes", "created_at", "started_at", "updated_at", "closed_at", "priority", "metadata",
			"lease_expires_at", "comment_count"}, rows: rows}, nil
	case strings.HasPrefix(q, "select i.id, i.title, i.status, i.issue_type, i.priority, i.created_at from issues i"):
		rows := [][]driver.Value{}
		for _, b := range s.beads {
			rows = append(rows, []driver.Value{b[0], b[1], b[4], b[5], b[12], b[8]})
		}
		return &keptRows{cols: []string{"id", "title", "status", "issue_type", "priority", "created_at"}, rows: rows}, nil
	case strings.Contains(q, "from labels where issue_id = '"):
		// One bead's labels under a dimension, as a story state asks.
		asked := regexp.MustCompile(`issue_id = '([^']*)' and label like '([^']*)%'`).FindStringSubmatch(q)
		rows := [][]driver.Value{}
		for _, l := range s.labels {
			if asked != nil && l[0] == asked[1] && strings.HasPrefix(strings.ToLower(l[1]), asked[2]) {
				rows = append(rows, []driver.Value{l[1]})
			}
		}
		return &keptRows{cols: []string{"label"}, rows: rows}, nil
	case strings.Contains(q, "from labels where label like '"):
		// The labels of a dimension on several beads, as a batch of states asks.
		asked := regexp.MustCompile(`label like '([^']*)%' and issue_id in \(([^)]*)\)`).FindStringSubmatch(q)
		rows := [][]driver.Value{}
		for _, l := range s.labels {
			if asked != nil && strings.HasPrefix(strings.ToLower(l[1]), asked[1]) && strings.Contains(asked[2], "'"+strings.ToLower(l[0])+"'") {
				rows = append(rows, []driver.Value{l[0], l[1]})
			}
		}
		return &keptRows{cols: []string{"issue_id", "label"}, rows: rows}, nil
	case strings.Contains(q, "label from labels"):
		rows := [][]driver.Value{}
		for _, l := range s.labels {
			if s.listed(q, l[0]) {
				rows = append(rows, []driver.Value{l[0], l[1]})
			}
		}
		return &keptRows{cols: []string{"issue_id", "label"}, rows: rows}, nil
	case strings.Contains(q, "as depends_on_id"):
		cols := []string{"issue_id", "depends_on_id", "type"}
		withStatus := strings.Contains(q, "target_status")
		if withStatus {
			cols = append(cols, "target_status")
		}
		rows := [][]driver.Value{}
		for _, e := range s.edges {
			if !s.listed(q, e[0]) {
				continue
			}
			row := []driver.Value{e[0], e[1], e[2]}
			if withStatus {
				row = append(row, s.status(e[1]))
			}
			rows = append(rows, row)
		}
		return &keptRows{cols: cols, rows: rows}, nil
	case strings.Contains(q, "union all select 'k'"):
		// The snapshot's notes and the comments of the beads it names.
		rows := s.commentRows("c")
		for _, key := range s.sortedNotes() {
			rows = append(rows, []driver.Value{"k", "", key, s.currentNotes()[key], "", nil})
		}
		return &keptRows{cols: []string{"k", "id", "a", "b", "t", "at"}, rows: rows}, nil
	case strings.Contains(q, "from wisp_comments"):
		rows := [][]driver.Value{}
		for _, b := range s.beads {
			rows = append(rows, []driver.Value{"b", b[0], b[0], "", "", nil})
		}
		rows = append(rows, s.commentRows("c")...)
		return &keptRows{cols: []string{"k", "id", "a", "b", "t", "at"}, rows: rows}, nil
	case strings.Contains(q, "show global status like 'connections'"):
		return &keptRows{cols: []string{"Variable_name", "Value"}, rows: [][]driver.Value{{"Connections", "4001637"}}}, nil
	case keyAsked.MatchString(query):
		key := strings.TrimPrefix(keyAsked.FindStringSubmatch(query)[1], "kv.")
		value, held := s.currentNotes()[key]
		if !held {
			return &keptRows{cols: []string{"value"}}, nil
		}
		return &keptRows{cols: []string{"value"}, rows: [][]driver.Value{{value}}}, nil
	case strings.Contains(q, "`value` as v from config"):
		rows := [][]driver.Value{}
		notes := s.currentNotes()
		for _, key := range s.sortedNotes() {
			rows = append(rows, []driver.Value{key, notes[key]})
		}
		return &keptRows{cols: []string{"k", "v"}, rows: rows}, nil
	}
	s.mu.Lock()
	s.unknown = append(s.unknown, query)
	s.mu.Unlock()
	return nil, fmt.Errorf("the stand-in server does not know the query %q", query)
}

// listed says whether a listing query holds the bead id: a read of the live
// beads leaves out a closed one that is no epic.
func (s *factoryServer) listed(q, id string) bool {
	if box := inboxAsked.FindStringSubmatch(q); box != nil {
		for _, b := range s.beads {
			if b[0] == id {
				return b[5] == "mail" && b[6] == box[1] && b[4] == "open"
			}
		}
		return false
	}
	if !strings.Contains(q, "i.status <> 'closed'") {
		return true
	}
	for _, b := range s.beads {
		if b[0] == id {
			return b[4] != "closed" || b[5] == "epic"
		}
	}
	return false
}

// status is the status of the bead id, nil for one the vault does not hold.
func (s *factoryServer) status(id string) driver.Value {
	for _, b := range s.beads {
		if b[0] == id {
			return b[4]
		}
	}
	return nil
}

func (s *factoryServer) commentRows(kind string) [][]driver.Value {
	var rows [][]driver.Value
	n := 0
	for _, id := range sortedKeys(s.comments) {
		for _, said := range s.comments[id] {
			n++
			rows = append(rows, []driver.Value{kind, fmt.Sprintf("c%d", n), id, said[0], said[1], said[2]})
		}
	}
	return rows
}

// currentNotes is the notes the server started with, as every kv set and kv
// clear the stand-in bd was asked for since has left them.
func (s *factoryServer) currentNotes() map[string]string {
	notes := map[string]string{}
	for k, v := range s.notes {
		notes[k] = v
	}
	data, _ := os.ReadFile(s.asked)
	for _, line := range strings.Split(string(data), "\n") {
		if _, rest, found := strings.Cut(line, " kv set "); found {
			key, value, _ := strings.Cut(rest, " ")
			notes[key] = value
		} else if _, rest, found := strings.Cut(line, " kv clear "); found {
			delete(notes, strings.TrimSpace(rest))
		}
	}
	return notes
}

func (s *factoryServer) sortedNotes() []string { return sortedKeys(s.currentNotes()) }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// recordingBd is a bd that records every call it is asked for, one line each,
// and answers every one as done: bd's batch mode asked for, an empty listing
// for one asked for JSON, nothing for the rest.
func recordingBd(t *testing.T) (program, asked string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	asked = filepath.Join(dir, "asked")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %[1]q
case "$*" in
*"config get dolt.auto-commit"*) echo batch ;;
*--json*) echo '[]' ;;
esac
exit 0
`, asked)
	program = filepath.Join(dir, "bd")
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return program, asked
}

// bdCallsSince is every bd call the stand-in recorded after the first from.
func bdCallsSince(t *testing.T, asked string, from int) []string {
	t.Helper()
	data, err := os.ReadFile(asked)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	if from > len(lines) {
		return nil
	}
	return lines[from:]
}

// aQuietFactory is a vault of one epic on the desktop: one story running there
// (its pane alive), one ready there that the cap holds back (it waited on a bead
// now closed, and carries a comment), one ready for the laptop, one waiting on
// a story the laptop is running.
func aQuietFactory(asked string) *factoryServer {
	epic := `{"rig":"millwright","branch":"main","harness":"claude","model":"opus","effort":"high","host":"desktop"}`
	at := "2026-10-18 09:00:00"
	bead := func(id, status, typ, assignee, metadata string, comments int64) []driver.Value {
		var who driver.Value
		if assignee != "" {
			who = assignee
		}
		var closed driver.Value
		if status == "closed" {
			closed = at
		}
		return []driver.Value{id, "The bead " + id, "", "", status, typ, who, nil, at, nil, at, closed, int64(2), metadata, nil, comments}
	}
	return &factoryServer{
		asked: asked,
		beads: [][]driver.Value{
			bead("mw-e", "open", "epic", "", epic, 0),
			bead("mw-e.1", "in_progress", "task", "mw@desktop", `{"host":"desktop"}`, 0),
			bead("mw-e.2", "open", "task", "", `{}`, 1),
			bead("mw-e.3", "open", "task", "", `{"host":"laptop"}`, 0),
			bead("mw-e.4", "open", "task", "", `{}`, 0),
			bead("mw-e.5", "in_progress", "task", "mw@laptop", `{"host":"laptop"}`, 0),
			bead("mw-e.9", "closed", "task", "", `{}`, 0),
		},
		labels: [][2]string{{"mw-e.1", "run:running"}, {"mw-e.5", "run:running"}},
		edges: [][3]string{
			{"mw-e.1", "mw-e", "parent-child"}, {"mw-e.2", "mw-e", "parent-child"}, {"mw-e.3", "mw-e", "parent-child"},
			{"mw-e.4", "mw-e", "parent-child"}, {"mw-e.5", "mw-e", "parent-child"}, {"mw-e.9", "mw-e", "parent-child"},
			{"mw-e.2", "mw-e.9", "blocks"}, {"mw-e.4", "mw-e.5", "blocks"},
		},
		comments: map[string][][3]string{"mw-e.2": {{"mayor@laptop", "a word from the Mayor", at}}},
		notes:    map[string]string{},
	}
}

// mw-gq6.612: after mw-gq6.608 the desktop's mw dispatch still started about 8
// bd a minute (the Mayor's attribution), every one several new connections to
// the beads server: what is running, what is ready, what the candidates wait on
// and what was said on them, and a note read or written for each gate, every
// minute, with nothing to start. With the connection kept open, a pass with
// nothing to start reads all of it over that connection and writes only what
// changed: one bd, the note of when the host was last level.
func TestANothingToDoDispatchPassStartsAtMostOneBd(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)),
		beads.WithActor("mw@desktop"), beads.WithKeptSaid(io.Discard))

	runner := apptest.NewFakeRunner()
	if err := runner.Start(ctx, application.SessionSpec{Name: application.SessionName("mw-e.1"), Command: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	ticks := &apptest.FakeTickLog{}
	_ = ticks.Append(ctx, now.Add(-time.Minute).Format(time.RFC3339)+" "+application.DispatchLogNothingReady)
	dispatch := application.Dispatch{
		Tracker: g, Memory: g, Mailbox: g,
		Worktrees:   struct{ application.Worktrees }{},
		Runner:      runner,
		Host:        "desktop",
		Cap:         1,
		MaxAttempts: 3,
		Rigs:        map[string]string{"millwright": t.TempDir()},
		SmokeHolds:  application.GristSmokeBook{Notes: g, Host: "desktop", Now: clock},
		Sync: application.Sync{
			Vault: &apptest.FakeVaultFiles{}, Tracker: g, Host: "desktop", Mode: application.BeadsSyncShared,
			Ticks: application.TickLogs{Dispatch: ticks}, MwCommit: "abc1234", Now: clock,
		},
		Load: &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 1, Cores: 16, MemAvailableMB: 8000, MemKnown: true}},
		Room: application.RoomLimits{LoadPerCore: 1, MinFreeMB: 1000},
		Log:  ticks,
		Now:  clock,
		Out:  io.Discard,
	}

	// The first pass may write what it finds for the first time.
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("the first pass: %v", err)
	}
	from := len(bdCallsSince(t, asked, 0))

	now = now.Add(time.Minute)
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if len(report.Started) != 0 || report.Running != 1 {
		t.Errorf("expected nothing started beside the one running, got started %v, running %d", report.Started, report.Running)
	}
	why := map[string]string{}
	for _, passed := range report.Passed {
		why[passed.StoryID] = passed.Gate
	}
	if why["mw-e.2"] != "cap" || len(report.Passed) != 1 {
		t.Errorf("expected mw-e.2 held by the cap, and mw-e.3 (the laptop's) and mw-e.4 (waiting) not offered, got %+v", report.Passed)
	}
	calls := bdCallsSince(t, asked, from)
	if len(calls) > 1 {
		t.Errorf("expected at most one bd started by a pass with nothing to start, got %d:\n%s", len(calls), strings.Join(calls, "\n"))
	}
	if len(calls) == 1 && !strings.Contains(calls[0], "kv set "+application.LastSyncKey("desktop")) {
		t.Errorf("expected the one bd to be the note of when the host was last level, got %s", calls[0])
	}
	if len(server.unknown) > 0 {
		t.Errorf("the stand-in server was asked what it does not know:\n%s", strings.Join(server.unknown, "\n"))
	}
}

// The work read over the kept connection is what bd's own listings say: a
// story waiting on a bead not closed, on one that is no bead, or under an epic
// that waits is not ready; a wait on a closed bead is over and is no need; a
// molecule is never ready; an open story still assigned is claimed. None of it
// starts a bd, nor does the graph of every bead.
func TestTheWorkIsReadOverTheKeptConnectionAsBdListsIt(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	at := "2026-10-18 10:00:00"
	more := func(id, status, typ, assignee, metadata string) []driver.Value {
		var who driver.Value
		if assignee != "" {
			who = assignee
		}
		return []driver.Value{id, "The bead " + id, "", "", status, typ, who, nil, at, nil, at, nil, int64(1), metadata, nil, int64(0)}
	}
	server.beads = append(server.beads,
		more("mw-f", "open", "epic", "", `{"rig":"millwright","branch":"main","harness":"claude","model":"opus","effort":"high","host":"desktop"}`),
		more("mw-f.1", "open", "task", "", `{}`),
		more("mw-e.6", "open", "task", "", `{}`),
		more("mw-e.7", "open", "task", "mw@laptop", `{"host":"laptop"}`),
		more("mw-mol-1", "open", "molecule", "", `{}`),
	)
	server.edges = append(server.edges,
		[3]string{"mw-f", "mw-e.5", "blocks"}, [3]string{"mw-f.1", "mw-f", "parent-child"},
		[3]string{"mw-e.6", "mw-e", "parent-child"}, [3]string{"mw-e.6", "mw-wisp-1", "blocks"},
		[3]string{"mw-e.7", "mw-e", "parent-child"},
	)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	ready, err := g.ReadyForHost(ctx, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if ids := detailIDs(ready); strings.Join(ids, " ") != "mw-e.2" {
		t.Errorf("expected only mw-e.2 ready on the desktop, got %v", ids)
	}
	if len(ready) == 1 && (len(ready[0].Needs) != 0 || ready[0].Merged().Rig != "millwright") {
		t.Errorf("expected mw-e.2 to need nothing (its wait is closed) and to have its epic's path, got needs %v, path %+v", ready[0].Needs, ready[0].Merged())
	}
	here, away, err := g.RunningEverywhere(ctx, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(detailIDs(here), " ") != "mw-e.1" || strings.Join(detailIDs(away), " ") != "mw-e.5" {
		t.Errorf("expected mw-e.1 running here and mw-e.5 away, got %v and %v", detailIDs(here), detailIDs(away))
	}
	work, err := g.WorkInHand(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(detailIDs(work.Ready), " "); got != "mw-e.2 mw-e.3" {
		t.Errorf("expected mw-e.2 and mw-e.3 ready anywhere, got %s", got)
	}
	if got := strings.Join(detailIDs(work.OpenClaimed), " "); got != "mw-e.7" {
		t.Errorf("expected mw-e.7 open and claimed, got %s", got)
	}
	graph, err := g.BeadGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph) != len(server.beads) || graph[2].ID != "mw-e.2" || len(graph[2].Edges) != 2 || graph[2].Priority != 2 || graph[7].Priority != 1 {
		t.Errorf("expected every bead in the graph with its edges, got %+v", graph)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started, got %d:\n%s", len(calls), strings.Join(calls, "\n"))
	}
}

// A note already holding what is to be written is not written again, nor is a
// note not there cleared: each would be a bd for nothing. A note that changes,
// and one that is there to clear, is a bd as ever.
func TestANoteIsWrittenOnlyWhenItChanges(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	server.notes["host.desktop.room"] = "ok"
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	for _, write := range []func() error{
		func() error { return g.SetNote(ctx, "host.desktop.room", "ok") },
		func() error { return g.ClearNote(ctx, "host.desktop.stuck") },
	} {
		if err := write(); err != nil {
			t.Fatal(err)
		}
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Fatalf("expected no bd for a note left as it was, got %v", calls)
	}
	if err := g.SetNote(ctx, "host.desktop.room", "no room: load 17 of 16"); err != nil {
		t.Fatal(err)
	}
	if err := g.ClearNote(ctx, "host.desktop.room"); err != nil {
		t.Fatal(err)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 2 {
		t.Fatalf("expected a bd for the change and one for the clear, got %v", calls)
	}
}

func detailIDs(details []application.StoryDetail) []string {
	ids := make([]string, 0, len(details))
	for _, d := range details {
		ids = append(ids, d.Story.ID)
	}
	return ids
}

// mw-gq6.826: a pass asked bd for the run state, the comments and the open
// molecule of each candidate, one by one. Over the kept connection the states
// of all of them are one query, and the open molecules one read of the live
// beads; neither starts a bd.
func TestTheStatesOfSeveralStoriesAreOneReadOverTheKeptConnection(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	got, err := g.StoriesState(ctx, []string{"mw-e.1", "mw-e.2", "mw-e.5"}, application.RunState)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"mw-e.1": "running", "mw-e.2": "", "mw-e.5": "running"}
	if len(got) != len(want) {
		t.Fatalf("expected a state for each of three stories, got %v", got)
	}
	for id, state := range want {
		if value, held := got[id]; !held || value != state {
			t.Errorf("expected %s to read %q, got %q (there: %v)", id, state, value, held)
		}
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started, got %d:\n%s", len(calls), strings.Join(calls, "\n"))
	}
	if len(server.unknown) > 0 {
		t.Errorf("the stand-in server was asked what it does not know:\n%s", strings.Join(server.unknown, "\n"))
	}
}

func TestTheOpenMoleculesOfSeveralRootsAreOneReadOverTheKeptConnection(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	at := "2026-10-18 10:00:00"
	more := func(id, status, typ string) []driver.Value {
		return []driver.Value{id, "The bead " + id, "", "", status, typ, nil, nil, at, nil, at, nil, int64(2), `{}`, nil, int64(0)}
	}
	server.beads = append(server.beads,
		more("mw-mol-a", "open", "molecule"),
		more("mw-mol-a.1", "open", "task"), more("mw-mol-a.2", "open", "task"), more("mw-mol-a.3", "closed", "task"),
		more("mw-mol-b", "open", "molecule"), more("mw-mol-b.1", "open", "task"),
		more("mw-mol-done", "closed", "molecule"), more("mw-mol-done.1", "closed", "task"),
	)
	server.edges = append(server.edges,
		[3]string{"mw-mol-a.1", "mw-mol-a", "parent-child"}, [3]string{"mw-mol-a.2", "mw-mol-a", "parent-child"},
		[3]string{"mw-mol-a.2", "mw-mol-a.1", "blocks"}, [3]string{"mw-mol-a.3", "mw-mol-a", "parent-child"},
		[3]string{"mw-mol-b.1", "mw-mol-b", "parent-child"}, [3]string{"mw-mol-done.1", "mw-mol-done", "parent-child"},
	)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	got, err := g.OpenMolecules(ctx, []string{"mw-mol-a", "mw-mol-b", "mw-mol-done", "mw-mol-nope"})
	if err != nil {
		t.Fatal(err)
	}
	stepIDs := func(m application.Molecule) string {
		var ids []string
		for _, step := range m.Steps {
			ids = append(ids, step.ID)
		}
		return strings.Join(ids, " ")
	}
	if a := got["mw-mol-a"]; a.RootID != "mw-mol-a" || stepIDs(a) != "mw-mol-a.1 mw-mol-a.2" {
		t.Errorf("expected mw-mol-a open with its two steps not closed, in worked order, got %+v", a)
	}
	if b := got["mw-mol-b"]; b.RootID != "mw-mol-b" || stepIDs(b) != "mw-mol-b.1" {
		t.Errorf("expected mw-mol-b open with its one step, got %+v", b)
	}
	// A root not found open may be closed, a wisp or missing: it is left out, for
	// the caller to ask OpenMolecule, rather than taken for a molecule not there.
	for _, id := range []string{"mw-mol-done", "mw-mol-nope"} {
		if m, held := got[id]; held {
			t.Errorf("expected %s left out, it being found open nowhere, got %+v", id, m)
		}
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started, got %d:\n%s", len(calls), strings.Join(calls, "\n"))
	}
}

// With no kept connection, or one that fails, the reads together are the reads
// one at a time that they stand in for, so that nothing is read otherwise than
// before.
func TestWithNoKeptConnectionTheReadsTogetherAreTheReadsOneAtATime(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithKeptSaid(io.Discard))

	states, err := g.StoriesState(ctx, []string{"mw-e.1", "mw-e.2", "mw-e.5"}, application.RunState)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 || states["mw-e.1"] != "" {
		t.Errorf("expected a state, empty here, for each of three stories, got %v", states)
	}
	calls := bdCallsSince(t, asked, 0)
	var stateCalls int
	for _, call := range calls {
		if strings.Contains(call, " state mw-e.") && strings.HasSuffix(call, " run") {
			stateCalls++
		}
	}
	if stateCalls != 3 {
		t.Errorf("expected one bd state for each of three stories, got %d:\n%s", stateCalls, strings.Join(calls, "\n"))
	}

	from := len(calls)
	// The stand-in bd shows nothing of them, so each is left out for the caller
	// to read alone and to fail as a read of it alone does.
	if open, err := g.OpenMolecules(ctx, []string{"mw-mol-a", "mw-mol-b"}); err != nil || len(open) != 0 {
		t.Errorf("expected no molecule the stand-in bd cannot show, and no failure of the rest, got %v, %v", open, err)
	}
	var shows int
	for _, call := range bdCallsSince(t, asked, from) {
		if strings.Contains(call, " show mw-mol-") {
			shows++
		}
	}
	if shows == 0 {
		t.Errorf("expected bd show asked for the molecule roots one at a time, got:\n%s", strings.Join(bdCallsSince(t, asked, from), "\n"))
	}
}
