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

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// standInFeed writes a stand-in bd that answers a `bd sql` over the comments
// table with comments, one over the events table with changes, and anything
// else with states, logging every query it is asked to a file beside it.
func standInFeed(t *testing.T, changes, comments, states string) (*beads.Gateway, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	asked := filepath.Join(dir, "asked")
	for name, body := range map[string]string{"changes": changes, "comments": comments, "states": states} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do last="$a"; done
printf '%%s\n' "$last" >> %[1]q
case "$last" in
*"as newest"*) printf '[{"newest":"2026-10-01T13:00:09Z"}]' ;;
*"from comments c "*) cat %[2]q ;;
*"from events e "*) cat %[3]q ;;
*) cat %[4]q ;;
esac
`, asked, filepath.Join(dir, "comments"), filepath.Join(dir, "changes"), filepath.Join(dir, "states"))
	path := filepath.Join(dir, "bd-feed")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return beads.New(t.TempDir(), beads.WithProgram(path)), asked
}

func TestBeadChangesReadsBdsEventsAndCommentsSinceATime(t *testing.T) {
	changes := `[{"k":"e1","id":"mw-1","what":"label_added","actor":"mw@laptop","at":"2026-10-01T13:00:02Z","type":"task","status":"in_progress","assignee":"mw@laptop","labels":"run:running"}]`
	comments := `[{"k":"c1","id":"mw-2","actor":"mw@laptop","text":"ANSWER x, txid direct:abc: yes","at":"2026-10-01T13:00:01Z","type":"task","status":"open","assignee":null,"labels":null}]`
	gateway, asked := standInFeed(t, changes, comments, `[]`)
	since := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	got, err := gateway.BeadChanges(context.Background(), since)
	if err != nil {
		t.Fatalf("reading the changes: %v", err)
	}
	want := []application.BeadChange{
		{Key: "c1", At: since.Add(time.Second), Actor: "mw@laptop", What: application.ChangeComment, Comment: "ANSWER x, txid direct:abc: yes",
			Bead: application.BeadNow{ID: "mw-2", Type: "task", Status: "open"}},
		{Key: "e1", At: since.Add(2 * time.Second), Actor: "mw@laptop", What: "label_added",
			Bead: application.BeadNow{ID: "mw-1", Type: "task", Status: "in_progress", Run: "running", Assignee: "mw@laptop"}},
	}
	if fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", want) {
		t.Fatalf("BeadChanges =\n%+v\nwant, oldest first,\n%+v", got, want)
	}
	queries, _ := os.ReadFile(asked)
	if n := strings.Count(string(queries), "'2026-10-01 13:00:00'"); n != 2 {
		t.Fatalf("expected both queries bounded by the since time, bd was asked:\n%s", queries)
	}
	if !strings.Contains(string(queries), "issue_type <> 'event'") {
		t.Fatalf("expected bd's own event beads left out, bd was asked:\n%s", queries)
	}
}

func TestBeadStatesReadsEveryBeadAndTheNewestChange(t *testing.T) {
	states := `[{"id":"mw-1","type":"task","status":"deferred","assignee":null,"labels":null},{"id":"mw-2","type":"mail","status":"open","assignee":"mayor","labels":null}]`
	gateway, _ := standInFeed(t, `[]`, `[]`, states)
	got, newest, err := gateway.BeadStates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Status != "deferred" || got[1].Assignee != "mayor" || got[1].Run != "" {
		t.Fatalf("BeadStates = %+v", got)
	}
	if want := time.Date(2026, 10, 1, 13, 0, 9, 0, time.UTC); !newest.Equal(want) {
		t.Fatalf("newest = %v, want %v", newest, want)
	}
}

func TestBeadChangesRefusesAnAnswerItCannotRead(t *testing.T) {
	gateway, _ := standInFeed(t, `not json`, `[]`, `[]`)
	if _, err := gateway.BeadChanges(context.Background(), time.Now()); err == nil {
		t.Fatal("expected unreadable output to be an error")
	}
}
