package main

import (
	"context"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
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
