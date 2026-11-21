package beads_test

import (
	"context"
	"database/sql/driver"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// mw-gq6.1544: the tidy of every millhand tick read the open mail and the open
// formulas by bd, the two bd a tick still started. Both are read over the kept
// connection, as bd lists them, and no bd is started.
func TestTheTidysOpenMailAndOpenFormulasAreReadOverTheKeptConnectionWithoutStartingABd(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	row := func(id, title, status, typ, metadata string) []driver.Value {
		return []driver.Value{id, title, "", "", status, typ, nil, nil, "2026-10-18 09:00:00", nil, "2026-10-18 09:00:00", nil, int64(2), metadata, nil, int64(0)}
	}
	server.beads = append(server.beads,
		row("mw-m2", "Second", "open", "mail", `{}`),
		row("mw-m1", "First", "open", "mail", `{}`),
		row("mw-m3", "Read", "closed", "mail", `{}`),
		row("mw-ev", "An event", "open", "event", `{}`),
		row("t-1", "A running story", "in_progress", "task", `{"molecule":"t-mol-a"}`),
		row("t-mol-a", "A running story", "open", "molecule", `{}`),
		row("t-mol-a1", "Implement until green", "open", "task", `{}`),
		row("t-mol-b2", "Understand t-2: A closed story", "open", "task", `{}`),
		row("t-mol-b3", "Commit", "open", "task", `{}`),
		row("t-mol-c1", "Understand t-9: Done", "closed", "task", `{}`),
		row("t-mol-c2", "Commit", "open", "task", `{}`),
	)
	server.edges = append(server.edges,
		[3]string{"t-mol-a1", "t-mol-a", "parent-child"},
		[3]string{"t-mol-b2", "t-mol-b", "parent-child"},
		[3]string{"t-mol-b3", "t-mol-b", "parent-child"},
		[3]string{"t-mol-b3", "t-mol-b2", "blocks"},
		[3]string{"t-mol-c1", "t-mol-c", "parent-child"},
		[3]string{"t-mol-c2", "t-mol-c", "parent-child"},
	)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	mail, err := g.OpenMail(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range mail {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, " ") != "mw-m1 mw-m2" {
		t.Errorf("the open mail is %v, want every open message, oldest first, by id: mw-m1 mw-m2", ids)
	}

	got, err := g.OpenFormulas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []application.PouredFormula{
		{Root: "t-mol-a", RootOpen: true, Steps: []string{"t-mol-a1"}, LiveStory: "t-1"},
		{Root: "t-mol-b", Steps: []string{"t-mol-b2", "t-mol-b3"}, Story: "t-2"},
		// Its only open step names no story: the closed one does.
		{Root: "t-mol-c", Steps: []string{"t-mol-c2"}, Story: "t-9"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the open formulas are %+v\nwant %+v", got, want)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("reading the open mail and formulas started bd %d times:\n%s", len(calls), strings.Join(calls, "\n"))
	}
	if len(server.unknown) > 0 {
		t.Errorf("the stand-in server was asked what it does not know:\n%s", strings.Join(server.unknown, "\n"))
	}
}

// With no kept connection the open mail and the open formulas are listed by bd,
// as they were.
func TestTheTidysOpenMailAndOpenFormulasAreListedByBdWhenThereIsNoKeptConnection(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	g := beads.New(t.TempDir(), beads.WithProgram(program))
	if _, err := g.OpenMail(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := g.OpenFormulas(ctx); err != nil {
		t.Fatal(err)
	}
	calls := bdCallsSince(t, asked, 0)
	if len(calls) != 2 || !strings.Contains(calls[0], "list --type mail --status open --limit 0 --json") ||
		!strings.Contains(calls[1], "list --limit 0 --brief --exclude-type mail,event --json") {
		t.Fatalf("expected one bd list of the open mail and one of the open beads, got %q", calls)
	}
}
