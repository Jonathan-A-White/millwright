package beads_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// mw-gq6.1545: the sweep of the Millhand's tick asks after the story of every
// session on the host, and ShowBeads started a bd show for them each tick. Over
// the kept connection the beads are read by id, closed ones too, and each is as
// bd show prints it: its epic's Path as its defaults, its waits narrowed to the
// beads not yet closed, the reason it was closed for.
func TestBeadsAreShownByIdOverTheKeptConnectionWithoutStartingABd(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	server.reasons = map[string]string{"mw-e.9": "landed abc"}
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	got, err := g.ShowBeads(ctx, []string{"mw-e.4", "mw-e.9", "mw-e.2", "mw-e"})
	if err != nil {
		t.Fatal(err)
	}
	if ids := strings.Join(detailIDs(got), " "); ids != "mw-e.4 mw-e.9 mw-e.2 mw-e" {
		t.Fatalf("expected the beads in the order asked, got %s", ids)
	}
	waiting, closed, narrowed, epic := got[0], got[1], got[2], got[3]
	if strings.Join(waiting.Needs, " ") != "mw-e.5" {
		t.Errorf("expected mw-e.4 to wait on the bead in progress, got %v", waiting.Needs)
	}
	if waiting.EpicID != "mw-e" || waiting.Defaults.Host != "desktop" || waiting.Defaults.Model != "opus" {
		t.Errorf("expected the defaults of mw-e.4 to be its epic's Path, got epic %q defaults %+v", waiting.EpicID, waiting.Defaults)
	}
	if !closed.Closed() || closed.CloseReason != "landed abc" || closed.IsEpic {
		t.Errorf("expected mw-e.9 closed for 'landed abc' and no epic, got status %q reason %q", closed.Status, closed.CloseReason)
	}
	if len(narrowed.Needs) != 0 {
		t.Errorf("expected mw-e.2's wait on a closed bead to be over, got %v", narrowed.Needs)
	}
	if !epic.IsEpic || epic.Defaults.Host != "" {
		t.Errorf("expected mw-e to be an epic with no parent's defaults, got %+v", epic)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("showing beads over the kept connection started bd %d times:\n%s", len(calls), strings.Join(calls, "\n"))
	}
	if len(server.unknown) > 0 {
		t.Errorf("the stand-in server was asked what it does not know:\n%s", strings.Join(server.unknown, "\n"))
	}
}

// A bead the vault's issues do not hold may be one bd shows from elsewhere, so
// bd is asked for it alone, and for nothing the connection found.
func TestABeadTheKeptConnectionDoesNotFindIsAskedOfBdAlone(t *testing.T) {
	ctx := context.Background()
	program, asked := recordingBd(t)
	server := aQuietFactory(asked)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(server.open(t)), beads.WithKeptSaid(io.Discard))

	got, err := g.ShowBeads(ctx, []string{"mw-e.2", "mw-wisp-x"})
	if err != nil {
		t.Fatal(err)
	}
	if ids := strings.Join(detailIDs(got), " "); ids != "mw-e.2" {
		t.Errorf("expected only the bead that is there, got %s", ids)
	}
	calls := bdCallsSince(t, asked, 0)
	if len(calls) != 1 || !strings.Contains(calls[0], "show mw-wisp-x --json") || strings.Contains(calls[0], "mw-e.2") {
		t.Errorf("expected one bd show of the missing bead alone, got %q", calls)
	}
}

// With no kept connection the beads are shown by bd, as they were.
func TestBeadsAreShownByBdWhenThereIsNoKeptConnection(t *testing.T) {
	program, asked := recordingBd(t)
	g := beads.New(t.TempDir(), beads.WithProgram(program))
	if _, err := g.ShowBeads(context.Background(), []string{"mw-e.2", "mw-e.4"}); err != nil {
		t.Fatal(err)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) == 0 || !strings.Contains(strings.Join(calls, "\n"), "show") {
		t.Errorf("expected bd show with no kept connection, got %q", calls)
	}
}
