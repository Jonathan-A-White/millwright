package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
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

func TestBeadsEnvFileIsUnderTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".config", "mw", "beads.env")
	if got := beadsEnvFile(); got != want {
		t.Fatalf("beadsEnvFile() = %q, want %q", got, want)
	}
}

// mw-gq6.193: the dispatch that ends a landing takes the host's dispatch lock,
// as the timer's dispatch does: with another holder, it does nothing.
func TestNextEndOfLandingDispatchDoesNothingWhileTheHostDispatchLockIsHeld(t *testing.T) {
	mwConfig(t, "vault = \"/nowhere/vault\"\nhost = \"vps\"\n")
	ctx := context.Background()

	other := hostDispatchLock()
	release, taken, err := other.TryTake(ctx)
	if err != nil || !taken {
		t.Fatalf("expected to take the host's dispatch lock, got taken=%v err=%v", taken, err)
	}
	defer release()

	var out bytes.Buffer
	dispatch := withHostDispatchLocks(application.Dispatch{Host: "vps", Cap: 1, Out: &out})
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected a dispatch that finds another running to leave quietly, got %v", err)
	}
	if !strings.Contains(out.String(), "another mw dispatch is running here; nothing done") {
		t.Fatalf("expected it to say another dispatch is running, got %q", out.String())
	}
	if len(report.Started) != 0 {
		t.Fatalf("expected nothing started, got %+v", report)
	}
}
