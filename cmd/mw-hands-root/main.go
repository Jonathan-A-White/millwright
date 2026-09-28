// Command mw-hands-root runs one hands step as root, once, and only once the
// Governor has approved exactly that step with his key (postern's
// docs/protocol.md §17). mw hands it the request on standard input through
// `sudo -n`, with a sudoers line naming only this program
// (contrib/install-hands-root). It takes nothing from its arguments or its
// environment: every path and limit is fixed in infrastructure/handsroot.
package main

import (
	"context"
	"os"

	"github.com/Jonathan-A-White/millwright/infrastructure/handsroot"
)

func main() {
	os.Exit(handsroot.Installed().Run(context.Background(), os.Stdin, os.Stdout, os.Stderr))
}
