package rig_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// A close-out runs in a pipeline, `{ mw next; } | tee -a next-N.log`, started in
// the story's worktree, so the tee that keeps its words holds the worktree by its
// working directory. The close-out takes that worktree away once the story has
// landed, and ending what holds it killed the tee: the next line mw next wrote
// (the "claimed <story>" of the dispatch that follows) died of SIGPIPE, with the
// claim made and no session launched (mw-gq6.1580). What reads the close-out's own
// output is not a writer to be ended.

const outputHelperEnv = "MW_RIG_OUTPUT_HELPER"

// TestTheCloseOutKeepsItsOwnOutputReaderWhenItTakesAWorktreeAway starts a stand-in
// for mw next as a process of its own, its output a pipe read by a process
// standing in the worktree, and has it remove the worktree and write after.
func TestTheCloseOutKeepsItsOwnOutputReaderWhenItTakesAWorktreeAway(t *testing.T) {
	here, dir, branch := aStoryWorktree(t, "mw-gq6.o1")
	kept := t.TempDir() + "/next.log"

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// The reader: what `tee -a next-N.log` is, started where the session was.
	reader := exec.Command("bash", "-c", `cat >> "$1"`, "bash", kept)
	reader.Dir = dir
	reader.Stdin = pr
	if err := reader.Start(); err != nil {
		t.Fatalf("starting the reader: %v", err)
	}
	t.Cleanup(func() { _ = reader.Process.Kill(); _ = reader.Wait() })

	closeOut := exec.Command(os.Args[0], "-test.run=^TestTheCloseOutStandIn$")
	closeOut.Dir = t.TempDir() // mw next works from the vault, not the worktree
	closeOut.Env = append(os.Environ(), outputHelperEnv+"=1",
		outputHelperEnv+"_RIG="+here, outputHelperEnv+"_DIR="+dir, outputHelperEnv+"_BRANCH="+branch)
	closeOut.Stdout, closeOut.Stderr = pw, pw
	if err := closeOut.Start(); err != nil {
		t.Fatalf("starting the close-out stand-in: %v", err)
	}
	// Only the stand-in holds the write end now, and only the reader the read end.
	_ = pr.Close()
	_ = pw.Close()

	if err := closeOut.Wait(); err != nil {
		t.Fatalf("the close-out stand-in died when it wrote after taking the worktree away (the reader of its output was ended): %v", err)
	}
	if err := reader.Wait(); err != nil {
		t.Fatalf("the reader of the close-out's output did not end on its own: %v", err)
	}
	words, err := os.ReadFile(kept)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(words), "written after the worktree went") {
		t.Fatalf("expected the line written after the removal to be kept, got %q", words)
	}
}

// TestTheCloseOutStandIn is the process the test above starts, and does nothing
// when run as a test of its own.
func TestTheCloseOutStandIn(t *testing.T) {
	if os.Getenv(outputHelperEnv) == "" {
		t.Skip("run by TestTheCloseOutKeepsItsOwnOutputReaderWhenItTakesAWorktreeAway")
	}
	here, dir, branch := os.Getenv(outputHelperEnv+"_RIG"), os.Getenv(outputHelperEnv+"_DIR"), os.Getenv(outputHelperEnv+"_BRANCH")
	if err := rig.New().Remove(context.Background(), here, dir, branch); err != nil {
		t.Fatalf("removing the worktree: %v", err)
	}
	// The first line after the removal, as "claimed <story>" is: written to stdout,
	// where a reader that was ended makes it a SIGPIPE.
	fmt.Fprintln(os.Stdout, "written after the worktree went")
}
