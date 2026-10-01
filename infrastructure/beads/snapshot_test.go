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
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	asked := filepath.Join(dir, "asked")
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
	return beads.New(t.TempDir(), beads.WithProgram(path)), asked
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
