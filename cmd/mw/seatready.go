package main

import (
	"context"
	"io"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
)

// readyWindows is the terminal a seat's session is opened through, held until
// claude answers on this host (mw-gq6.1328): the seats' and the millhand's
// sessions start in a window, so they wait as a story's session does through
// application.HarnessReadyRunner. The asked-about claude is the one `mw version`
// reads.
func readyWindows(windows application.Windows, host string) application.Windows {
	return application.HarnessReadyWindows{
		Windows: windows,
		Host:    host,
		Out:     os.Stderr,
		Ready: func(ctx context.Context) error {
			_, err := ownHarnessVersion(ctx)
			return err
		},
	}
}

// harnessReadyRunner is the Runner every command that starts a session hands its
// use case (mw-gq6.1590): tmux, held until claude answers on this host, so a
// session started while Claude Code is being replaced waits for it as mw
// dispatch's does (mw-gq6.1281). mw dispatch, mw next, mw retry and mw land build
// theirs here; a raw tmux.New() starts a session with no wait.
func harnessReadyRunner(host string, out io.Writer) application.Runner {
	return application.HarnessReadyRunner{
		Runner: tmux.New(), Host: host, Out: out,
		Ready: func(ctx context.Context) error {
			_, err := ownHarnessVersion(ctx)
			return err
		},
	}
}
