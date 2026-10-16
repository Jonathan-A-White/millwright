package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// Over a link with a 70 ms round trip every bd costs seconds, whatever it
// reads, so `mw status` is as slow as the number of bd processes it starts
// (mw-gq6.356). These cases hold the count of the reads status leans on to a
// number that does not grow with the stories or epics read.

// roundsDB is what a stand-in bd holds, as the JSON bd prints it: the rows of
// `ready`, of `list` (the claimed) and of `blocked`, the bead `show` prints for
// an id, and the rows `list --parent` prints for an epic.
type roundsDB struct {
	ready, claimed, blocked string
	shown                   map[string]string
	children                map[string]string
	unlisted                []string // beads `show` knows and `list --id` does not print
}

// roundsStandIn is a stand-in bd over a roundsDB. `show` takes any number of
// ids and prints the bead for each, as bd does, and refuses when one is unknown.
// Every argv is written down, and every `list --parent` holds for a moment and
// writes "overlap" to the file returned if another bd was inside one at the time;
// every `show` does the same, and writes to that file's name with ".show" after it.
func roundsStandIn(t *testing.T, db roundsDB, opts ...beads.Option) (*beads.Gateway, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "asked.log")
	overlap := filepath.Join(dir, "overlap")

	var known, arms, kids, hidden strings.Builder
	for _, id := range db.unlisted {
		fmt.Fprintf(&hidden, "            %s) continue ;;\n", id)
	}
	for id, bead := range db.shown {
		fmt.Fprintf(&known, "        %s) ;;\n", id)
		fmt.Fprintf(&arms, "        %s) bead='%s' ;;\n", id, bead)
	}
	for id, rows := range db.children {
		fmt.Fprintf(&kids, "      %s) printf '%%s' '%s' ;;\n", id, rows)
	}
	// `show` prints the bead for each id as one array; the script builds it
	// id by id, so a batch of ids answers the same as the ids one at a time.
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %[1]s
shift 2
case "$1" in
  --actor) shift 2 ;;
esac
case "$1" in
  ready) printf '%%s' '%[2]s' ;;
  blocked) printf '%%s' '%[3]s' ;;
  list)
    case "$2" in
      --parent)
        if mkdir %[5]s.lock 2>/dev/null; then
          sleep 0.3
          rmdir %[5]s.lock
        else
          echo overlap >> %[5]s
          sleep 0.3
        fi
        case "$3" in
%[6]s          *) printf '[]' ;;
        esac ;;
      --id)
        first=1
        printf '['
        for id in $(echo "$3" | tr ',' ' '); do
          case "$id" in
%[9]s          esac
          case "$id" in
%[7]s            *) continue ;;
          esac
          case "$id" in
%[8]s          esac
          [ $first = 1 ] || printf ','
          first=0
          printf '%%s' "$bead"
        done
        printf ']' ;;
      *) printf '%%s' '%[4]s' ;;
    esac ;;
  show)
    shift
    if mkdir %[5]s.show.lock 2>/dev/null; then
      sleep 0.3
      rmdir %[5]s.show.lock
    else
      echo overlap >> %[5]s.show
      sleep 0.3
    fi
    out=""
    for id in "$@"; do
      case "$id" in
        --json|--include-comments) continue ;;
%[7]s        *) echo "no issues found matching $id" >&2; exit 1 ;;
      esac
    done
    first=1
    printf '['
    for id in "$@"; do
      case "$id" in
        --json|--include-comments) continue ;;
%[8]s      esac
      [ $first = 1 ] || printf ','
      first=0
      printf '%%s' "$bead"
    done
    printf ']' ;;
  *) exit 1 ;;
esac
`, log, db.ready, db.blocked, db.claimed, overlap, kids.String(), known.String(), arms.String(), hidden.String())
	path := filepath.Join(dir, "bd-rounds")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(t.TempDir(), append([]beads.Option{beads.WithProgram(path)}, opts...)...), log, overlap
}

func bdCalls(t *testing.T, log string) int {
	t.Helper()
	return len(asked(t, log))
}

// What is ready and what is claimed name an epic only by its id, so each epic
// is read for its defaults. Read one at a time, a database of many epics costs
// a bd each; the epics are read in one.
func TestWorkInHandReadsAllTheEpicsInOneBd(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	gateway, log, _ := roundsStandIn(t, roundsDB{
		ready:   "[" + story("mw-a.1", "mw-e1") + "," + story("mw-b.1", "mw-e2") + "," + story("mw-c.1", "mw-e3") + "]",
		claimed: "[" + story("mw-d.1", "mw-e4") + "," + story("mw-a.2", "mw-e1") + "]",
		shown: map[string]string{
			"mw-e1": epic("mw-e1", "laptop"), "mw-e2": epic("mw-e2", "laptop"),
			"mw-e3": epic("mw-e3", "laptop"), "mw-e4": epic("mw-e4", "laptop"),
		},
	})

	inHand, err := gateway.WorkInHand(context.Background())
	if err != nil {
		t.Fatalf("reading what is in hand: %v", err)
	}
	if len(inHand.Ready) != 3 || len(inHand.Running) != 2 {
		t.Fatalf("expected 3 ready and 2 running, got %d and %d", len(inHand.Ready), len(inHand.Running))
	}
	for _, detail := range append(inHand.Ready, inHand.Running...) {
		if detail.Merged().Host != "laptop" {
			t.Errorf("expected %s to inherit its epic's host laptop, got %q", detail.Story.ID, detail.Merged().Host)
		}
	}
	if got := bdCalls(t, log); got > 4 {
		t.Errorf("expected ready, the two lists and one show of the epics (4 bd calls), got %d: %v", got, asked(t, log))
	}
}

// The epics a story's defaults come from are read for their own fields, and a
// bd list prints those for any number of ids in one query where bd show makes a
// round trip an id (2 to 2.5 s each over wg0, mw-gq6.358): against a server the
// epics of what is in hand are one bd list, and no bd show.
func TestWorkInHandReadsTheEpicsWithOneListAgainstAServer(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.88.0.2")
	gateway, log, _ := roundsStandIn(t, roundsDB{
		ready:   "[" + story("mw-a.1", "mw-e1") + "," + story("mw-b.1", "mw-e2") + "]",
		claimed: "[" + story("mw-d.1", "mw-e4") + "," + story("mw-a.2", "mw-e1") + "]",
		shown: map[string]string{
			"mw-e1": epic("mw-e1", "laptop"), "mw-e2": epic("mw-e2", "laptop"), "mw-e4": epic("mw-e4", "laptop"),
		},
	})

	inHand, err := gateway.WorkInHand(context.Background())
	if err != nil {
		t.Fatalf("reading what is in hand: %v", err)
	}
	for _, detail := range append(inHand.Ready, inHand.Running...) {
		if detail.Merged().Host != "laptop" {
			t.Errorf("expected %s to inherit its epic's host laptop, got %q", detail.Story.ID, detail.Merged().Host)
		}
	}
	if shows := showsAsked(t, log); len(shows) != 0 {
		t.Errorf("expected no bd show for the epics, got %v", shows)
	}
	if got := bdCalls(t, log); got != 4 {
		t.Errorf("expected ready, the two lists and one list of the epics (4 bd calls), got %d: %v", got, asked(t, log))
	}
}

// A bead bd list does not print (a type it hides) is read with bd show, so the
// shortcut never loses an epic.
func TestWorkInHandReadsAnEpicBdListHidesWithShow(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.88.0.2")
	gateway, log, _ := roundsStandIn(t, roundsDB{
		ready:    "[" + story("mw-a.1", "mw-e1") + "," + story("mw-b.1", "mw-e2") + "]",
		shown:    map[string]string{"mw-e1": epic("mw-e1", "laptop"), "mw-e2": epic("mw-e2", "laptop")},
		unlisted: []string{"mw-e2"},
	})

	inHand, err := gateway.WorkInHand(context.Background())
	if err != nil {
		t.Fatalf("reading what is in hand: %v", err)
	}
	for _, detail := range inHand.Ready {
		if detail.Merged().Host != "laptop" {
			t.Errorf("expected %s to inherit its epic's host laptop, got %q", detail.Story.ID, detail.Merged().Host)
		}
	}
	if shows := showsAsked(t, log); len(shows) != 1 || !strings.HasSuffix(shows[0], "show mw-e2 --json") {
		t.Errorf("expected one bd show, of mw-e2, got %v", shows)
	}
}

// bd blocked prints rows with no parent, so each blocked story is read back,
// and its epic after it: the stories in one bd, the epics in another.
func TestBlockedForHostReadsTheStoriesAndTheEpicsInOneBdEach(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	gateway, log, _ := roundsStandIn(t, roundsDB{
		blocked: "[" + row("mw-a.1") + "," + row("mw-b.1") + "," + row("mw-c.1") + "," + row("mw-d.1") + "]",
		shown: map[string]string{
			"mw-a.1": story("mw-a.1", "mw-e1"), "mw-b.1": story("mw-b.1", "mw-e2"),
			"mw-c.1": story("mw-c.1", "mw-e1"), "mw-d.1": story("mw-d.1", "mw-e3"),
			"mw-e1": epic("mw-e1", "laptop"), "mw-e2": epic("mw-e2", "laptop"), "mw-e3": epic("mw-e3", "vps"),
		},
	})

	blocked, err := gateway.BlockedForHost(context.Background(), "laptop")
	if err != nil {
		t.Fatalf("reading what is blocked: %v", err)
	}
	if len(blocked) != 3 {
		t.Fatalf("expected 3 stories blocked on laptop, got %+v", blocked)
	}
	if got := bdCalls(t, log); got > 3 {
		t.Errorf("expected blocked, one show of the stories and one of the epics (3 bd calls), got %d: %v", got, asked(t, log))
	}
}

// Each epic's stories take a bd list of their own, one epic at a time, so the
// epics of a busy database were a bd apiece in a row. Against a server, which
// takes reads side by side, they are read together; against a database in the
// vault, which takes one bd at a time, they are not.
func epicsWithChildren() roundsDB {
	db := roundsDB{shown: map[string]string{}, children: map[string]string{}}
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("mw-e%d", i)
		db.shown[id] = epic(id, "laptop")
		db.children[id] = "[" + story(id+".1", id) + "]"
	}
	return db
}

func TestShowEpicsReadsTheStoriesOfSeveralEpicsAtOnceAgainstAServer(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.88.0.2")
	gateway, _, overlap := roundsStandIn(t, epicsWithChildren())

	epics, err := gateway.ShowEpics(context.Background(), []string{"mw-e1", "mw-e2", "mw-e3", "mw-e4", "mw-e5", "mw-e6"})
	if err != nil {
		t.Fatalf("reading the epics: %v", err)
	}
	for i, epic := range epics {
		if want := fmt.Sprintf("mw-e%d", i+1); epic.ID != want || len(epic.Stories) != 1 {
			t.Errorf("expected epic %d to be %s with one story, got %+v", i, want, epic)
		}
	}
	if _, err := os.Stat(overlap); err != nil {
		t.Error("expected two of the lists of children to be under way at once against a server")
	}
}

func TestShowEpicsReadsOneBdAtATimeAgainstTheVaultsOwnDatabase(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	gateway, _, overlap := roundsStandIn(t, epicsWithChildren())

	if _, err := gateway.ShowEpics(context.Background(), []string{"mw-e1", "mw-e2", "mw-e3", "mw-e4", "mw-e5", "mw-e6"}); err != nil {
		t.Fatalf("reading the epics: %v", err)
	}
	if _, err := os.Stat(overlap); err == nil {
		t.Error("expected no two bd to run at once against the vault's own database")
	}
}

// `bd show` of several ids is one round trip to the server per id, one after
// another inside the one process, so a batch of 46 beads was minutes over wg0
// (2 to 2.5 s an id, measured on the desktop, mw-gq6.358). Sixteen bd, one id
// each, side by side take as long as one; so against a server a batch of ids
// is read as a bd apiece, together.
func manyBeads(n int) (roundsDB, []string) {
	db := roundsDB{shown: map[string]string{}}
	var ids []string
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("mw-e%d", i)
		db.shown[id] = epic(id, "laptop")
		ids = append(ids, id)
	}
	return db, ids
}

func showsAsked(t *testing.T, log string) []string {
	t.Helper()
	var shows []string
	for _, line := range asked(t, log) {
		if strings.Contains(line, " show ") {
			shows = append(shows, line)
		}
	}
	return shows
}

func TestShowBeadsReadsABatchAsABdApieceTogetherAgainstAServer(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.88.0.2")
	db, ids := manyBeads(6)
	gateway, log, overlap := roundsStandIn(t, db)

	found, err := gateway.ShowBeads(context.Background(), ids)
	if err != nil {
		t.Fatalf("reading the beads: %v", err)
	}
	if len(found) != len(ids) {
		t.Fatalf("expected %d beads, got %d", len(ids), len(found))
	}
	for i, one := range found {
		if one.Story.ID != ids[i] {
			t.Errorf("expected bead %d to be %s, in the order asked, got %s", i, ids[i], one.Story.ID)
		}
	}
	if got := len(showsAsked(t, log)); got != len(ids) {
		t.Errorf("expected a bd show for each of the %d beads, got %d: %v", len(ids), got, asked(t, log))
	}
	if _, err := os.Stat(overlap + ".show"); err != nil {
		t.Error("expected two of the bd show to be under way at once against a server")
	}
}

func TestShowBeadsReadsABatchInOneBdAgainstTheVaultsOwnDatabase(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	db, ids := manyBeads(6)
	gateway, log, _ := roundsStandIn(t, db)

	if _, err := gateway.ShowBeads(context.Background(), ids); err != nil {
		t.Fatalf("reading the beads: %v", err)
	}
	if got := len(showsAsked(t, log)); got != 1 {
		t.Errorf("expected one bd show for the batch against the vault's own database, got %d: %v", got, asked(t, log))
	}
}

func TestShowBeadsLeavesOutAnIdBdHasNoRecordOfAgainstAServer(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.88.0.2")
	db, ids := manyBeads(3)
	gateway, _, _ := roundsStandIn(t, db)

	found, err := gateway.ShowBeads(context.Background(), append([]string{"mw-nobody"}, ids...))
	if err != nil {
		t.Fatalf("reading the beads: %v", err)
	}
	if len(found) != len(ids) {
		t.Errorf("expected the %d beads there are, got %+v", len(ids), found)
	}
}

// mw-gq6.452: against a server bd show reads its ids one round trip after
// another inside one process (20 ids took 31 s on cloud1), and a bd show apiece
// cost mw status 865 processes and two minutes; one bd sql reads the comments of
// any number of beads, oldest first: a bead with none is there with none, and
// an id bd has no record of is left out, as bd show leaves it out.
func TestStoriesCommentsReadsABatchInOneSqlAgainstAServer(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.88.0.2")
	table := `[
	 {"k":"b","id":"mw-a.1","a":"mw-a.1","b":"","t":"","at":null},
	 {"k":"b","id":"mw-c.3","a":"mw-c.3","b":"","t":"","at":null},
	 {"k":"b","id":"mw-o'b.2","a":"mw-o'b.2","b":"","t":"","at":null},
	 {"k":"c","id":"c2","a":"mw-a.1","b":"mayor","t":"second","at":"2026-10-10 21:00:00"},
	 {"k":"c","id":"c1","a":"mw-a.1","b":"builder","t":"first","at":"2026-10-10 20:00:00"},
	 {"k":"c","id":"c3","a":"mw-o'b.2","b":"mayor","t":"quoted","at":"2026-10-11T08:00:00Z"}
	]`
	gateway, log := standInSnapshot(t, "[]", table)

	ids := []string{"mw-a.1", "mw-o'b.2", "mw-c.3", "mw-nobody"}
	said, err := gateway.StoriesComments(context.Background(), ids)
	if err != nil {
		t.Fatalf("reading the comments: %v", err)
	}
	if len(said["mw-a.1"]) != 2 || said["mw-a.1"][0].Text != "first" || said["mw-a.1"][1].Text != "second" || said["mw-a.1"][0].Author != "builder" {
		t.Errorf("expected the two comments of mw-a.1 oldest first, got %+v", said["mw-a.1"])
	}
	if got := said["mw-a.1"][0].Created; !got.Equal(time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)) {
		t.Errorf("expected the time bd sql gave, got %v", got)
	}
	if len(said["mw-o'b.2"]) != 1 || said["mw-o'b.2"][0].Text != "quoted" {
		t.Errorf("expected the comment of mw-o'b.2, got %+v", said["mw-o'b.2"])
	}
	if none, there := said["mw-c.3"]; !there || len(none) != 0 {
		t.Errorf("expected mw-c.3 there with no comments, got %+v (there: %v)", none, there)
	}
	if _, there := said["mw-nobody"]; there {
		t.Errorf("expected mw-nobody, which bd has no record of, left out, got %+v", said)
	}
	calls := asked(t, log)
	if len(calls) != 1 || !strings.Contains(calls[0], " sql ") {
		t.Fatalf("expected one bd sql for the batch, got %v", calls)
	}
	for _, want := range []string{"'mw-a.1'", "'mw-o''b.2'", "'mw-c.3'", "wisp_comments"} {
		if !strings.Contains(calls[0], want) {
			t.Errorf("expected the query to name %s, got %s", want, calls[0])
		}
	}
}

// The query names each id four times and Linux takes no argument over 128 KiB,
// so a long batch is read in queries of 400 ids at most.
func TestStoriesCommentsReadsALongBatchInQueriesOf400(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.88.0.2")
	gateway, log := standInSnapshot(t, "[]", "[]")
	var ids []string
	for i := range 401 {
		ids = append(ids, fmt.Sprintf("mw-long.%d", i))
	}

	if _, err := gateway.StoriesComments(context.Background(), ids); err != nil {
		t.Fatalf("reading the comments: %v", err)
	}
	calls := asked(t, log)
	if len(calls) != 2 {
		t.Fatalf("expected two bd sql for 401 ids, got %d", len(calls))
	}
	if !strings.Contains(calls[0], "'mw-long.399'") || strings.Contains(calls[0], "'mw-long.400'") || !strings.Contains(calls[1], "'mw-long.400'") {
		t.Errorf("expected the first 400 ids in the first query and the last in the second")
	}
}

// A server whose bd cannot answer the query is read as before, a bd show apiece.
func TestStoriesCommentsReadsABdShowApieceWhenTheSqlFails(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.88.0.2")
	db, ids := manyBeads(3)
	for _, id := range ids {
		db.shown[id] = fmt.Sprintf(`{"id":"%s","title":"t","status":"open","issue_type":"task","comments":[{"author":"a","text":"hello from %s","created_at":"2026-10-10T20:00:00Z"}]}`, id, id)
	}
	gateway, log, _ := roundsStandIn(t, db)

	said, err := gateway.StoriesComments(context.Background(), ids)
	if err != nil {
		t.Fatalf("reading the comments: %v", err)
	}
	for _, id := range ids {
		if len(said[id]) != 1 || said[id][0].Text != "hello from "+id {
			t.Errorf("expected the comment of %s, got %+v", id, said[id])
		}
	}
	if got := len(showsAsked(t, log)); got != len(ids) {
		t.Errorf("expected a bd show for each of the %d beads after the sql, got %d: %v", len(ids), got, asked(t, log))
	}
}

func TestShowEpicsReadsTheEpicsThemselvesWithOneListAgainstAServer(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.88.0.2")
	gateway, log, _ := roundsStandIn(t, epicsWithChildren())

	epics, err := gateway.ShowEpics(context.Background(), []string{"mw-e1", "mw-e2", "mw-e3", "mw-e4", "mw-e5", "mw-e6"})
	if err != nil {
		t.Fatalf("reading the epics: %v", err)
	}
	if len(epics) != 6 || epics[0].ID != "mw-e1" || epics[5].ID != "mw-e6" {
		t.Errorf("expected the six epics in the order asked, got %+v", epics)
	}
	if shows := showsAsked(t, log); len(shows) != 0 {
		t.Errorf("expected no bd show for the epics, got %v", shows)
	}
}

// An open story that names a host as its assignee is in neither `bd ready
// --unassigned` nor the in-progress list: WorkInHand finds it in the open list
// and no other open story with it (mw-gq6.417).
func TestWorkInHandFindsAnOpenStoryStillClaimedByAHost(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	stale := `{"id":"mw-a.9","title":"t","status":"open","issue_type":"task","assignee":"mw@laptop","parent":"mw-e1"}`
	mayors := `{"id":"mw-a.8","title":"t","status":"open","issue_type":"task","assignee":"mayor","parent":"mw-e1"}`
	gateway, _, _ := roundsStandIn(t, roundsDB{
		claimed: "[" + stale + "," + mayors + "," + story("mw-a.7", "mw-e1") + "]",
		shown:   map[string]string{"mw-e1": epic("mw-e1", "laptop")},
	})

	inHand, err := gateway.WorkInHand(context.Background())
	if err != nil {
		t.Fatalf("reading what is in hand: %v", err)
	}
	if len(inHand.OpenClaimed) != 1 || inHand.OpenClaimed[0].Story.ID != "mw-a.9" {
		t.Fatalf("expected only mw-a.9 open but claimed, got %+v", inHand.OpenClaimed)
	}
}
