package beads_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// standInBd writes a shell script as a stand-in for bd.
func standInBd(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	program := filepath.Join(t.TempDir(), "bd-standin")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program
}

// A bd create that stalls (the single-writer lock, a Dolt server under load)
// ended only at the caller's own time bound, with no bead and no word of why
// (mw-gq6.563). It now ends at its own bound and names the cause.
func TestABdCreateThatStallsFailsAtItsBoundNamingTheCause(t *testing.T) {
	program := standInBd(t, "sleep 60\n")
	gateway := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithCreateBound(300*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	started := time.Now()
	id, err := gateway.CreateStory(ctx, application.NewStory{Title: "[bug] x", EpicID: "mw-x", Description: "the finding\n\nHOW TO CHECK IT"})

	if err == nil {
		t.Fatalf("expected a refusal, got story %q", id)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("expected the stalled create to stop at its bound, it took %s", elapsed)
	}
	for _, want := range []string{"did not finish", "no bead was made", "lock"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to say %q, got: %v", want, err)
		}
	}
	if ctx.Err() != nil {
		t.Errorf("the caller's own time was used up: %v", ctx.Err())
	}
}

// bd must never be handed the process's own standard input: a bd that reads it
// would wait on whatever holds the other end open. It is given none.
func TestBdIsGivenNoStandardInputToWaitOn(t *testing.T) {
	program := standInBd(t, "cat >/dev/null\necho t-1\n")
	gateway := beads.New(t.TempDir(), beads.WithProgram(program))

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	was := os.Stdin
	os.Stdin = reader
	defer func() { os.Stdin = was }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, err := gateway.CreateStory(ctx, application.NewStory{Title: "[bug] x", EpicID: "mw-x", Description: "d"})
	if err != nil || id != "t-1" {
		t.Fatalf("expected story t-1, got %q, %v", id, err)
	}
}
