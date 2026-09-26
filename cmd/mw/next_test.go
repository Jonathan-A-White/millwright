package main

import (
	"bytes"
	"context"
	"testing"
)

// mw next --heartbeat is never run for real here: it would reach the real bd
// and tmux this machine has, and it is chained into a dispatched session's
// shell line, not run by hand. What is checked is the wiring — that the flag
// takes the command straight to Next.Heartbeat rather than down the close-out
// path, proven the same way the close-out path tells the two apart: a
// close-out given a vault that does not exist fails immediately trying to
// work from it, while Heartbeat never touches the vault at all and answers
// only to its context.

func TestNextCommandHasAHeartbeatFlag(t *testing.T) {
	cmd := newNextCmd()
	if cmd.Flags().Lookup("heartbeat") == nil {
		t.Fatal("expected mw next to have a --heartbeat flag")
	}
}

func TestNextHeartbeatFlagGoesToHeartbeatNotTheCloseOut(t *testing.T) {
	// A vault that does not exist: the close-out path fails on it before it
	// gets anywhere near Next.Heartbeat (it os.Chdir's into it first thing).
	// If --heartbeat instead took this path, this test would see that
	// failure, not the context's.
	mwConfig(t, "vault = \"/nowhere/vault\"\nhost = \"vps\"\n")

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"next", "--heartbeat", "mw-gq6.1"})

	// Heartbeat's own loop waits on its context before it does anything
	// else (application/next.go's wait): a context already done is answered
	// straight away, without Heartbeat ever reaching the tracker or the
	// runner — so this both proves the flag reaches Heartbeat and stays fast.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("expected --heartbeat to answer to its context and return cleanly, got: %v (%s)", err, out)
	}
}

func TestNextHeartbeatFlagStillNeedsAStoryID(t *testing.T) {
	mwConfig(t, "vault = \"/nowhere/vault\"\nhost = \"vps\"\n")

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"next", "--heartbeat"})

	if err := root.Execute(); err == nil {
		t.Fatalf("expected --heartbeat with no story id to fail, got none (%s)", out)
	}
}
