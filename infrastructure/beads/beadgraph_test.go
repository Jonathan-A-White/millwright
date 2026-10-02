package beads_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

// BeadGraph is one bd list of everything but mail, events and molecules, and
// reads the edges bd prints under each bead as the links that bead carries.
func TestBeadGraphIsOneListAndReadsEachBeadsEdges(t *testing.T) {
	gateway, log := leaseStandIn(t, map[string]string{
		"list": `[{"id": "t-e", "title": "An epic", "status": "open", "issue_type": "epic", "labels": ["wayfinder:map"]},
		          {"id": "t-e.1", "title": "one", "status": "closed", "issue_type": "task",
		           "dependencies": [{"issue_id": "t-e.1", "depends_on_id": "t-e", "type": "parent-child"},
		                            {"issue_id": "t-e.1", "depends_on_id": "t-x", "type": "related"}]}]`,
	})

	graph, err := gateway.BeadGraph(context.Background())
	if err != nil {
		t.Fatalf("reading the graph: %v", err)
	}
	if len(graph) != 2 || graph[0].ID != "t-e" || graph[0].Type != "epic" || len(graph[0].Labels) != 1 {
		t.Fatalf("got %+v, want the epic first with its label", graph)
	}
	want := []domain.BeadEdge{{To: "t-e", Kind: domain.EdgeParentChild}, {To: "t-x", Kind: domain.EdgeRelated}}
	if got := graph[1].Edges; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got edges %+v, want %+v", got, want)
	}

	asked, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(asked)), "\n"); len(lines) != 1 ||
		!strings.Contains(lines[0], "list --all --limit 0 --exclude-type mail,event,molecule --json") {
		t.Fatalf("expected one bd list of everything, got %q", asked)
	}
}
