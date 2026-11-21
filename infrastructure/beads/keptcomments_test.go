package beads_test

import (
	"context"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

var commentCountsAsked = regexp.MustCompile(`(?i)from comments c where c\.issue_id in \(([^)]*)\) group by c\.issue_id`)

// mw-gq6.1532: the listing's comment count was a subselect that grouped the
// whole comments table, for every bead there is. The counts are read for the
// ids of the beads listed, and each bead still has its own.
func TestAListingsCommentCountsAreReadForTheIdsOfItsBeadsOnly(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	if _, err := g.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	seen := append([]string(nil), server.seen...)
	server.mu.Unlock()
	var counted string
	for _, q := range seen {
		l := strings.ToLower(q)
		if strings.Contains(l, "leases") && strings.Contains(l, "from comments") {
			t.Errorf("expected the listing of the beads not to read the comments table, got %s", q)
		}
		if m := commentCountsAsked.FindStringSubmatch(q); m != nil {
			counted = m[1]
		} else if strings.Contains(l, "from comments") && strings.Contains(l, "group by") && !strings.Contains(l, "union all") {
			t.Errorf("expected the comments counted for the ids listed, got %s", q)
		}
	}
	if counted == "" {
		t.Fatalf("expected one statement counting the comments of the beads listed, saw:\n%s", strings.Join(seen, "\n"))
	}
	for _, id := range []string{"mw-e", "mw-e.1", "mw-e.2", "mw-e.9"} {
		if !strings.Contains(counted, "'"+id+"'") {
			t.Errorf("expected %s among the ids counted, got %s", id, counted)
		}
	}
	// mw-e.2 has one comment, so the snapshot reads the comments of it and of no
	// other bead.
	var read string
	for _, q := range seen {
		if strings.Contains(strings.ToLower(q), "union all select 'k'") {
			read = q
		}
	}
	if !strings.Contains(read, "'mw-e.2'") || strings.Contains(read, "'mw-e.1'") {
		t.Errorf("expected the comments read for mw-e.2 alone, got %s", read)
	}
}
