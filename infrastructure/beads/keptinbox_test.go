package beads_test

import (
	"context"
	"database/sql/driver"
	"io"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// mw-gq6.749: the events follower listed the Mayor's whole open inbox by bd
// every few seconds. A mailbox is listed over the kept connection, as bd lists
// it, and no bd is started.
func TestAMailboxIsListedOverTheKeptConnectionWithoutStartingABd(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	mail := func(id, status, assignee, at, metadata string) []driver.Value {
		return []driver.Value{id, "Subject of " + id, "Body of " + id, "", status, "mail", assignee, nil, at, nil, at, nil, int64(2), metadata, nil, int64(0)}
	}
	server.beads = append(server.beads,
		mail("mw-m2", "open", "mayor", "2026-10-18 09:30:00", `{"from":"deputy","to":"mayor"}`),
		mail("mw-m1", "open", "mayor", "2026-10-18 09:10:00", `{"from":"builder"}`),
		mail("mw-m3", "closed", "mayor", "2026-10-18 09:40:00", `{}`),
		mail("mw-m4", "open", "deputy", "2026-10-18 09:50:00", `{}`),
	)
	server.edges = append(server.edges, [3]string{"mw-m2", "mw-m1", "related"})
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	got, err := g.Inbox(ctx, "mayor")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, " ") != "mw-m1 mw-m2" {
		t.Fatalf("the inbox of the mayor is %v, want the open mail assigned to it, oldest first: mw-m1 mw-m2", ids)
	}
	if got[1].From != "deputy" || got[1].Subject != "Subject of mw-m2" || got[1].Body != "Body of mw-m2" || got[1].ReplyTo != "mw-m1" {
		t.Errorf("the message reads %+v", got[1])
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("listing the inbox started bd %d times:\n%s", len(calls), strings.Join(calls, "\n"))
	}
	if len(server.unknown) > 0 {
		t.Errorf("the stand-in server was asked what it does not know:\n%s", strings.Join(server.unknown, "\n"))
	}
}

// With no kept connection the inbox is listed by bd, as it was.
func TestAMailboxIsListedByBdWhenThereIsNoKeptConnection(t *testing.T) {
	program, asked := recordingBd(t)
	g := beads.New(t.TempDir(), beads.WithProgram(program))
	if _, err := g.Inbox(context.Background(), "mayor"); err != nil {
		t.Fatal(err)
	}
	calls := bdCallsSince(t, asked, 0)
	if len(calls) != 1 || !strings.Contains(calls[0], "list --type mail --assignee mayor --status open --limit 0 --json") {
		t.Fatalf("expected one bd list of the inbox, got %q", calls)
	}
}

// mw-gq6.1530: the inbox's edges were a join of every dependency with the
// issues on the mailbox's own condition, and an order by on top. They are read
// for the ids of the mail found, which the server finds by an index.
func TestTheEdgesOfAnInboxAreReadByTheIdsOfItsMailAndNotByAJoin(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	mail := func(id, at string) []driver.Value {
		return []driver.Value{id, "Subject of " + id, "Body of " + id, "", "open", "mail", "mayor", nil, at, nil, at, nil, int64(2), `{}`, nil, int64(0)}
	}
	server.beads = append(server.beads, mail("mw-m1", "2026-10-18 09:10:00"), mail("mw-m2", "2026-10-18 09:30:00"))
	server.edges = append(server.edges, [3]string{"mw-m2", "mw-m1", "related"}, [3]string{"mw-e.2", "mw-e.9", "related"})
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	got, err := g.Inbox(ctx, "mayor")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].ReplyTo != "mw-m1" {
		t.Fatalf("expected the two mails, the second a reply to the first, got %+v", got)
	}
	server.mu.Lock()
	seen := append([]string(nil), server.seen...)
	server.mu.Unlock()
	edges := 0
	for _, q := range seen {
		l := strings.ToLower(q)
		if !strings.Contains(l, "from dependencies") {
			continue
		}
		edges++
		if strings.Contains(l, "join issues i on i.id = d.issue_id") || strings.Contains(l, "order by") {
			t.Errorf("expected the edges read with no join and no order by, got %s", q)
		}
		if !strings.Contains(l, "d.issue_id in ('mw-m1', 'mw-m2')") {
			t.Errorf("expected the edges read by the ids of the mail found, got %s", q)
		}
	}
	if edges != 1 {
		t.Errorf("expected one edges statement, got %d", edges)
	}
}
