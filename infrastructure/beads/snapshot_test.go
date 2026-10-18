package beads_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// standInSnapshot writes a stand-in bd that answers `list` with listing and
// `sql` with table, writes down every argv to a file beside it, and answers
// anything else with a failure, since a snapshot-built view asks for nothing
// else; it returns the Gateway that runs it and the file it writes to.
func standInSnapshot(t *testing.T, listing, table string) (*beads.Gateway, string) {
	t.Helper()
	program, asked := standInSnapshotProgram(t, listing, table)
	return beads.New(t.TempDir(), beads.WithProgram(program)), asked
}

// standInSnapshotProgram is the stand-in bd of standInSnapshot, by the path of
// the program, for a Gateway the test builds itself.
func standInSnapshotProgram(t *testing.T, listing, table string) (program, asked string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	asked = filepath.Join(dir, "asked")
	for name, body := range map[string]string{"list": listing, "sql": table} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %[1]q
case "$*" in
*" list "*) cat %[2]q ;;
*" sql "*) cat %[3]q ;;
*) echo "unexpected bd call: $*" >&2; exit 1 ;;
esac
`, asked, filepath.Join(dir, "list"), filepath.Join(dir, "sql"))
	path := filepath.Join(dir, "bd-snapshot")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, asked
}

// mw-jrx0s.19: a view built through the snapshot is two bd calls — one list and
// one sql for the notes and the comments — with the comments and the notes it
// needs read from that second call, and a bead it holds no comments for read
// through the gateway's own call.
func TestSnapshotReadsTheNotesAndTheCommentsInOneSqlCall(t *testing.T) {
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	listing := `[
	 {"id":"t-1","title":"Epic","status":"open","issue_type":"epic","priority":1,"comment_count":0},
	 {"id":"t-1.1","title":"By hand","status":"open","issue_type":"task","parent":"t-1","labels":["hitl"],"comment_count":1},
	 {"id":"t-1.2","title":"Asked","status":"open","issue_type":"task","parent":"t-1","comment_count":0},
	 {"id":"t-0","title":"Long gone","status":"closed","issue_type":"task","closed_at":"2026-08-01T00:00:00Z","comment_count":3}
	]`
	table := `[
	 {"k":"c","id":"c1","a":"t-1.1","b":"mw@laptop","t":"BY HAND: do the thing.","at":"2026-10-01T12:00:00Z"},
	 {"k":"k","id":"","a":"postern.question.t-1.2","b":"{\"text\":\"Which way?\"}","t":"","at":null},
	 {"k":"k","id":"","a":"doctor.laptop.x","b":"ok","t":"","at":null}
	]`
	gateway, asked := standInSnapshot(t, listing, table)

	view := application.PosternView{
		Tracker: gateway, Notes: gateway, Host: "laptop", Now: func() time.Time { return now },
	}
	doc, err := view.Build(context.Background())
	if err != nil {
		t.Fatalf("building the view: %v", err)
	}
	encoded, _ := json.Marshal(doc)
	if !strings.Contains(string(encoded), "do the thing") {
		t.Errorf("expected the BY HAND comment from the sql call in the view, got %s", encoded)
	}
	if !strings.Contains(string(encoded), `"t-1.2"`) {
		t.Errorf("expected the bead with a note in the view, got %s", encoded)
	}

	log, _ := os.ReadFile(asked)
	calls := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(calls) > 2 {
		t.Errorf("expected the view in at most two bd calls, got %d:\n%s", len(calls), log)
	}
	// The closed bead long ago closed is left out of the comments asked for.
	if strings.Contains(string(log), "'t-0'") {
		t.Errorf("expected no comments asked for a bead closed long ago, bd was asked:\n%s", log)
	}
	if !strings.Contains(string(log), "'t-1.1'") {
		t.Errorf("expected the comments of the open bead asked for, bd was asked:\n%s", log)
	}
}

// A snapshot whose sql call gives nothing usable (an embedded-mode bd has no
// sql) still takes: it holds no notes, and the view asks for them itself.
func TestSnapshotGoesWithoutNotesWhenTheSqlCallFails(t *testing.T) {
	listing := `[{"id":"t-1","title":"Epic","status":"open","issue_type":"epic","priority":1,"comment_count":0}]`
	gateway, _ := standInSnapshot(t, listing, `Error: 'bd sql' is not yet supported in embedded mode`)
	snapshot, err := gateway.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("taking the snapshot: %v", err)
	}
	notes, ok := snapshot.(application.SnapshotNotes)
	if !ok {
		t.Fatal("expected the snapshot to offer its notes")
	}
	if got, held := notes.AllNotes(); held {
		t.Errorf("expected the snapshot to hold no notes, got %v", got)
	}
}

// mw-d6y59n: the view holds the stories filed under no epic — a running one and
// one landed today — and none of the mail, molecules, steps or epics that have
// no parent either.
func TestSnapshotViewHoldsTheStoriesUnderNoEpic(t *testing.T) {
	now := time.Date(2026, 10, 7, 18, 40, 0, 0, time.UTC)
	listing := `[
	 {"id":"t-run","title":"Running","status":"in_progress","issue_type":"bug","priority":1,"metadata":{"rig":"millwright"},"started_at":"2026-10-07T18:00:00Z"},
	 {"id":"t-done","title":"Landed","status":"closed","issue_type":"task","priority":2,"metadata":{"rig":"millwright"},"closed_at":"2026-10-07T18:26:00Z"},
	 {"id":"t-old","title":"Landed long ago","status":"closed","issue_type":"task","metadata":{"rig":"millwright"},"closed_at":"2026-08-01T00:00:00Z"},
	 {"id":"t-mail","title":"Mail","status":"open","issue_type":"mail"},
	 {"id":"t-mol","title":"Molecule","status":"open","issue_type":"molecule"},
	 {"id":"t-mol.1","title":"Step","status":"open","issue_type":"task","parent":"t-mol","metadata":{"rig":"millwright"}}
	]`
	gateway, _ := standInSnapshot(t, listing, `[]`)
	view := application.PosternView{
		Tracker: gateway, Notes: gateway, Host: "laptop", Now: func() time.Time { return now },
	}
	doc, err := view.Build(context.Background())
	if err != nil {
		t.Fatalf("building the view: %v", err)
	}
	got := map[string]application.PosternViewBead{}
	for _, b := range doc.Beads {
		got[b.ID] = b
	}
	if b, ok := got["t-run"]; !ok || b.Status != "in_progress" || b.Parent != "" {
		t.Errorf("expected the running story in progress under no epic, got %+v", b)
	}
	if b, ok := got["t-done"]; !ok || b.Status != "closed" || b.Closed != "2026-10-07T18:26:00Z" {
		t.Errorf("expected the landed story with its closed time, got %+v", b)
	}
	for _, id := range []string{"t-old", "t-mail", "t-mol", "t-mol.1"} {
		if _, ok := got[id]; ok {
			t.Errorf("expected no bead %s in the view, got %+v", id, got[id])
		}
	}
}

// A landing under an epic that has since closed keeps its verify need, folded
// once it is old enough, read from the same listing; one closed over 30 days
// ago, a story that never landed and a bead under a live epic's own read are
// not read as one.
func TestSnapshotViewHoldsALandingUnderAClosedEpicAsAFoldedVerifyNeed(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	listing := `[
	 {"id":"t-1","title":"Epic","status":"closed","issue_type":"epic","priority":1,"closed_at":"2026-10-08T00:00:00Z"},
	 {"id":"t-1.1","title":"Landed","status":"closed","issue_type":"task","parent":"t-1","labels":["run:landed"],"closed_at":"2026-10-08T00:00:00Z","comment_count":0},
	 {"id":"t-1.2","title":"Dropped","status":"closed","issue_type":"task","parent":"t-1","closed_at":"2026-10-08T00:00:00Z","comment_count":0},
	 {"id":"t-1.3","title":"Landed long ago","status":"closed","issue_type":"task","parent":"t-1","labels":["run:landed"],"closed_at":"2026-08-01T00:00:00Z","comment_count":0}
	]`
	gateway, _ := standInSnapshot(t, listing, `[]`)

	doc, err := application.PosternView{
		Tracker: gateway, Notes: gateway, Host: "laptop", Now: func() time.Time { return now },
	}.Build(context.Background())
	if err != nil {
		t.Fatalf("building the view: %v", err)
	}
	var verifies []string
	for _, need := range doc.Needs {
		if need.Kind == application.PosternNeedVerify {
			verifies = append(verifies, need.Bead)
			if !need.Folded || need.Epic != "t-1" {
				t.Errorf("expected a folded verify need under t-1, got %+v", need)
			}
		}
	}
	if len(verifies) != 1 || verifies[0] != "t-1.1" {
		t.Errorf("expected one verify need, on t-1.1, got %v", verifies)
	}
}

// mw-gq6.492: against a server a bd call is two seconds, and BlockedForHost
// cost `bd blocked` and a `bd show` for each of 168 blocked stories. The
// snapshot's listing holds every edge and every status, so it answers from the
// one read what `bd blocked` and the shows would: the open, unclaimed stories
// on the host with a blocking edge to a bead not yet closed, never a step of a
// poured formula, an epic, a claimed story or one that is only related.
func TestSnapshotBlockedForHostIsReadFromTheListing(t *testing.T) {
	edge := func(from, to, kind string) string {
		return fmt.Sprintf(`{"issue_id":%q,"depends_on_id":%q,"type":%q}`, from, to, kind)
	}
	listing := `[
	 {"id":"t-e","title":"Epic","status":"open","issue_type":"epic","priority":1,"metadata":{"host":"vps","rig":"millwright"}},
	 {"id":"t-0","title":"Done","status":"closed","issue_type":"task","parent":"t-e","closed_at":"2026-10-01T00:00:00Z"},
	 {"id":"t-c","title":"Still open","status":"open","issue_type":"task","parent":"t-e"},
	 {"id":"t-a","title":"Waits on a closed one","status":"open","issue_type":"task","parent":"t-e","dependencies":[` + edge("t-a", "t-0", "blocks") + `]},
	 {"id":"t-b","title":"Waits on t-c","status":"open","issue_type":"task","parent":"t-e","dependencies":[` + edge("t-b", "t-e", "parent-child") + `,` + edge("t-b", "t-0", "blocks") + `,` + edge("t-b", "t-c", "blocks") + `]},
	 {"id":"t-d","title":"Another host's","status":"open","issue_type":"task","parent":"t-e","metadata":{"host":"desktop"},"dependencies":[` + edge("t-d", "t-c", "blocks") + `]},
	 {"id":"t-f","title":"Claimed","status":"in_progress","assignee":"mw@vps","issue_type":"task","parent":"t-e","dependencies":[` + edge("t-f", "t-c", "blocks") + `]},
	 {"id":"t-g","title":"Only related","status":"open","issue_type":"task","parent":"t-e","dependencies":[` + edge("t-g", "t-c", "related") + `]},
	 {"id":"t-mol-1","title":"A step","status":"open","issue_type":"task","parent":"t-e","dependencies":[` + edge("t-mol-1", "t-c", "blocks") + `]},
	 {"id":"t-h","title":"Waits on t-c too","status":"open","issue_type":"task","parent":"t-e","dependencies":[` + edge("t-h", "t-c", "waits-for") + `]}
	]`
	gateway, asked := standInSnapshot(t, listing, `[]`)
	snapshot, err := gateway.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("taking the snapshot: %v", err)
	}

	blocked, err := snapshot.BlockedForHost(context.Background(), "vps")
	if err != nil {
		t.Fatalf("reading what is blocked: %v", err)
	}
	var ids []string
	for _, detail := range blocked {
		ids = append(ids, detail.Story.ID)
	}
	if got := strings.Join(ids, " "); got != "t-b t-h" {
		t.Fatalf("expected t-b and t-h blocked on vps, got %q", got)
	}
	if got := strings.Join(blocked[0].Needs, " "); got != "t-c" {
		t.Errorf("expected t-b to wait for t-c alone, the closed t-0 left out, got %q", got)
	}
	if blocked[0].Merged().Rig != "millwright" {
		t.Errorf("expected the epic's defaults overlaid, got %+v", blocked[0].Merged())
	}
	log, _ := os.ReadFile(asked)
	for _, call := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		if !strings.Contains(call, " list ") && !strings.Contains(call, " sql ") {
			t.Errorf("expected the listing and the sql call alone, bd was asked %q", call)
		}
	}
}

// mw-gq6.492: the stories in flight on this host and on the others come of one
// read of the in-progress list, each narrowed as RunningStories and
// RunningElsewhere narrow it.
func TestGatewayReadsWhatIsRunningHereAndElsewhereInOneList(t *testing.T) {
	listing := `[
	 {"id":"t-1","title":"Here","status":"in_progress","issue_type":"task","metadata":{"host":"vps"}},
	 {"id":"t-2","title":"There","status":"in_progress","issue_type":"task","metadata":{"host":"desktop"}},
	 {"id":"t-3","title":"Anywhere","status":"in_progress","issue_type":"task","metadata":{"host":"auto"}},
	 {"id":"t-4","title":"Nowhere","status":"in_progress","issue_type":"task"}
	]`
	gateway, asked := standInSnapshot(t, listing, `[]`)

	here, away, err := gateway.RunningEverywhere(context.Background(), "vps")
	if err != nil {
		t.Fatalf("reading what is running: %v", err)
	}
	if len(here) != 1 || here[0].Story.ID != "t-1" {
		t.Errorf("expected t-1 running here, got %+v", here)
	}
	if len(away) != 1 || away[0].Story.ID != "t-2" {
		t.Errorf("expected t-2 running elsewhere, got %+v", away)
	}
	log, _ := os.ReadFile(asked)
	if calls := strings.Split(strings.TrimSpace(string(log)), "\n"); len(calls) != 1 || !strings.Contains(calls[0], "--status in_progress") {
		t.Errorf("expected one list of the in-progress stories, bd was asked:\n%s", log)
	}
}

// mw-gq6.492: against a server a select of `bd sql` only reads, so it runs
// beside the reads under way. Taken for a write, as every sql was, it waited
// for each read to end and held back every one after it: a snapshot's second
// call started when all the others were done. The stand-in's list holds until
// the sql has run, so a sql that waited on it would never run.
func TestASqlSelectRunsBesideAListAgainstAServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.0.0.1")
	dir := t.TempDir()
	started, release := filepath.Join(dir, "list.started"), filepath.Join(dir, "release")
	script := fmt.Sprintf(`#!/bin/sh
shift 2
case "$1" in --actor) shift 2 ;; esac
case "$1" in
  list)
    : > %[1]q
    for i in $(seq 1 100); do
      [ -e %[2]q ] && { printf '[]'; exit 0; }
      sleep 0.1
    done
    echo "the sql never ran beside the list" >&2; exit 1 ;;
  sql) : > %[2]q; printf '[]' ;;
  *) exit 1 ;;
esac
`, started, release)
	path := filepath.Join(dir, "bd-sql")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	gateway := beads.New(t.TempDir(), beads.WithProgram(path))

	listed := make(chan error, 1)
	go func() {
		_, err := gateway.LiveEpics(context.Background())
		listed <- err
	}()
	for i := 0; ; i++ {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if i > 200 {
			t.Fatal("the list never started")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := gateway.StoriesComments(context.Background(), []string{"t-1", "t-2"}); err != nil {
		t.Fatalf("reading the comments: %v", err)
	}
	if err := <-listed; err != nil {
		t.Errorf("expected the list to end once the sql had run: %v", err)
	}
}
