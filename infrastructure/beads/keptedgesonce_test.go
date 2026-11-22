package beads_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// mw-gq6.1626: the listing of every bead and the bead graph each read the whole
// dependencies table (4.1 MB, 1 to 3.6 s at load), twice in one mw doctor
// --dry-run. Under a run cache the table is read once and each of them keeps the
// edges of the beads it holds.
func TestUnderARunCacheTheWholeDependenciesAreReadOnceForTheListingAndTheGraph(t *testing.T) {
	s := &keptServer{}
	s.graphed.Store(true)
	program, _ := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)), beads.WithRunCache())
	ctx := context.Background()

	if _, err := g.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	graph, err := g.BeadGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	whole := 0
	for _, q := range s.asked() {
		l := strings.ToLower(q)
		if strings.Contains(l, "from dependencies d") && !strings.Contains(l, "where") {
			whole++
		}
	}
	if whole != 1 {
		t.Errorf("expected the whole dependencies read once for the listing and the graph, got %d reads", whole)
	}
	found := false
	for _, b := range graph {
		if b.ID == "t-1.1" && len(b.Edges) == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the graph to hold the two edges of t-1.1, got %v", graph)
	}
}
