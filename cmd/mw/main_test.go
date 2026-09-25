package main

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// A SIGTERM is what systemd sends to stop the dispatch unit, and what a
// person's kill sends by default. mw's command tree runs under a context that
// must end when either arrives, so that a sync in flight can stop its bd and
// git children instead of leaving them orphaned when the process dies.
func TestSignalContextEndsWhenTheProcessIsSentSIGTERM(t *testing.T) {
	ctx, stop := signalContext()
	defer stop()

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM to this process: %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected the context to end when the process is sent SIGTERM")
	}
}

func TestSignalContextEndsWhenTheProcessIsSentSIGINT(t *testing.T) {
	ctx, stop := signalContext()
	defer stop()

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT to this process: %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected the context to end when the process is sent SIGINT")
	}
}
