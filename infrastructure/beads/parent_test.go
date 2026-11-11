package beads_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
