// Command mw is the millwright factory's command line: it files stories,
// dispatches sessions to work them, and reports what the factory is doing.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/commandlog"
)

func main() {
	ctx, stop := signalContext()
	defer stop()
	if err := runRecorded(ctx); err != nil {
		// cobra has already printed the error.
		os.Exit(exitStatus(err))
	}
}

// runRecorded runs the command line mw was given and keeps a line of it in this
// host's command log (~/.local/state/mw/commands.jsonl).
func runRecorded(ctx context.Context) error {
	var log application.CommandLog
	if l, err := commandlog.Default(); err == nil {
		log = l
	}
	return executeRecorded(ctx, newRootCmd(), os.Args[1:], log, time.Now)
}

// signalContext is the context mw's command tree runs under. SIGTERM (a
// person's kill, or systemd stopping the unit) and SIGINT end it, so that
// whatever mw was doing — a sync, a dispatch tick — is told to stop rather
// than being killed outright and leaving the bd and git it started behind.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}
