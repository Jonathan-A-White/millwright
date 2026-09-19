package claude

// The transcripts adapter is tested against fixture transcripts written under
// t.TempDir(), with the projects root pointed there. Nothing here reads the
// machine's own ~/.claude.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// writeTranscript writes a transcript of these lines into the projects
// directory of dir, modified at the given time, and returns its path.
func writeTranscript(t *testing.T, root, dir, session string, modified time.Time, lines ...string) string {
	t.Helper()
	project := ProjectDir(root, dir)
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("making the project directory: %v", err)
	}
	path := filepath.Join(project, session+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("writing the transcript: %v", err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatalf("dating the transcript: %v", err)
	}
	return path
}

// turn is one assistant line of a transcript.
func turn(input, cacheRead, cacheCreation int) string {
	return `{"type":"assistant","message":{"role":"assistant","usage":{"input_tokens":` + strconv.Itoa(input) +
		`,"output_tokens":700,"cache_read_input_tokens":` + strconv.Itoa(cacheRead) +
		`,"cache_creation_input_tokens":` + strconv.Itoa(cacheCreation) + `}}}`
}

const userLine = `{"type":"user","message":{"role":"user","content":"go on"}}`

func TestProjectDirIsTheDirectoryWithEveryOddCharacterAHyphen(t *testing.T) {
	// The names Claude Code gives its own project directories: a slash, and a
	// dot in a hidden directory, each become a hyphen.
	got := ProjectDir("/r/projects", "/home/jwhite/.mw-worktrees/mw-0om.2")
	want := "/r/projects/-home-jwhite--mw-worktrees-mw-0om-2"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestLiveContextIsTheLastAssistantTurnOfTheNewestTranscript(t *testing.T) {
	root := t.TempDir()
	dir := "/work/seat"
	morning := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)

	// An older session that ran much fuller, and must not be read.
	writeTranscript(t, root, dir, "11111111-old", morning,
		userLine, turn(1, 190000, 5000))
	// The newest: three assistant turns, a subagent's turn after them (another
	// context, so not this one's) and a line cut off mid-write.
	writeTranscript(t, root, dir, "22222222-new", morning.Add(time.Hour),
		userLine,
		turn(3, 20000, 4000),
		userLine,
		turn(1, 24000, 6000),
		userLine,
		turn(2, 30000, 5000),
		`{"isSidechain":true,"type":"assistant","message":{"role":"assistant","usage":{"input_tokens":999999}}}`,
		`{"type":"assistant","message":{"usage":{"input_to`)

	got, err := NewTranscripts(root).LiveContext(context.Background(), dir)
	if err != nil {
		t.Fatalf("reading the live context: %v", err)
	}
	if got.Tokens != 2+30000+5000 {
		t.Fatalf("expected the last turn's 35002 tokens, got %d", got.Tokens)
	}
	if got.SessionID != "22222222-new" {
		t.Fatalf("expected the newest transcript's session, got %q", got.SessionID)
	}
}

func TestLiveContextReadsAnotherDirectorysTranscriptsNotAtAll(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "/work/elsewhere", "33333333", time.Now(), turn(1, 1, 1))

	_, err := NewTranscripts(root).LiveContext(context.Background(), "/work/seat")
	if err == nil {
		t.Fatalf("expected no transcript for a directory that has none")
	}
	if want := ProjectDir(root, "/work/seat"); !strings.Contains(err.Error(), want) {
		t.Fatalf("expected the error to name %s, got %q", want, err)
	}
}

func TestLiveContextWithNoAssistantTurnYetSaysWhereItLooked(t *testing.T) {
	root := t.TempDir()
	path := writeTranscript(t, root, "/work/seat", "44444444", time.Now(), userLine)

	_, err := NewTranscripts(root).LiveContext(context.Background(), "/work/seat")
	if err == nil {
		t.Fatalf("expected an error from a transcript with no assistant turn")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("expected the error to name %s, got %q", path, err)
	}
}

func TestDefaultProjectsRootIsUnderTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := DefaultProjectsRoot()
	if err != nil {
		t.Fatalf("finding the projects root: %v", err)
	}
	if want := filepath.Join(home, ".claude", "projects"); got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
