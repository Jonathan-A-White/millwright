package beads_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// A bead typed task that has child beads is a parent, not a story a session
// can take (mw-gq6.1086): ReadyForHost still lists it, so that mw dispatch can
// say why it passed it over, but carries how many children it has. A bead with
// none, and one whose children are all closed, carries none.

// Over the kept connection: the live beads say who hangs from whom.
func TestAReadyTaskWithChildrenCarriesTheirCountOverTheKeptConnection(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	at := "2026-10-18 10:00:00"
	task := func(id, status string) []driver.Value {
		return []driver.Value{id, "The bead " + id, "", "", status, "task", nil, nil, at, nil, at, nil, int64(1), `{"host":"desktop"}`, nil, int64(0)}
	}
	server.beads = append(server.beads,
		task("mw-e.20", "open"), // a parent with two live children
		task("mw-e.20.1", "open"), task("mw-e.20.2", "in_progress"),
		task("mw-e.21", "open"), // its only child is closed
		task("mw-e.21.1", "closed"),
	)
	server.edges = append(server.edges,
		[3]string{"mw-e.20", "mw-e", "parent-child"}, [3]string{"mw-e.21", "mw-e", "parent-child"},
		[3]string{"mw-e.20.1", "mw-e.20", "parent-child"}, [3]string{"mw-e.20.2", "mw-e.20", "parent-child"},
		[3]string{"mw-e.21.1", "mw-e.21", "parent-child"},
	)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	ready, err := g.ReadyForHost(ctx, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	children := map[string]int{}
	for _, d := range ready {
		children[d.Story.ID] = d.Children
	}
	if children["mw-e.20"] != 2 {
		t.Errorf("expected mw-e.20 to carry its 2 live children, got %v", children)
	}
	if n, listed := children["mw-e.2"]; !listed || n != 0 {
		t.Errorf("expected mw-e.2 listed with no children, got %v", children)
	}
	if n, listed := children["mw-e.21"]; !listed || n != 0 {
		t.Errorf("expected mw-e.21, whose only child is closed, listed with no children, got %v", children)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started, got %v", calls)
	}
}

// parentStandIn is a stand-in bd whose `ready` lists mw-p.1 (with two children)
// and mw-p.2 (with none) pathed to laptop, and whose `list` lists the beads not
// closed, the two children among them (and none in progress); a call is recorded in the file it
// returns.
func parentStandIn(t *testing.T) (*beads.Gateway, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	asked := filepath.Join(dir, "asked")
	script := fmt.Sprintf(`#!/bin/sh
shift 2
case "$1" in
  --actor) shift 2 ;;
esac
printf '%%s\n' "$*" >> %q
case "$1" in
  ready)
    printf '[{"id":"mw-p.1","title":"t","status":"open","issue_type":"task","metadata":{"host":"laptop","rig":"millwright"}},'
    printf '{"id":"mw-p.2","title":"t","status":"open","issue_type":"task","metadata":{"host":"laptop","rig":"millwright"}}]' ;;
  list)
    case "$3" in
      in_progress|deferred) printf '[]'; exit 0 ;;
    esac
    printf '[{"id":"mw-p.1.1","title":"t","status":"open","issue_type":"task","parent":"mw-p.1"},'
    printf '{"id":"mw-p.1.2","title":"t","status":"in_progress","issue_type":"task","parent":"mw-p.1"},'
    printf '{"id":"mw-p.2","title":"t","status":"open","issue_type":"task"}]' ;;
  *) printf '[]' ;;
esac
`, asked)
	path := filepath.Join(dir, "bd-parent")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(t.TempDir(), beads.WithProgram(path)), asked
}

// With no kept connection: the same, from one more bd list.
func TestAReadyTaskWithChildrenCarriesTheirCountFromBd(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	g, _ := parentStandIn(t)

	ready, err := g.ReadyForHost(context.Background(), "laptop")
	if err != nil {
		t.Fatal(err)
	}
	children := map[string]int{}
	for _, d := range ready {
		children[d.Story.ID] = d.Children
	}
	if len(children) != 2 || children["mw-p.1"] != 2 || children["mw-p.2"] != 0 {
		t.Errorf("expected mw-p.1 to carry 2 children and mw-p.2 none, got %v", children)
	}
}

// With no kept connection, WorkInHand carries the same counts on what is ready
// as the kept connection does (mw-gq6.1089), so mw status does not show a
// parent as ready.
func TestWorkInHandCarriesTheChildrenOfAReadyTaskFromBd(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	g, _ := parentStandIn(t)

	inHand, err := g.WorkInHand(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	children := map[string]int{}
	for _, d := range inHand.Ready {
		children[d.Story.ID] = d.Children
	}
	if len(children) != 2 || children["mw-p.1"] != 2 || children["mw-p.2"] != 0 {
		t.Errorf("expected mw-p.1 to carry 2 children and mw-p.2 none, got %v", children)
	}
}

// With no kept connection, ReadyWithLabel carries the children of what it lists
// as the kept connection does (mw-gq6.1090).
func TestReadyWithLabelCarriesTheChildrenOfATaskFromBd(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	g, _ := parentStandIn(t)

	ready, err := g.ReadyWithLabel(context.Background(), "todo")
	if err != nil {
		t.Fatal(err)
	}
	children := map[string]int{}
	for _, d := range ready {
		children[d.Story.ID] = d.Children
	}
	if len(children) != 2 || children["mw-p.1"] != 2 || children["mw-p.2"] != 0 {
		t.Errorf("expected mw-p.1 to carry 2 children and mw-p.2 none, got %v", children)
	}
}

// With no kept connection, WorkInHand lists the held stories as the kept
// connection does (mw-gq6.1090).
func TestWorkInHandListsTheHeldStoriesFromBd(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
shift 2
case "$1" in
  --actor) shift 2 ;;
esac
case "$1" in
  list)
    case "$3" in
      deferred) printf '[{"id":"mw-h.1","title":"t","status":"deferred","issue_type":"task","metadata":{"host":"laptop"}}]'; exit 0 ;;
    esac
    printf '[]' ;;
  *) printf '[]' ;;
esac
`
	path := filepath.Join(dir, "bd-held")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	g := beads.New(t.TempDir(), beads.WithProgram(path))

	inHand, err := g.WorkInHand(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inHand.Held) != 1 || inHand.Held[0].Story.ID != "mw-h.1" {
		t.Errorf("expected the held story mw-h.1 in Held, got %v", inHand.Held)
	}
}

// claimedParentStandIn is a stand-in bd with nothing ready, mw-c.1 open but
// claimed by laptop, mw-c.2 in progress and mw-c.3 held, each a task with a
// child that is not closed; the live list (which leaves out the closed) names them.
func claimedParentStandIn(t *testing.T) *beads.Gateway {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	script := `#!/bin/sh
shift 2
case "$1" in
  --actor) shift 2 ;;
esac
case "$1" in
  list)
    case "$3" in
      open) printf '[{"id":"mw-c.1","title":"t","status":"open","issue_type":"task","assignee":"mw@laptop","metadata":{"host":"laptop"}}]' ;;
      in_progress) printf '[{"id":"mw-c.2","title":"t","status":"in_progress","issue_type":"task","assignee":"mw@laptop","metadata":{"host":"laptop"}}]' ;;
      deferred) printf '[{"id":"mw-c.3","title":"t","status":"deferred","issue_type":"task","metadata":{"host":"laptop"}}]' ;;
      *) printf '[{"id":"mw-c.1.1","title":"t","status":"open","issue_type":"task","parent":"mw-c.1"},'
         printf '{"id":"mw-c.2.1","title":"t","status":"open","issue_type":"task","parent":"mw-c.2"},'
         printf '{"id":"mw-c.3.1","title":"t","status":"open","issue_type":"task","parent":"mw-c.3"}]' ;;
    esac ;;
  *) printf '[]' ;;
esac
`
	path := filepath.Join(t.TempDir(), "bd-claimed")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(t.TempDir(), beads.WithProgram(path))
}

// WorkInHand carries the children of the claimed work as it does of what is
// ready (mw-gq6.1097): a task-typed parent that is open but claimed, running or
// held is a parent to whoever reads it.
func TestWorkInHandCarriesTheChildrenOfClaimedWorkFromBd(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	g := claimedParentStandIn(t)

	inHand, err := g.WorkInHand(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inHand.OpenClaimed) != 1 || inHand.OpenClaimed[0].Story.ID != "mw-c.1" || inHand.OpenClaimed[0].Children != 1 {
		t.Errorf("expected the claimed mw-c.1 with 1 child, got %+v", inHand.OpenClaimed)
	}
	if len(inHand.Running) != 1 || inHand.Running[0].Story.ID != "mw-c.2" || inHand.Running[0].Children != 1 {
		t.Errorf("expected the running mw-c.2 with 1 child , got %+v", inHand.Running)
	}
	if len(inHand.Held) != 1 || inHand.Held[0].Story.ID != "mw-c.3" || inHand.Held[0].Children != 1 {
		t.Errorf("expected the held mw-c.3 with 1 child, got %+v", inHand.Held)
	}
}

// Over the kept connection: the same counts, and no bd started.
func TestWorkInHandCarriesTheChildrenOfClaimedWorkOverTheKeptConnection(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	at := "2026-10-18 10:00:00"
	task := func(id, status, assignee string, parent bool) []driver.Value {
		var who driver.Value
		if assignee != "" {
			who = assignee
		}
		return []driver.Value{id, "The bead " + id, "", "", status, "task", who, nil, at, nil, at, nil, int64(1), `{"host":"desktop"}`, nil, int64(0)}
	}
	server.beads = append(server.beads,
		task("mw-c.1", "open", "mw@desktop", false), task("mw-c.1.1", "open", "", true),
		task("mw-c.2", "in_progress", "mw@desktop", false), task("mw-c.2.1", "open", "", true), task("mw-c.2.2", "closed", "", true),
		task("mw-c.3", "deferred", "", false), task("mw-c.3.1", "open", "", true),
	)
	server.edges = append(server.edges,
		[3]string{"mw-c.1.1", "mw-c.1", "parent-child"},
		[3]string{"mw-c.2.1", "mw-c.2", "parent-child"}, [3]string{"mw-c.2.2", "mw-c.2", "parent-child"},
		[3]string{"mw-c.3.1", "mw-c.3", "parent-child"},
	)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	inHand, err := g.WorkInHand(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := func(details []application.StoryDetail) map[string]int {
		out := map[string]int{}
		for _, d := range details {
			out[d.Story.ID] = d.Children
		}
		return out
	}
	if got := counts(inHand.OpenClaimed); got["mw-c.1"] != 1 {
		t.Errorf("expected the claimed mw-c.1 with 1 child, got %v", got)
	}
	if got := counts(inHand.Running); got["mw-c.2"] != 1 {
		t.Errorf("expected the running mw-c.2 with 1 child, got %v", got)
	}
	if got := counts(inHand.Held); got["mw-c.3"] != 1 {
		t.Errorf("expected the held mw-c.3 with 1 child, got %v", got)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started, got %v", calls)
	}
}

// A child of an epic that has children of its own is a parent to the reader of
// ShowEpics (mw-gq6.1097), and the children of every epic are counted with one
// more bd list, not one apiece.
func TestShowEpicsCarriesTheChildrenOfEachChild(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	live := `[{"id":"mw-e1.1.1","title":"t","status":"open","issue_type":"task","parent":"mw-e1.1"},` +
		`{"id":"mw-e2.1.1","title":"t","status":"open","issue_type":"task","parent":"mw-e2.1"},` +
		`{"id":"mw-e2.1.2","title":"t","status":"open","issue_type":"task","parent":"mw-e2.1"}]`
	db := epicsWithChildren()
	db.claimed = live
	g, log, _ := roundsStandIn(t, db)

	epics, err := g.ShowEpics(context.Background(), []string{"mw-e1", "mw-e2", "mw-e3"})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 0}
	for i, epic := range epics {
		if len(epic.Stories) != 1 || epic.Stories[0].Children != want[i] {
			t.Errorf("expected the child of %s to count %d open children, got %+v", epic.ID, want[i], epic.Stories)
		}
	}
	lives := 0
	for _, line := range asked(t, log) {
		if strings.Contains(line, "list --status") {
			lives++
		}
	}
	if lives != 1 {
		t.Errorf("expected one bd list of the live beads for all the epics, got %d:\n%v", lives, asked(t, log))
	}
}

// A bead page counts the children of its own children (mw-gq6.1097) in the
// bd calls it has: show, its children, and the one list of the live beads.
func TestABeadPageCarriesTheChildrenOfItsChildren(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	db := roundsDB{
		shown:    map[string]string{"mw-p": `{"id":"mw-p","title":"The parent","status":"open","issue_type":"task","priority":2}`},
		children: map[string]string{"mw-p": `[{"id":"mw-p.1","title":"a","status":"open","issue_type":"task","parent":"mw-p"},{"id":"mw-p.2","title":"b","status":"open","issue_type":"task","parent":"mw-p"}]`},
		claimed:  `[{"id":"mw-p.1.1","title":"t","status":"open","issue_type":"task","parent":"mw-p.1"}]`,
	}
	g, log, _ := roundsStandIn(t, db)

	page, found, err := g.ShowBeadPage(context.Background(), "mw-p")
	if err != nil || !found {
		t.Fatalf("reading the page: found %v: %v", found, err)
	}
	counts := map[string]int{}
	for _, child := range page.Children {
		counts[child.Story.ID] = child.Children
	}
	if len(counts) != 2 || counts["mw-p.1"] != 1 || counts["mw-p.2"] != 0 {
		t.Errorf("expected mw-p.1 to count 1 child and mw-p.2 none, got %v", counts)
	}
	if n := bdCalls(t, log); n > 3 {
		t.Errorf("expected at most 3 bd calls, got %d:\n%v", n, asked(t, log))
	}
}

// A snapshot's ShowEpics reads the listing it holds, so it counts each child's
// open children from that listing as the gateway's does (mw-gq6.1097).
func TestASnapshotsShowEpicsCarriesTheChildrenOfEachChild(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	at := "2026-10-18 10:00:00"
	task := func(id, status string) []driver.Value {
		return []driver.Value{id, "The bead " + id, "", "", status, "task", nil, nil, at, nil, at, nil, int64(1), `{"host":"desktop"}`, nil, int64(0)}
	}
	server.beads = append(server.beads, task("mw-e.6", "open"), task("mw-e.6.1", "open"), task("mw-e.6.2", "closed"))
	server.edges = append(server.edges,
		[3]string{"mw-e.6", "mw-e", "parent-child"},
		[3]string{"mw-e.6.1", "mw-e.6", "parent-child"}, [3]string{"mw-e.6.2", "mw-e.6", "parent-child"})
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	tracker, err := g.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	epics, err := tracker.ShowEpics(ctx, []string{"mw-e"})
	if err != nil || len(epics) != 1 {
		t.Fatalf("reading the epic: %+v, %v", epics, err)
	}
	counts := map[string]int{}
	for _, d := range epics[0].Stories {
		counts[d.Story.ID] = d.Children
	}
	if counts["mw-e.6"] != 1 || counts["mw-e.2"] != 0 {
		t.Errorf("expected mw-e.6 to count its 1 open child and mw-e.2 none, got %v", counts)
	}
}
