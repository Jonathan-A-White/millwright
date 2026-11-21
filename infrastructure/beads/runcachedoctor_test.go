package beads_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// asking counts the queries the server was sent that contain every one of the
// words.
func (s *keptServer) asking(words ...string) int {
	n := 0
	for _, q := range s.asked() {
		l := strings.ToLower(q)
		all := true
		for _, w := range words {
			all = all && strings.Contains(l, w)
		}
		if all {
			n++
		}
	}
	return n
}

// mw-gq6.1323: the doctor's dampers pass read the bead graph twice, once for the
// grillings and once for the stuck check, each three queries and 2.5 s on a
// loaded laptop. Under a run cache the second read is the first's answer.
func TestUnderARunCacheTheBeadGraphIsReadOnce(t *testing.T) {
	s := &keptServer{}
	s.graphed.Store(true)
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)), beads.WithRunCache())

	first, err := g.BeadGraph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.BeadGraph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.asking("select i.id, i.title, i.status, i.issue_type, i.priority, i.created_at from issues i"); got != 1 {
		t.Errorf("the bead graph's beads were read %d times in one run, want once", got)
	}
	if got := s.asking("from dependencies d"); got != 1 {
		t.Errorf("the bead graph's edges were read %d times in one run, want once", got)
	}
	if len(first) == 0 || len(first) != len(second) {
		t.Errorf("the second read of the graph holds %d beads, the first %d", len(second), len(first))
	}
	// A caller that reorders what it was given must not change the next one's.
	first[0].ID = "changed"
	third, _ := g.BeadGraph(context.Background())
	if third[0].ID == "changed" {
		t.Errorf("the cache handed out its own slice: a caller's change reached the next read")
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Errorf("expected no bd while the connection answers, got %d", got)
	}
}

// Without a run cache each read of the graph asks the server, as it did.
func TestWithoutARunCacheTheBeadGraphIsReadEachTime(t *testing.T) {
	s := &keptServer{}
	s.graphed.Store(true)
	program, _ := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))

	for range 2 {
		if _, err := g.BeadGraph(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.asking("select i.id, i.title, i.status, i.issue_type, i.priority, i.created_at from issues i"); got != 2 {
		t.Errorf("without a run cache the graph's beads were read %d times for two reads, want 2", got)
	}
}

// mw-gq6.1323: every NotesWithPrefix over the kept connection read the whole
// table of notes (1.2 MB, 0.3 s at load) and kept the prefix's keys, and the
// dampers pass asks for five prefixes. Under a run cache the table is read once
// and each prefix is answered from it.
func TestUnderARunCacheNotesOfSeveralPrefixesAreReadFromOneTable(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)), beads.WithRunCache())

	some, err := g.NotesWithPrefix(context.Background(), "some.")
	if err != nil || len(some) != 1 || some["some.note"] != "its value" {
		t.Fatalf("expected the one note under some. (a null value is no note), got %v, %v", some, err)
	}
	other, err := g.NotesWithPrefix(context.Background(), "other.")
	if err != nil || len(other) != 1 || other["other.note"] != "x" {
		t.Fatalf("expected the one note under other., got %v, %v", other, err)
	}
	none, err := g.NotesWithPrefix(context.Background(), "no.such.")
	if err != nil || len(none) != 0 {
		t.Fatalf("expected no note under no.such., got %v, %v", none, err)
	}
	some["some.note"] = "changed"
	again, _ := g.NotesWithPrefix(context.Background(), "some.")
	if again["some.note"] != "its value" {
		t.Errorf("the cache handed out its own map: a caller's change reached the next read")
	}
	if got := s.asking("`value` as v from config"); got != 1 {
		t.Errorf("the table of notes was read %d times for four reads of prefixes, want once", got)
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Errorf("expected no bd while the connection answers, got %d", got)
	}
}

// mw-gq6.1323: the proposals of mw verify are looked for in the comments that
// open one of its words, read by one query for all the ids, oldest first, and
// not in every comment of every bead.
func TestProposalCommentsAreReadByOneNarrowQueryOverTheKeptConnection(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))

	said, err := g.StoriesProposalComments(context.Background(), []string{"t-9", "t-8"})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.asking("regexp_like(c.text"); got != 1 {
		t.Errorf("expected one narrow query for both ids, got %d", got)
	}
	if got := s.asking("from comments c where c.issue_id in"); got != 1 {
		t.Errorf("expected the one query to be the only one naming comments, got %d", got)
	}
	if len(said) != 1 || len(said["t-9"]) != 2 {
		t.Fatalf("expected t-9's two comments and no entry for t-8, got %v", said)
	}
	if !strings.HasPrefix(said["t-9"][0].Text, "VERIFIED") || !strings.HasPrefix(said["t-9"][1].Text, "PROPOSED") {
		t.Errorf("expected the comments oldest first, got %q then %q", said["t-9"][0].Text, said["t-9"][1].Text)
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Errorf("expected no bd while the connection answers, got %d", got)
	}
}

// mw-gq6.1323: the starved-rig check read each starved rig's newest closed
// stories by a bd list, 6 to 9 s each at load on a host that reaches its beads
// by the network, 59 s for the first of five once, so the check was cut at its
// budget and the dampers line read all five again. One query over the kept
// connection reads a rig's, and a run cache keeps it.
func TestAClosedRigIsReadOverTheKeptConnectionWithoutStartingABd(t *testing.T) {
	s := &keptServer{}
	program, asked := standInSnapshotProgram(t, bdListing, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)), beads.WithRunCache())

	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	closed, err := g.ClosedStoriesOfRig(context.Background(), "millwright", since, 200)
	if err != nil {
		t.Fatal(err)
	}
	if got := bdsStarted(t, asked); got != 0 {
		t.Fatalf("expected no bd started while the connection answers, got %d", got)
	}
	if len(closed) != 2 || closed[0].ID != "t-9" || closed[0].Rig != "millwright" || closed[0].Host != "laptop" ||
		closed[0].CloseReason != "Done" || !closed[0].ClosedAt.Equal(time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)) || closed[1].Host != "cloud5" {
		t.Errorf("the rig's closed stories read wrongly: %+v", closed)
	}
	if _, err := g.ClosedStoriesOfRig(context.Background(), "millwright", since.Add(time.Minute), 200); err != nil {
		t.Fatal(err)
	}
	if got := s.asking("json_extract(i.metadata"); got != 1 {
		t.Errorf("expected the rig read once in the run, got %d", got)
	}
}

// With the connection down a rig's closed stories are read by bd, as they were.
func TestAClosedRigIsReadByBdWhenTheKeptConnectionFails(t *testing.T) {
	s := &keptServer{}
	s.fail.Store(true)
	program, asked := standInSnapshotProgram(t, `[{"id":"t-9","title":"Landed","status":"closed","issue_type":"task","close_reason":"Done","metadata":{"rig":"millwright","host":"laptop"}}]`, bdTable)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))

	closed, err := g.ClosedStoriesOfRig(context.Background(), "millwright", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 200)
	if err != nil || len(closed) != 1 || closed[0].Host != "laptop" {
		t.Fatalf("expected the closed story from bd, got %v, %v", closed, err)
	}
	if got := bdsStarted(t, asked); got != 1 {
		t.Errorf("expected one bd, got %d", got)
	}
}
