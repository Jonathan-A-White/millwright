package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
}

// roundsStandIn is a stand-in bd over a roundsDB. `show` takes any number of
// ids and prints the bead for each, as bd does, and refuses when one is unknown.
// Every argv is written down, and every `list --parent` holds for a moment and
// writes "overlap" to the file returned if another bd was inside one at the time.
func roundsStandIn(t *testing.T, db roundsDB, opts ...beads.Option) (*beads.Gateway, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "asked.log")
	overlap := filepath.Join(dir, "overlap")

	var known, arms, kids strings.Builder
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
      *) printf '%%s' '%[4]s' ;;
    esac ;;
  show)
    shift
    out=""
    for id in "$@"; do
      case "$id" in
        --json) continue ;;
%[7]s        *) echo "no issues found matching $id" >&2; exit 1 ;;
      esac
    done
    first=1
    printf '['
    for id in "$@"; do
      case "$id" in
        --json) continue ;;
%[8]s      esac
      [ $first = 1 ] || printf ','
      first=0
      printf '%%s' "$bead"
    done
    printf ']' ;;
  *) exit 1 ;;
esac
`, log, db.ready, db.blocked, db.claimed, overlap, kids.String(), known.String(), arms.String())
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
	if got := bdCalls(t, log); got > 3 {
		t.Errorf("expected ready, list and one show of the epics (3 bd calls), got %d: %v", got, asked(t, log))
	}
}

// bd blocked prints rows with no parent, so each blocked story is read back,
// and its epic after it: the stories in one bd, the epics in another.
func TestBlockedForHostReadsTheStoriesAndTheEpicsInOneBdEach(t *testing.T) {
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
