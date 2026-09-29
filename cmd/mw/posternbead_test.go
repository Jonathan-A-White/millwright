package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// bdShowing puts a stand-in for bd first on PATH that answers bd show with
// shown — carrying comments inline, as bd show --include-comments does — bd
// list with an empty list and bd comments with comments.
func bdShowing(t *testing.T, shown, comments string) {
	t.Helper()
	if trimmed := strings.TrimSpace(shown); strings.HasSuffix(trimmed, "}]") {
		shown = strings.TrimSuffix(trimmed, "}]") + `, "comments": ` + comments + "}]"
	}
	bin := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  case "$a" in
    show) printf '%%s\n' %s; exit 0;;
    comments) printf '%%s\n' %s; exit 0;;
    list) echo '[]'; exit 0;;
  esac
done
exit 0
`, shellWord(shown), shellWord(comments))
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for bd: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func shellWord(text string) string { return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'" }

const bdShowStory = `[{"id": "t-e.2", "title": "A bug", "status": "open", "priority": 1, "issue_type": "bug",
  "description": "Full **markdown**", "acceptance_criteria": "it works", "comment_count": 1}]`

func TestPosternBeadJSONPrintsTheDetail(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	bdShowing(t, bdShowStory, `[{"author": "root", "text": "a word", "created_at": "2026-09-28T10:00:00Z"}]`)

	out, err := runPostern(t, "bead", "t-e.2", "--json")
	if err != nil {
		t.Fatalf("mw postern bead --json failed: %v\n%s", err, out)
	}
	var detail application.PosternBeadDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("expected the detail's JSON, got %q: %v", out, err)
	}
	if detail.ID != "t-e.2" || detail.Type != "bug" || detail.Description != "Full **markdown**" ||
		detail.Acceptance != "it works" || len(detail.Comments) != 1 || detail.Comments[0].Author != "root" {
		t.Fatalf("expected the bug in full, got %+v", detail)
	}
}

func TestPosternBeadPrintsTheSealedDetail(t *testing.T) {
	posternHome(t, "http://unused", "", "governor-pubkey-hex")
	bdShowing(t, bdShowStory, `[]`)
	cipher := apptest.NewFakeCipher()
	realCipher := posternCipher
	t.Cleanup(func() { posternCipher = realCipher })
	posternCipher = func(*postern.KeyFile) application.Cipher { return cipher }

	out, err := runPostern(t, "bead", "t-e.2")
	if err != nil {
		t.Fatalf("mw postern bead failed: %v\n%s", err, out)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 1 {
		t.Fatalf("expected one line of sealed text, got %q", out)
	}
	var detail application.PosternBeadDetail
	if err := application.OpenPosternDoc(cipher, "any", out, &detail); err != nil {
		t.Fatalf("opening the printed detail: %v", err)
	}
	if detail.ID != "t-e.2" || detail.V != 2 {
		t.Fatalf("expected the sealed detail of t-e.2, got %+v", detail)
	}
}

// A bead bd does not know leaves mw with status 3: the backend's 404.
func TestPosternBeadLeavesWithThreeForABeadThatIsNotThere(t *testing.T) {
	posternHome(t, "http://unused", "", "governor-pubkey-hex")
	bdShowing(t, `[]`, `[]`)

	out, err := runPostern(t, "bead", "t-nope")
	if err == nil {
		t.Fatalf("expected mw postern bead to fail for an unknown bead, got %q", out)
	}
	if got := exitStatus(err); got != 3 {
		t.Fatalf("expected status 3, got %d (%v)", got, err)
	}
	if !strings.Contains(out, "t-nope") {
		t.Fatalf("expected the error to name the bead, got %q", out)
	}
	if got := exitStatus(fmt.Errorf("anything else")); got != 1 {
		t.Fatalf("expected any other failure to leave with 1, got %d", got)
	}
}

// bdPageFixture stands in for bd over a small tree: the epic t-e, and under it
// t-e.1, t-e.2 (the story asked about, waiting on t-e.1) and t-e.3 (waiting on
// t-e.2). Every invocation is appended, one line each, to the returned log
// file. bd show prints the story with its parent embedded and its comments
// inline whenever --include-comments is asked for; bd comments prints the same
// comments alone.
func bdPageFixture(t *testing.T) (log string) {
	t.Helper()
	bin := t.TempDir()
	log = filepath.Join(t.TempDir(), "bd.log")
	const parent = `{"id":"t-e","title":"The epic","status":"open","priority":1,"issue_type":"epic",` +
		`"metadata":{"rig":"postern","host":"laptop","model":"sonnet","harness":"claude"},"dependency_type":"parent-child"}`
	const comments = `[{"id":"c2","author":"root","text":"second word","created_at":"2026-09-28T11:00:00Z"},` +
		`{"id":"c1","author":"mw@laptop","text":"first word","created_at":"2026-09-28T10:00:00Z"}]`
	story := `{"id":"t-e.2","title":"The story","status":"open","priority":2,"issue_type":"task","parent":"t-e",` +
		`"created_at":"2026-09-28T09:00:00Z","description":"Do it","acceptance_criteria":"it is done","labels":["hitl"],` +
		`"metadata":{"model":"opus"},"comment_count":2,"dependencies":[` + parent +
		`,{"id":"t-e.1","status":"open","issue_type":"task","dependency_type":"blocks"}]`
	epic := `{"id":"t-e","title":"The epic","status":"open","priority":1,"issue_type":"epic",` +
		`"metadata":{"rig":"postern","host":"laptop","model":"sonnet","harness":"claude"}`
	children := `[{"id":"t-e.3","title":"After","status":"open","priority":2,"issue_type":"task","parent":"t-e",` +
		`"created_at":"2026-09-28T09:02:00Z","dependencies":[{"issue_id":"t-e.3","depends_on_id":"t-e.2","type":"blocks"},` +
		`{"issue_id":"t-e.3","depends_on_id":"t-e","type":"parent-child"}]},` +
		`{"id":"t-e.2","title":"The story","status":"open","priority":2,"issue_type":"task","parent":"t-e",` +
		`"created_at":"2026-09-28T09:01:00Z"},` +
		`{"id":"t-e.1","title":"Before","status":"open","priority":2,"issue_type":"task","parent":"t-e",` +
		`"created_at":"2026-09-28T09:00:00Z"}]`
	// The gateway names the vault and the actor before the subcommand: find the
	// subcommand, then the bead among what follows.
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >> %s
sub= withcomments= bead=
for a in "$@"; do
  case "$a" in
    show|comments|list) [ -z "$sub" ] && sub="$a";;
    --include-comments) withcomments=1;;
    t-e|t-e.2) [ -z "$bead" ] && bead="$a";;
  esac
done
case "$sub" in
  show)
    case "$bead" in
      t-e.2) if [ -n "$withcomments" ]; then printf '[%%s,"comments":%%s}]\n' %s %s; else printf '[%%s}]\n' %s; fi;;
      t-e) printf '[%%s}]\n' %s;;
      *) echo "Error: no issue found matching $bead" >&2; exit 1;;
    esac
    exit 0;;
  comments) printf '%%s\n' %s; exit 0;;
  list) printf '%%s\n' %s; exit 0;;
esac
exit 0
`, shellWord(log), shellWord(story), shellWord(comments), shellWord(story), shellWord(epic), shellWord(comments), shellWord(children))
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for bd: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// wantBeadPage is what mw postern bead --json printed for the tree of
// bdPageFixture before the page was read in fewer bd calls: the bead, what it
// waits on and blocks, the merged path and its comments oldest first.
const wantBeadPage = `{"v":2,"id":"t-e.2","title":"The story","type":"task","status":"open","priority":2,"parent":"t-e","labels":["hitl"],"assignee":"","waits":["t-e.1"],"blocks":["t-e.3"],"children":[],"created":"2026-09-28T09:00:00Z","updated":"","started":"","closed":"","path":{"rig":"postern","branch":"","host":"laptop","model":"opus","effort":"","formula":"","harness":"claude"},"attempts":0,"description":"Do it","acceptance":"it is done","comments":[{"at":"2026-09-28T10:00:00Z","author":"mw@laptop","text":"first word"},{"at":"2026-09-28T11:00:00Z","author":"root","text":"second word"}]}`

// A bead's page costs the backend's 30 s limit one bd process per bd call, and
// each waits behind every other bd: a story with a parent takes at most three,
// and prints what it always printed.
func TestPosternBeadPageTakesAtMostThreeBdCalls(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	log := bdPageFixture(t)

	out, err := runPostern(t, "bead", "t-e.2", "--json")
	if err != nil {
		t.Fatalf("mw postern bead --json failed: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Split(strings.TrimSpace(string(calls)), "\n")); n > 3 {
		t.Errorf("expected at most 3 bd calls for a story with a parent, got %d:\n%s", n, calls)
	}
	if got := strings.TrimSpace(out); got != wantBeadPage {
		t.Errorf("expected the page to print as it always did:\nwant %s\ngot  %s", wantBeadPage, got)
	}
}
