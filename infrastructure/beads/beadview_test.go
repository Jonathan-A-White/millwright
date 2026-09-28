package beads_test

import (
	"context"
	"os"
	"strings"
	"testing"
)

// showTwoBeadsJSON is what bd show prints for a story and its epic asked for
// together: the story embeds its parent and a closed and an open blocker as
// whole linked beads; the epic is filed under a map.
const showTwoBeadsJSON = `[
  {"id": "t-e.2", "title": "A bug", "status": "open", "priority": 1, "issue_type": "bug",
   "labels": ["hitl"], "created_at": "2026-09-27T09:00:00Z", "updated_at": "2026-09-28T10:00:00Z",
   "comment_count": 3, "metadata": {"model": "sonnet"}, "parent": "t-e",
   "dependencies": [
     {"id": "t-e", "title": "An epic", "status": "open", "issue_type": "epic",
      "metadata": {"rig": "postern", "branch": "main"}, "dependency_type": "parent-child"},
     {"id": "t-e.1", "title": "Done", "status": "closed", "issue_type": "task", "dependency_type": "blocks"},
     {"id": "t-e.3", "title": "Not yet", "status": "open", "issue_type": "task", "dependency_type": "blocks"}
   ]},
  {"id": "t-e", "title": "An epic", "status": "deferred", "priority": 2, "issue_type": "epic",
   "labels": ["wayfinder:map"], "parent": "t-m", "metadata": {"rig": "postern", "branch": "main"}}
]`

// ShowBeads reads several beads in one bd show, whatever their type, in the
// order asked: a story with its parent's Path overlaid from the copy bd
// embeds, its waits narrowed to the unfinished, and an epic with its own.
func TestShowBeadsReadsSeveralBeadsInOneShow(t *testing.T) {
	gateway, log := leaseStandIn(t, map[string]string{"show": showTwoBeadsJSON})

	found, err := gateway.ShowBeads(context.Background(), []string{"t-e", "t-e.2"})
	if err != nil {
		t.Fatalf("showing the beads: %v", err)
	}
	if len(found) != 2 || found[0].Story.ID != "t-e" || found[1].Story.ID != "t-e.2" {
		t.Fatalf("expected t-e then t-e.2, got %+v", found)
	}
	epic, story := found[0], found[1]
	if !epic.IsEpic || epic.Type != "epic" || epic.EpicID != "t-m" || epic.Status != "deferred" || len(epic.Labels) != 1 {
		t.Errorf("expected the held epic under the map, got %+v", epic)
	}
	if story.Type != "bug" || story.CommentCount != 3 || story.EpicID != "t-e" || story.Priority != 1 {
		t.Errorf("expected the bug with three comments under t-e, got %+v", story)
	}
	if merged := story.Merged(); merged.Rig != "postern" || merged.Branch != "main" || merged.Model != "sonnet" {
		t.Errorf("expected the story's Path overlaid on its epic's, got %+v", merged)
	}
	if len(story.Needs) != 1 || story.Needs[0] != "t-e.3" {
		t.Errorf("expected t-e.2 to wait on t-e.3 only, got %v", story.Needs)
	}
	asked, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(asked)), "\n"); len(lines) != 1 || !strings.Contains(lines[0], "show t-e t-e.2 --json") {
		t.Fatalf("expected one bd show of both ids, got %q", asked)
	}
}

// A bead bd says it has no record of is simply absent, never a failure.
func TestShowBeadsLeavesOutABeadBdDoesNotKnow(t *testing.T) {
	gateway, _ := leaseStandIn(t, map[string]string{"show": `fail:Error: no issue found matching "t-nope"`})

	found, err := gateway.ShowBeads(context.Background(), []string{"t-nope"})
	if err != nil {
		t.Fatalf("expected no error for an unknown bead, got %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected nothing found, got %+v", found)
	}
}

// ShowEpic carries the epic's own bead: its parent, labels and times, for a
// reader that shows the epic as one bead among the rest.
func TestShowEpicCarriesTheEpicsOwnBeadFromTheSameShow(t *testing.T) {
	gateway, _ := leaseStandIn(t, map[string]string{
		"show": `[{"id": "t-e", "title": "An epic", "status": "open", "issue_type": "epic", "parent": "t-m",
		          "labels": ["wayfinder:map"], "comment_count": 2, "created_at": "2026-09-20T08:00:00Z"}]`,
		"list": `[]`,
	})

	epic, err := gateway.ShowEpic(context.Background(), "t-e")
	if err != nil {
		t.Fatalf("showing the epic: %v", err)
	}
	if epic.Bead.EpicID != "t-m" || !epic.Bead.IsEpic || epic.Bead.CommentCount != 2 || epic.Bead.Created.IsZero() || len(epic.Bead.Labels) != 1 {
		t.Fatalf("expected the epic's own bead under t-m, got %+v", epic.Bead)
	}
}

func TestSetStoryPriorityAndHoldStoryAreOneBdUpdateEach(t *testing.T) {
	gateway, log := recorder(t, nil...)
	ctx := context.Background()

	if err := gateway.SetStoryPriority(ctx, "t-1", 0); err != nil {
		t.Fatalf("setting the priority: %v", err)
	}
	if err := gateway.HoldStory(ctx, "t-1"); err != nil {
		t.Fatalf("holding: %v", err)
	}
	if err := gateway.SetStoryPriority(ctx, "t-1", 7); err == nil {
		t.Fatal("expected priority 7 to be refused before bd is asked")
	}
	asked, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(asked)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two bd calls, got %q", asked)
	}
	if !strings.HasSuffix(lines[0], "update t-1 --priority 0") {
		t.Errorf("expected bd update t-1 --priority 0, got %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "update t-1 --status deferred") {
		t.Errorf("expected bd update t-1 --status deferred, got %q", lines[1])
	}
}

// NotesWithPrefix reads every note in one bd kv list; a bd that prints
// nothing lists no notes rather than failing the read.
func TestNotesWithPrefixIsOneListAndReadsNothingPrintedAsNoNotes(t *testing.T) {
	gateway, log := leaseStandIn(t, map[string]string{
		"list": `{"host.vps.last_sync": "2026-09-28T11:00:00Z", "postern.question.t-1": "{}", "schema_version": 3}`,
	})
	found, err := gateway.NotesWithPrefix(context.Background(), "host.")
	if err != nil {
		t.Fatalf("listing the notes: %v", err)
	}
	if len(found) != 1 || found["host.vps.last_sync"] != "2026-09-28T11:00:00Z" {
		t.Fatalf("expected only the host note, got %v", found)
	}
	asked, _ := os.ReadFile(log)
	if !strings.Contains(string(asked), "kv list --json") {
		t.Fatalf("expected one bd kv list --json, got %q", asked)
	}

	silent, _ := recorder(t)
	if found, err := silent.NotesWithPrefix(context.Background(), ""); err != nil || len(found) != 0 {
		t.Fatalf("expected no notes and no error from a bd that printed nothing, got %v: %v", found, err)
	}
}
