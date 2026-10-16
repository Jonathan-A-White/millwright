package beads_test

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TriageBeads is one bd list of everything but mail and events: it keeps the
// formula beads, takes a story's rig from its parent when it names none, and
// reads the blockers and formula root a bead carries.
func TestTriageBeadsIsOneListAndReadsRigBlockersAndFormula(t *testing.T) {
	gateway, log := leaseStandIn(t, map[string]string{
		"list": `[{"id": "t-e", "title": "An epic", "status": "open", "issue_type": "epic", "metadata": {"rig": "millwright"}},
		          {"id": "t-e.1", "title": "one", "status": "open", "issue_type": "task", "parent": "t-e", "priority": 1,
		           "labels": ["icebox"], "metadata": {"molecule": "mw-mol-r", "size": "S"},
		           "dependencies": [{"issue_id": "t-e.1", "depends_on_id": "t-e", "type": "parent-child"},
		                            {"issue_id": "t-e.1", "depends_on_id": "t-x", "type": "blocks"}]},
		          {"id": "mw-mol-r", "title": "root", "status": "open", "issue_type": "molecule"}]`,
	})

	beads, err := gateway.TriageBeads(context.Background())
	if err != nil {
		t.Fatalf("reading the beads: %v", err)
	}
	if len(beads) != 3 {
		t.Fatalf("got %d beads, want all 3 (formula beads stay in)", len(beads))
	}
	one := beads[1]
	if one.Rig != "millwright" || one.Priority != 1 || one.Size != "S" || one.Molecule != "mw-mol-r" ||
		len(one.Needs) != 1 || one.Needs[0] != "t-x" || len(one.Labels) != 1 {
		t.Fatalf("got %+v", one)
	}

	asked, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(asked)), "\n"); len(lines) != 1 ||
		!strings.Contains(lines[0], "list --all --limit 0 --exclude-type mail,event --json") {
		t.Fatalf("expected one bd list of everything, got %q", asked)
	}
}
