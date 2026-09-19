package beads

// The fixtures here are `bd --json` output captured verbatim from bd, so that
// the decoding is pinned to the shapes bd really prints. The two commands print
// a bead's dependencies differently — show prints the linked beads, ready
// prints the edges — and both have to be survivable.
//
// Captured from 1.0.4 and re-checked against 1.3.0, which prints these same
// fields plus some the factory ignores (revision, lease_expires_at,
// heartbeat_at, the *_count fields). 1.3.0 stopped inlining a bead's comments
// and dependents without --include-comments / --include-dependents; it still
// inlines dependencies, which is where a story's parent epic is read from.

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

// What `bd list --parent <epic> --all --limit 0 --json` prints for an epic with
// two stories, the second waiting on the first: children newest first, and the
// waiting written as an edge whose type is "blocks". Captured from bd 1.3.0.
const listChildrenJSON = `[
  {
    "id": "t-c3i.2",
    "title": "Story B",
    "acceptance_criteria": "y",
    "status": "deferred",
    "priority": 2,
    "issue_type": "task",
    "created_at": "2026-09-18T22:05:44Z",
    "updated_at": "2026-09-18T22:05:44Z",
    "metadata": {
      "model": "sonnet"
    },
    "dependencies": [
      {
        "issue_id": "t-c3i.2",
        "depends_on_id": "t-c3i",
        "type": "parent-child",
        "created_at": "2026-09-18T22:05:44Z",
        "metadata": "{}"
      },
      {
        "issue_id": "t-c3i.2",
        "depends_on_id": "t-c3i.1",
        "type": "blocks",
        "created_at": "2026-09-18T22:05:44Z",
        "metadata": "{}"
      }
    ],
    "dependency_count": 1,
    "parent": "t-c3i"
  },
  {
    "id": "t-c3i.1",
    "title": "Story A",
    "acceptance_criteria": "x",
    "status": "closed",
    "priority": 2,
    "issue_type": "task",
    "estimated_minutes": 60,
    "created_at": "2026-09-18T22:05:42Z",
    "updated_at": "2026-09-18T22:07:00Z",
    "dependencies": [
      {
        "issue_id": "t-c3i.1",
        "depends_on_id": "t-c3i",
        "type": "parent-child",
        "created_at": "2026-09-18T22:05:42Z",
        "metadata": "{}"
      }
    ],
    "dependency_count": 0,
    "parent": "t-c3i"
  }
]`

// What `bd show` prints for that same second story: the beads it waits on are
// whole beads with a "dependency_type", not edges. Captured from bd 1.3.0,
// trimmed to the fields the factory reads.
const showBlockedStoryJSON = `[
  {
    "id": "t-c3i.2",
    "title": "Story B",
    "status": "deferred",
    "issue_type": "task",
    "created_at": "2026-09-18T22:05:44Z",
    "metadata": { "model": "sonnet" },
    "dependencies": [
      {
        "id": "t-c3i",
        "title": "Epic one",
        "issue_type": "epic",
        "metadata": { "rig": "millwright" },
        "dependency_type": "parent-child"
      },
      {
        "id": "t-c3i.1",
        "title": "Story A",
        "status": "closed",
        "issue_type": "task",
        "dependency_type": "blocks"
      },
      {
        "id": "t-c3i.3",
        "title": "Story C",
        "status": "open",
        "issue_type": "task",
        "dependency_type": "blocks"
      }
    ],
    "parent": "t-c3i"
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

func TestWhatAStoryWaitsOnIsReadFromEitherDependencyShape(t *testing.T) {
	listed, err := decodeBeads([]byte(listChildrenJSON))
	if err != nil {
		t.Fatalf("decoding a listing of an epic's children: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected two beads, got %d", len(listed))
	}
	for _, b := range listed {
		got := b.needs()
		switch b.ID {
		case "t-c3i.2":
			if len(got) != 1 || got[0] != "t-c3i.1" {
				t.Errorf("expected %s to wait on t-c3i.1, got %v", b.ID, got)
			}
		case "t-c3i.1":
			if len(got) != 0 {
				t.Errorf("expected %s to wait on nothing, got %v", b.ID, got)
			}
		}
	}

	// A shown story carries its blockers whole, so a wait on work that is
	// already closed can be — and is — left out: it waits on t-c3i.3, not on
	// the finished t-c3i.1 and not on the epic it hangs from.
	shown, err := decodeBeads([]byte(showBlockedStoryJSON))
	if err != nil {
		t.Fatalf("decoding a shown story: %v", err)
	}
	if got := shown[0].needs(); len(got) != 1 || got[0] != "t-c3i.3" {
		t.Errorf("expected the shown story to wait on t-c3i.3 alone, got %v", got)
	}
	// And the story detail carries it, which is what a tree of an epic reads.
	if got := shown[0].detail(domain.Path{}).Needs; len(got) != 1 || got[0] != "t-c3i.3" {
		t.Errorf("expected the story detail to carry what it waits on, got %v", got)
	}
}

func TestChildrenComeBackInTheOrderTheyWereFiled(t *testing.T) {
	listed, err := decodeBeads([]byte(listChildrenJSON))
	if err != nil {
		t.Fatalf("decoding a listing of an epic's children: %v", err)
	}
	ordered := inFiledOrder(listed)
	if ordered[0].ID != "t-c3i.1" || ordered[1].ID != "t-c3i.2" {
		t.Fatalf("expected the oldest child first, got %s then %s", ordered[0].ID, ordered[1].ID)
	}

	// Beads bd stamped in the same second fall back to id order, counting the
	// parts between the dots as the numbers they are.
	same := []bead{
		{ID: "t-a.10", CreatedAt: "2026-09-18T22:05:44Z"},
		{ID: "t-a.9", CreatedAt: "2026-09-18T22:05:44Z"},
		{ID: "t-a.2", CreatedAt: "2026-09-18T22:05:44Z"},
	}
	want := []string{"t-a.2", "t-a.9", "t-a.10"}
	for i, b := range inFiledOrder(same) {
		if b.ID != want[i] {
			t.Fatalf("expected %v, got %s in place %d", want, b.ID, i)
		}
	}
}

func TestPathMetadataIgnoresValuesThatAreNotStrings(t *testing.T) {
	b := bead{Metadata: map[string]any{"rig": "millwright", "priority": 2.0}}
	if got := b.pathMetadata(); len(got) != 1 || got["rig"] != "millwright" {
		t.Fatalf("expected only the string metadata, got %v", got)
	}
}
