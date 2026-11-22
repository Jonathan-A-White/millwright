package beads_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// mw-gq6.1625: the host-upkeep check of mw doctor asks ShowBeads for the stories
// of the worktrees on the host, through the doctor's gateway, which keeps a run
// cache; the cache answered by reading every bead (51.8 MB, 14 to 60 s of the
// server) for a handful of ids. ShowBeadsFew, which the check asks, reads the
// beads by id when the run holds no list yet.
func TestShowingFewBeadsUnderARunCacheReadsThemByIdNotEveryBead(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard), beads.WithRunCache())

	got, err := g.ShowBeadsFew(ctx, []string{"mw-e.4", "mw-e.9"})
	if err != nil {
		t.Fatal(err)
	}
	if ids := strings.Join(detailIDs(got), " "); ids != "mw-e.4 mw-e.9" {
		t.Fatalf("expected the two beads asked, got %s", ids)
	}
	for _, q := range server.queries() {
		l := strings.ToLower(q)
		if strings.Contains(l, "from issues i") && strings.Contains(l, "comment_count") && !strings.Contains(l, "i.id in (") {
			t.Errorf("showing two beads read more than those two:\n%s", q)
		}
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("showing beads over the kept connection started bd %d times:\n%s", len(calls), strings.Join(calls, "\n"))
	}
}

// A run that already holds the list of every bead answers from it, as it did.
func TestShowingFewBeadsUnderARunCacheThatHoldsTheListAnswersFromIt(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard), beads.WithRunCache())

	if _, err := g.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	before := len(server.queries())
	got, err := g.ShowBeadsFew(ctx, []string{"mw-e.4", "mw-e.9"})
	if err != nil {
		t.Fatal(err)
	}
	if ids := strings.Join(detailIDs(got), " "); ids != "mw-e.4 mw-e.9" {
		t.Fatalf("expected the two beads asked, got %s", ids)
	}
	if after := len(server.queries()); after != before {
		t.Errorf("expected the held list to answer, but the server was sent %d more queries", after-before)
	}
}

// queries is every query the stand-in server was sent, in order.
func (s *factoryServer) queries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}
