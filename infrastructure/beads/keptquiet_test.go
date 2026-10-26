package beads_test

import (
	"context"
	"database/sql/driver"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// aQuietFactoryWithAWaitingHitl is aQuietFactory with the desktop's running
// story claimed long ago, and two beads for the Governor under the epic: one
// ready, one waiting on a story still running.
func aQuietFactoryWithAWaitingHitl(asked string) *factoryServer {
	server := aQuietFactory(asked)
	server.beads[1][9] = "2026-10-18 09:30:00" // mw-e.1 was started three hours before the tests' now
	at := "2026-10-18 09:00:00"
	hitl := func(id string) []driver.Value {
		return []driver.Value{id, "The bead " + id, "", "", "open", "task", nil, nil, at, nil, at, nil, int64(2), `{}`, nil, int64(0)}
	}
	server.beads = append(server.beads, hitl("mw-e.10"), hitl("mw-e.11"))
	server.labels = append(server.labels, [2]string{"mw-e.10", "hitl"}, [2]string{"mw-e.11", "hitl"})
	server.edges = append(server.edges,
		[3]string{"mw-e.10", "mw-e", "parent-child"}, [3]string{"mw-e.11", "mw-e", "parent-child"},
		[3]string{"mw-e.11", "mw-e.5", "blocks"})
	return server
}

// mw-gq6.613: after mw-gq6.608 half of the Laptop's new connections to the beads
// server came from contrib/mail-notify's tick: mw nudge started a bd for the
// beads for the Governor that are ready, and two for each story running here (its
// run state, and the open steps of its formula), every minute. With the
// connection kept open the quiet alarm reads all of it over that connection.
func TestTheQuietAlarmStartsNoBdWithAKeptConnection(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactoryWithAWaitingHitl(asked)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)),
		beads.WithActor("mw@desktop"), beads.WithKeptSaid(io.Discard))
	now := time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC)

	clauses, err := application.Nudge{
		Tracker: g, Notes: g, Mayor: application.MayorReader{Tracker: g, Notes: g, Now: func() time.Time { return now }},
		Host: "desktop", Now: func() time.Time { return now },
	}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	keys := map[string]string{}
	for _, c := range clauses {
		keys[c.Key] = c.Text
	}
	if len(clauses) != 2 || !strings.Contains(keys["mw-e.1"], "in progress 210 min") || !strings.Contains(keys["mayor.mw-e.10"], "waiting on the Mayor 240 min") {
		t.Errorf("expected the quiet story mw-e.1 and the ready bead for the Governor mw-e.10 waiting on the Mayor, got %+v", clauses)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started by the quiet alarm, got %d:\n%s", len(calls), strings.Join(calls, "\n"))
	}
	if len(server.unknown) > 0 {
		t.Errorf("the stand-in server was asked what it does not know:\n%s", strings.Join(server.unknown, "\n"))
	}
}

// The ready beads under a label read over the kept connection are what bd ready
// --label lists: open, nobody's epic, and not waiting on a bead still open.
func TestTheReadyBeadsUnderALabelAreReadOverTheKeptConnectionAsBdReadyListsThem(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactoryWithAWaitingHitl(asked)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	ready, err := g.ReadyWithLabel(ctx, "hitl")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(detailIDs(ready), " "); got != "mw-e.10" {
		t.Errorf("expected only mw-e.10 ready under hitl (mw-e.11 waits on a running story), got %s", got)
	}
	if len(ready) == 1 && ready[0].Merged().Rig != "millwright" {
		t.Errorf("expected mw-e.10 to have its epic's path, got %+v", ready[0].Merged())
	}
	none, err := g.ReadyWithLabel(ctx, "no-such-label")
	if err != nil || len(none) != 0 {
		t.Errorf("expected nothing ready under a label nobody carries, got %v, %v", detailIDs(none), err)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started, got %d:\n%s", len(calls), strings.Join(calls, "\n"))
	}
}

// bd state reads the value of a story's label for a dimension; one with none
// set reads as empty.
func TestAStoryStateIsReadOverTheKeptConnectionAsBdStateReadsIt(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactoryWithAWaitingHitl(asked)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	if got, err := g.StoryState(ctx, "mw-e.1", application.RunState); err != nil || got != "running" {
		t.Errorf("expected mw-e.1 to be running, got %q, %v", got, err)
	}
	if got, err := g.StoryState(ctx, "mw-e.2", application.RunState); err != nil || got != "" {
		t.Errorf("expected mw-e.2 to have no run state, got %q, %v", got, err)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started, got %d:\n%s", len(calls), strings.Join(calls, "\n"))
	}
}

// mw-gq6.613: mw postern snapshot read the live epics with a bd, each epic's
// children with another bd apiece (66 live epics were 66 bd), and the rest of
// what it needs with a few more, every ten minutes while the beads moved. It
// takes a snapshot of every bead as the Governor's view does, and over the kept
// connection starts none.
func TestThePosternSnapshotStartsNoBdWithAKeptConnection(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactoryWithAWaitingHitl(asked)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)),
		beads.WithActor("mw@desktop"), beads.WithKeptSaid(io.Discard))
	now := time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC)
	snapshot := application.PosternSnapshot{Tracker: g, Notes: g, Now: func() time.Time { return now }, Err: io.Discard}

	doc, err := snapshot.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Epics) != 1 || doc.Epics[0].ID != "mw-e" {
		t.Fatalf("expected the one live epic, mw-e, got %+v", doc.Epics)
	}
	if got := len(doc.Epics[0].Working); got == 0 {
		t.Errorf("expected working stories under mw-e, got %+v", doc.Epics[0])
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started by a snapshot, got %d:\n%s", len(calls), strings.Join(calls, "\n"))
	}
	if len(server.unknown) > 0 {
		t.Errorf("the stand-in server was asked what it does not know:\n%s", strings.Join(server.unknown, "\n"))
	}
}

// mw-gq6.613: the follower's cloud check read its work in hand, a bd ready and
// two bd list, every minute. It counts the stories that wait for any host by the
// rule bd ready has (open, nobody's, nothing open to wait on), and over the kept
// connection starts no bd to do it.
func TestTheCloudCheckReadsItsWorkInHandOverTheKeptConnection(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactoryWithAWaitingHitl(asked)
	at := "2026-10-18 09:00:00"
	story := func(id, status, assignee string) []driver.Value {
		var who driver.Value
		if assignee != "" {
			who = assignee
		}
		return []driver.Value{id, "The bead " + id, "", "", status, "task", who, nil, at, nil, at, nil, int64(2), `{"host":"auto"}`, nil, int64(0)}
	}
	server.beads = append(server.beads, story("mw-e.12", "open", ""), story("mw-e.13", "open", ""), story("mw-e.14", "open", "mw@laptop"), story("mw-e.15", "deferred", ""))
	server.edges = append(server.edges,
		[3]string{"mw-e.12", "mw-e", "parent-child"}, [3]string{"mw-e.13", "mw-e", "parent-child"}, [3]string{"mw-e.14", "mw-e", "parent-child"},
		[3]string{"mw-e.13", "mw-e.5", "blocks"})
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)),
		beads.WithActor("mw@desktop"), beads.WithKeptSaid(io.Discard))
	now := time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC)
	up := now.Add(-10 * time.Minute)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: up, IdleSince: up}}}}

	report, err := application.CloudCheck{
		Work: g, Stories: g, Notes: g, Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: application.CloudPlan{MaxBoxes: 2, MonthlyCapUSD: 75, HourlyUSD: 0.132, Idle: 30 * time.Minute, BoxCap: 2},
		Now:  func() time.Time { return now },
	}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if report.Waiting != 1 {
		t.Errorf("expected only mw-e.12 to wait for any host (mw-e.13 waits on a running story, mw-e.14 is held), got %d", report.Waiting)
	}
	if work, err := g.WorkInHand(ctx); err != nil || len(work.Held) != 1 || work.Held[0].Story.ID != "mw-e.15" {
		t.Errorf("expected the deferred mw-e.15 as the one story held, got %+v, %v", work.Held, err)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started by the cloud check, got %d:\n%s", len(calls), strings.Join(calls, "\n"))
	}
	if len(server.unknown) > 0 {
		t.Errorf("the stand-in server was asked what it does not know:\n%s", strings.Join(server.unknown, "\n"))
	}
}
