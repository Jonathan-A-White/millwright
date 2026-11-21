package main

import (
	"os"
	"os/signal"
	"syscall"
)

// surviveBrokenOutput makes a write to a standard output or error nobody reads
// any more fail with EPIPE, as every other write does, instead of ending the
// process with SIGPIPE. A close-out is run as `{ mw next; } | tee -a next-N.log`
// and goes on for minutes after its story landed, claiming and starting the next
// one; the words it keeps are not worth that (mw-gq6.1580). A handler, not
// signal.Ignore: an ignored signal stays ignored in everything the close-out
// runs, a rig's tests among them, and a handler is put back to the default by exec.
func surviveBrokenOutput() {
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
}
