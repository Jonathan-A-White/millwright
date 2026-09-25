// Command mw is the millwright factory's command line: it files stories,
// dispatches sessions to work them, and reports what the factory is doing.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signalContext()
	defer stop()
	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		// cobra has already printed the error.
		os.Exit(exitCode(err))
	}
}

// signalContext is the context mw's command tree runs under. SIGTERM (a
// person's kill, or systemd stopping the unit) and SIGINT end it, so that
// whatever mw was doing — a sync, a dispatch tick — is told to stop rather
// than being killed outright and leaving the bd and git it started behind.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}
