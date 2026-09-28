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
// shown, bd list with an empty list and bd comments with comments.
func bdShowing(t *testing.T, shown, comments string) {
	t.Helper()
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
