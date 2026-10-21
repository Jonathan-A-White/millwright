package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const clearBead = `[{"id": "mw-x.1", "title": "A story", "status": "open", "issue_type": "task"}]`

// bdWithQuestionNote is bdRecording with the question note of mw-x.1 set:
// kv get answers it, the others as bdRecording does.
func bdWithQuestionNote(t *testing.T, note string) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellWord(log) + "\n" +
		"case \"$*\" in\n  *\"kv get postern.question.mw-x.1\")\n" +
		"    if [ -n " + shellWord(note) + " ]; then printf '%s\\n' " + shellWord(note) + "; exit 0; fi\n" +
		"    echo 'not set' >&2; exit 1;;\nesac\n" +
		"for a in \"$@\"; do\n  case \"$a\" in\n    show) printf '%s\\n' " + shellWord(clearBead) + "; exit 0;;\n    list) echo '{}'; exit 0;;\n  esac\ndone\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for bd: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// mw postern clear clears the note, comments the bead with the why and replies
// under the card's txid; it never updates the bead.
func TestPosternClearClearsTheNoteCommentsAndReplies(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	push := fakeHandsPush(t, nil)
	log := bdWithQuestionNote(t, `{"txid":"direct:abc","q":"Release?"}`)

	out, err := runPostern(t, "clear", "mw-x.1", "--why", "he approved it by hand")
	if err != nil {
		calls, _ := os.ReadFile(log)
		t.Fatalf("mw postern clear failed: %v\n%s\n%s", err, out, calls)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{"kv clear postern.question.mw-x.1", "comment mw-x.1 Card cleared by mw postern clear: he approved it by hand"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("expected bd to be asked %q, got:\n%s", want, calls)
		}
	}
	if strings.Contains(string(calls), "update ") || strings.Contains(string(calls), "close ") {
		t.Errorf("expected the bead left alone, got:\n%s", calls)
	}
	sent := push.Sent()
	if len(sent) != 1 || sent[0].Re != "direct:abc" || sent[0].Thread != "mw-x.1" {
		t.Errorf("expected one reply under the card, got %+v", sent)
	}
}

// A bead with no question note is refused, naming it, and bd is asked to change
// nothing.
func TestPosternClearRefusesABeadWithNoQuestionNote(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	push := fakeHandsPush(t, nil)
	log := bdWithQuestionNote(t, "")

	out, err := runPostern(t, "clear", "mw-x.1", "--why", "x")
	if err == nil || !strings.Contains(err.Error(), "mw-x.1") {
		t.Fatalf("expected a refusal naming the bead, got %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(log)
	for _, bad := range []string{"kv clear", "comment ", "update "} {
		if strings.Contains(string(calls), bad) {
			t.Errorf("expected no %q, got:\n%s", bad, calls)
		}
	}
	if len(push.Sent()) != 0 {
		t.Errorf("expected no reply, got %+v", push.Sent())
	}
}

// --dry-run prints the three acts and changes nothing.
func TestPosternClearDryRunPrintsTheActsAndChangesNothing(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	push := fakeHandsPush(t, nil)
	log := bdWithQuestionNote(t, `{"txid":"direct:abc","q":"Release?"}`)

	out, err := runPostern(t, "clear", "mw-x.1", "--why", "x", "--dry-run")
	if err != nil {
		t.Fatalf("mw postern clear --dry-run failed: %v\n%s", err, out)
	}
	for _, want := range []string{"would clear the question note of mw-x.1", "would comment on mw-x.1", "would reply under direct:abc"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in %q", want, out)
		}
	}
	calls, _ := os.ReadFile(log)
	for _, bad := range []string{"kv clear", "comment ", "update "} {
		if strings.Contains(string(calls), bad) {
			t.Errorf("a dry run asked bd for %q:\n%s", bad, calls)
		}
	}
	if len(push.Sent()) != 0 {
		t.Errorf("a dry run sent %+v", push.Sent())
	}
}

// --why is required.
func TestPosternClearNeedsWhy(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	if out, err := runPostern(t, "clear", "mw-x.1"); err == nil {
		t.Fatalf("expected --why required, got %q", out)
	}
}
