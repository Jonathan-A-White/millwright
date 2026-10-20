package beads_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// runListing is a vault with an epic and its story, a poured formula with an
// open and a closed step, and a mail bead.
const runListing = `[
 {"id": "t-e", "title": "An epic", "status": "open", "issue_type": "epic", "created_at": "2026-10-01T10:00:00Z"},
 {"id": "t-e.1", "title": "A story", "status": "open", "issue_type": "task", "parent": "t-e", "created_at": "2026-10-02T10:00:00Z",
  "dependencies": [{"issue_id": "t-e.1", "depends_on_id": "t-e", "type": "parent-child"}]},
 {"id": "t-mol", "title": "mol", "status": "open", "issue_type": "molecule", "created_at": "2026-10-03T10:00:00Z"},
 {"id": "t-mol.1", "title": "Understand", "status": "closed", "issue_type": "task", "parent": "t-mol", "created_at": "2026-10-03T10:00:00Z"},
 {"id": "t-mol.2", "title": "Implement", "status": "open", "issue_type": "task", "parent": "t-mol", "created_at": "2026-10-03T10:00:00Z",
  "dependencies": [{"issue_id": "t-mol.2", "depends_on_id": "t-mol", "type": "parent-child"}, {"issue_id": "t-mol.2", "depends_on_id": "t-mol.1", "type": "blocks"}]}
]`

// askedWith is the argv lines the stand-in bd was given that start with verb.
func askedWith(t *testing.T, log, verb string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(strings.TrimPrefix(line, "-C "+strings.Fields(line)[1]+" --actor mw@vps "), verb) {
			lines = append(lines, line)
		}
	}
	return lines
}

// A Gateway made to keep a run's reads reads every bead once for the snapshot,
// the bead graph and the stories since, and the notes once for every prefix and
// key asked, and answers a molecule's open steps from the listing (mw-gq6.659).
func TestRunCacheReadsEveryBeadAndEveryNoteOnce(t *testing.T) {
	gateway, log := leaseStandIn(t, map[string]string{
		"list": runListing,
		"kv":   `{"host.laptop.paused": "x", "hands.steps.t-e.1": "a", "host.vps.ticks": "ticks", "schema_version": 1}`,
		"sql":  `[]`,
	}, beads.WithRunCache())
	ctx := context.Background()

	if _, err := gateway.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	graph, err := gateway.BeadGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range graph {
		if b.Type == "molecule" {
			t.Errorf("the graph holds the molecule %s", b.ID)
		}
	}
	if len(graph) != 4 {
		t.Errorf("the graph holds %d beads, want 4 (the epic, its story and the two steps)", len(graph))
	}
	stories, err := gateway.StoriesSince(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 3 {
		t.Errorf("got %d stories since, want 3 (the story and the two steps; no epic, no molecule)", len(stories))
	}
	steps, err := gateway.OpenSteps(ctx, "t-mol")
	if err != nil || len(steps) != 1 || steps[0].ID != "t-mol.2" {
		t.Errorf("the open steps of t-mol are %+v (%v), want only t-mol.2", steps, err)
	}

	shown, err := gateway.ShowBeads(ctx, []string{"t-mol.2", "t-e.1"})
	if err != nil || len(shown) != 2 || shown[0].Story.ID != "t-mol.2" || shown[1].Story.ID != "t-e.1" {
		t.Errorf("ShowBeads = %+v (%v), want t-mol.2 then t-e.1", shown, err)
	}
	if shown[1].Story.ID == "t-e.1" && len(shown[1].Needs) != 0 {
		t.Errorf("the story waits on %v, want nothing (its parent is no wait)", shown[1].Needs)
	}
	epic, err := gateway.ShowEpic(ctx, "t-e")
	if err != nil || epic.ID != "t-e" || len(epic.Stories) != 1 || epic.Stories[0].Story.ID != "t-e.1" {
		t.Errorf("ShowEpic = %+v (%v), want t-e with its one story", epic, err)
	}

	for _, prefix := range []string{"hands.", "host.", "hands.steps."} {
		found, err := gateway.NotesWithPrefix(ctx, prefix)
		if err != nil || len(found) == 0 {
			t.Errorf("notes under %q: %v (%v)", prefix, found, err)
		}
	}
	if note, err := gateway.Note(ctx, "host.vps.ticks"); err != nil || note != "ticks" {
		t.Errorf("Note host.vps.ticks = %q (%v), want ticks", note, err)
	}
	if note, err := gateway.Note(ctx, "host.vps.absent"); err != nil || note != "" {
		t.Errorf("Note host.vps.absent = %q (%v), want nothing", note, err)
	}

	if lists := askedWith(t, log, "list"); len(lists) != 1 {
		t.Errorf("bd list ran %d times, want 1:\n%s", len(lists), strings.Join(lists, "\n"))
	}
	if shows := askedWith(t, log, "show"); len(shows) != 0 {
		t.Errorf("bd show ran for beads the list holds:\n%s", strings.Join(shows, "\n"))
	}
	if kvs := askedWith(t, log, "kv"); len(kvs) != 1 {
		t.Errorf("bd kv ran %d times, want 1:\n%s", len(kvs), strings.Join(kvs, "\n"))
	}
}

// A write through the Gateway ends what it kept, so the next read sees it.
func TestRunCacheForgetsWhatAWriteChanged(t *testing.T) {
	gateway, log := leaseStandIn(t, map[string]string{"list": runListing, "kv": `{}`, "sql": `[]`}, beads.WithRunCache())
	ctx := context.Background()
	if _, err := gateway.BeadGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if err := gateway.SetNote(ctx, "host.vps.x", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.BeadGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if lists := askedWith(t, log, "list"); len(lists) != 2 {
		t.Errorf("bd list ran %d times, want 2 (one before the write, one after)", len(lists))
	}
}

var _ application.TrackerSnapshot = (*beads.Gateway)(nil)
