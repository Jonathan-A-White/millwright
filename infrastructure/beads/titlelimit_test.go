package beads_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// aBdThatRefusesLongTitles is a stand-in for bd that refuses a create whose
// title is over 500 bytes, as bd does, and keeps the arguments of the last call
// it was given, one per NUL, in the file it names.
func aBdThatRefusesLongTitles(t *testing.T) (program, args string) {
	t.Helper()
	args = filepath.Join(t.TempDir(), "args")
	program = standInBd(t, "printf '%s\\0' \"$@\" > '"+args+"'\n"+
		"while [ $# -gt 0 ] && [ \"$1\" != create ]; do shift; done\n"+
		"if [ \"$1\" = create ] && [ \"$(printf '%s' \"$2\" | wc -c)\" -gt 500 ]; then echo \"title must be 500 characters or less\" >&2; exit 1; fi\n"+
		"echo t-1\n")
	return program, args
}

func flagOf(t *testing.T, argsFile, flag string) (title, value string) {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("reading the stand-in's arguments: %v", err)
	}
	args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	at := -1
	for i, arg := range args {
		if arg == "create" && at < 0 {
			at = i
		}
		if arg == flag && i+1 < len(args) {
			value = args[i+1]
		}
	}
	if at < 0 || at+1 >= len(args) {
		t.Fatalf("expected bd create <title>, got %q", args)
	}
	return args[at+1], value
}

// mw-gq6.1521: bd refuses a title over 500 characters, and lampas main's red
// was lost for it. The adapter's create cuts any longer title to fit and keeps
// the whole of it at the head of the description, and a message's subject the
// same in its body, so no title a use case builds loses its write this way.
func TestACreateCutsATitleOverBdsLimitAndKeepsTheWholeInTheDescription(t *testing.T) {
	long := "[bug] main is red: " + strings.Repeat("× Then the reader sees the chapter's first page, ", 14)
	if len(long) <= domain.MaxTitleLength {
		t.Fatalf("the fixture's title is only %d bytes", len(long))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for _, create := range []struct {
		what string
		do   func(*beads.Gateway) (string, error)
	}{
		{"story", func(g *beads.Gateway) (string, error) {
			return g.CreateStory(ctx, application.NewStory{Title: long, EpicID: "mw-x", Description: "the finding"})
		}},
		{"epic", func(g *beads.Gateway) (string, error) {
			return g.CreateEpic(ctx, application.NewEpic{Title: long, Description: "the finding"})
		}},
		{"message", func(g *beads.Gateway) (string, error) {
			return g.Send(ctx, application.NewMessage{From: "mw", To: "mayor", Subject: long, Body: "the finding"})
		}},
	} {
		program, args := aBdThatRefusesLongTitles(t)
		id, err := create.do(beads.New(t.TempDir(), beads.WithProgram(program)))
		if err != nil || id != "t-1" {
			t.Fatalf("expected the %s filed as t-1, got %q, %v", create.what, id, err)
		}
		title, description := flagOf(t, args, "--description")
		if len(title) > domain.MaxTitleLength || !utf8.ValidString(title) {
			t.Errorf("expected the %s's title cut to at most %d bytes at a character, got %d: %q", create.what, domain.MaxTitleLength, len(title), title)
		}
		if !strings.HasSuffix(title, domain.Ellipsis) || !strings.HasPrefix(long, strings.TrimSuffix(title, domain.Ellipsis)) {
			t.Errorf("expected the %s's title to be the head of the whole with an ellipsis, got %q", create.what, title)
		}
		if !strings.Contains(description, strings.TrimSpace(long)) || !strings.Contains(description, "the finding") {
			t.Errorf("expected the %s's description to keep the whole title and its own text, got %q", create.what, description)
		}
	}

	// A title within the limit goes to bd as it is, and the description too.
	program, args := aBdThatRefusesLongTitles(t)
	if _, err := beads.New(t.TempDir(), beads.WithProgram(program)).CreateStory(ctx, application.NewStory{Title: "[bug] short", EpicID: "mw-x", Description: "the finding"}); err != nil {
		t.Fatal(err)
	}
	if title, description := flagOf(t, args, "--description"); title != "[bug] short" || description != "the finding" {
		t.Errorf("expected a short title and its description untouched, got %q and %q", title, description)
	}
}

// aBdThatRefusesLongEditedTitles is a stand-in for bd that refuses an update
// whose --title is over 500 bytes, as bd does, and answers a show with one
// story whose description is "the old notes". It keeps the arguments of the
// last update it was given, one per NUL, in the file it names.
func aBdThatRefusesLongEditedTitles(t *testing.T) (program, args string) {
	t.Helper()
	args = filepath.Join(t.TempDir(), "args")
	program = standInBd(t, "case \"$*\" in *' show '*) echo '[{\"id\": \"t-1\", \"title\": \"A story\", \"status\": \"open\", \"description\": \"the old notes\"}]'; exit 0;; esac\n"+
		"printf '%s\\0' \"$@\" > '"+args+"'\n"+
		"while [ $# -gt 0 ]; do\n"+
		"  if [ \"$1\" = --title ] && [ \"$(printf '%s' \"$2\" | wc -c)\" -gt 500 ]; then echo \"title must be 500 characters or less\" >&2; exit 1; fi\n"+
		"  shift\n"+
		"done\n")
	return program, args
}

func updateFlag(t *testing.T, argsFile, flag string) string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("reading the stand-in's arguments: %v", err)
	}
	args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// mw-gq6.1538: an edit's --title went to bd with no guard, so bd refused one
// over 500 bytes. The adapter's edit cuts it as a create does and keeps the
// whole at the head of the description, the one the edit names or, when it
// names none, the one the story holds.
func TestAnEditCutsATitleOverBdsLimit(t *testing.T) {
	long := "[bug] main is red: " + strings.Repeat("× Then the reader sees the chapter's first page, ", 14)
	if len(long) <= domain.MaxTitleLength {
		t.Fatalf("the fixture's title is only %d bytes", len(long))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	notes := "the new notes"

	for _, edit := range []struct {
		what        string
		edit        application.StoryEdit
		wantNotesIn string
	}{
		{"a title alone", application.StoryEdit{Title: &long}, "the old notes"},
		{"a title and a description", application.StoryEdit{Title: &long, Description: &notes}, "the new notes"},
	} {
		program, args := aBdThatRefusesLongEditedTitles(t)
		if err := beads.New(t.TempDir(), beads.WithProgram(program)).EditStory(ctx, "t-1", edit.edit); err != nil {
			t.Fatalf("expected %s edited, got %v", edit.what, err)
		}
		title, description := updateFlag(t, args, "--title"), updateFlag(t, args, "--description")
		if len(title) > domain.MaxTitleLength || !utf8.ValidString(title) {
			t.Errorf("expected %s cut to at most %d bytes at a character, got %d: %q", edit.what, domain.MaxTitleLength, len(title), title)
		}
		if !strings.HasSuffix(title, domain.Ellipsis) || !strings.HasPrefix(long, strings.TrimSuffix(title, domain.Ellipsis)) {
			t.Errorf("expected %s's title to be the head of the whole with an ellipsis, got %q", edit.what, title)
		}
		if !strings.Contains(description, strings.TrimSpace(long)) || !strings.Contains(description, edit.wantNotesIn) {
			t.Errorf("expected %s's description to keep the whole title and %q, got %q", edit.what, edit.wantNotesIn, description)
		}
	}

	// A title within the limit goes to bd as it is, with no read of the story.
	program, args := aBdThatRefusesLongEditedTitles(t)
	short := "[bug] short"
	if err := beads.New(t.TempDir(), beads.WithProgram(program)).EditStory(ctx, "t-1", application.StoryEdit{Title: &short}); err != nil {
		t.Fatal(err)
	}
	if title, description := updateFlag(t, args, "--title"), updateFlag(t, args, "--description"); title != short || description != "" {
		t.Errorf("expected a short title untouched and no description, got %q and %q", title, description)
	}
}
