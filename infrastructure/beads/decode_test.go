package beads

// The fixtures here are `bd --json` output captured verbatim from bd 1.0.4, so
// that the decoding is pinned to the shapes bd really prints. The two commands
// print a bead's dependencies differently — show prints the linked beads, ready
// prints the edges — and both have to be survivable.

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

const showStoryJSON = `[
  {
    "id": "t-42s.1",
    "title": "A story",
    "acceptance_criteria": "it works",
    "status": "open",
    "priority": 2,
    "issue_type": "task",
    "estimated_minutes": 45,
    "created_at": "2026-09-18T18:08:08Z",
    "created_by": "root",
    "updated_at": "2026-09-18T18:08:08Z",
    "metadata": {
      "model": "haiku"
    },
    "dependencies": [
      {
        "id": "t-42s",
        "title": "An epic",
        "status": "open",
        "priority": 2,
        "issue_type": "epic",
        "metadata": {
          "rig": "millwright",
          "host": "vps",
          "model": "opus",
          "branch": "main",
          "effort": "high",
          "formula": "tdd-feature",
          "harness": "claude"
        },
        "dependency_type": "parent-child"
      }
    ],
    "parent": "t-42s"
  }
]`

const readyStoryJSON = `[
  {
    "id": "t-42s.1",
    "title": "A story",
    "acceptance_criteria": "it works",
    "status": "open",
    "priority": 2,
    "issue_type": "task",
    "estimated_minutes": 45,
    "metadata": {
      "host": "vps",
      "model": "haiku"
    },
    "dependencies": [
      {
        "issue_id": "t-42s.1",
        "depends_on_id": "t-42s",
        "type": "parent-child",
        "created_at": "2026-09-18T18:08:07Z",
        "created_by": "root",
        "metadata": "{}"
      }
    ],
    "dependency_count": 0,
    "parent": "t-42s"
  }
]`

const errorJSON = `{
  "error": "--days must be at least 1",
  "schema_version": 1
}`

func TestDecodeBeadsReadsAStoryAndItsEmbeddedEpic(t *testing.T) {
	got, err := decodeBeads([]byte(showStoryJSON))
	if err != nil {
		t.Fatalf("decoding a shown story: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one bead, got %d", len(got))
	}

	b := got[0]
	if b.ID != "t-42s.1" || b.Title != "A story" || b.Parent != "t-42s" {
		t.Errorf("expected the story's id, title and parent, got %+v", b)
	}
	if b.Acceptance != "it works" || b.EstimatedMinutes != 45 {
		t.Errorf("expected the acceptance criteria and estimate, got %q and %d", b.Acceptance, b.EstimatedMinutes)
	}
	if got := domain.PathFromMetadata(b.pathMetadata()); got != (domain.Path{Model: domain.ModelHaiku}) {
		t.Errorf("expected only the story's own override, got %+v", got)
	}

	defaults, ok := b.parentPath()
	if !ok {
		t.Fatal("expected the epic embedded in the story's dependencies to be found")
	}
	want := domain.Path{
		Rig:     "millwright",
		Branch:  "main",
		Harness: domain.HarnessClaude,
		Model:   domain.ModelOpus,
		Effort:  domain.EffortHigh,
		Formula: "tdd-feature",
		Host:    "vps",
	}
	if defaults != want {
		t.Fatalf("expected the epic's default path %+v, got %+v", want, defaults)
	}
}

func TestDecodeBeadsSurvivesTheDependencyEdgeShape(t *testing.T) {
	got, err := decodeBeads([]byte(readyStoryJSON))
	if err != nil {
		t.Fatalf("decoding a ready story: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one bead, got %d", len(got))
	}
	if _, ok := got[0].parentPath(); ok {
		t.Error("expected no epic path from an edge, which carries none")
	}
	if got := domain.PathFromMetadata(got[0].pathMetadata()); got.Host != "vps" {
		t.Errorf("expected the story's own metadata to be read, got %+v", got)
	}
}

func TestDecodeBeadsReportsAnErrorObject(t *testing.T) {
	_, err := decodeBeads([]byte(errorJSON))
	if err == nil {
		t.Fatal("expected bd's error object to be reported as an error")
	}
	if err.Error() != "--days must be at least 1" {
		t.Fatalf("expected bd's own message, got %q", err.Error())
	}
}

func TestDecodeBeadsAcceptsAnEmptyList(t *testing.T) {
	got, err := decodeBeads([]byte("[]\n"))
	if err != nil {
		t.Fatalf("decoding an empty list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no beads, got %d", len(got))
	}
}

func TestPathMetadataIgnoresValuesThatAreNotStrings(t *testing.T) {
	b := bead{Metadata: map[string]any{"rig": "millwright", "priority": 2.0}}
	if got := b.pathMetadata(); len(got) != 1 || got["rig"] != "millwright" {
		t.Fatalf("expected only the string metadata, got %v", got)
	}
}
